package symbols

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/symbols/native"
	"github.com/mcihad/kentos-izci/internal/symbols/sourcemap"
)

// Limits of uploads.
const (
	// DefaultMaxFileBytes bounds one assembled file (a bundle archive or a
	// debug file) and one file inside a bundle unless Options.MaxFileBytes
	// says otherwise.
	DefaultMaxFileBytes = 128 << 20
	// minUnpackedBytes is the least a bundle may unpack to; with larger
	// files it may unpack to twice the file limit.
	minUnpackedBytes = 1 << 30
	// ChunkBytes is the chunk size clients are told to use.
	ChunkBytes = 8 << 20
	// maxBundleFiles bounds the files of one bundle.
	maxBundleFiles = 20000
	// MaxProjectBytes bounds what a project stores; old uploads have to be
	// deleted (or expire) before new ones fit.
	MaxProjectBytes = 10 << 30
	// maxPendingChunkBytes bounds the chunks an organization has uploaded
	// but not assembled.
	maxPendingChunkBytes = 4 << 30
)

// ErrChunkQuota means an organization has too many chunks pending.
var ErrChunkQuota = errors.New("too many uploaded chunks are waiting to be assembled; try again later")

// KindDartSymbolMap is the kind of Dart obfuscation maps.
const KindDartSymbolMap = "dartsymbolmap"

// Assembly states of Sentry's protocol.
const (
	StateOK       = "ok"
	StateNotFound = "not_found"
	StateError    = "error"
)

// Assembly is the outcome of an assemble request.
type Assembly struct {
	State   string
	Missing []string
	Detail  string
	// File is the stored debug file (debug file assemblies).
	File *store.DebugFile
}

// Checksum returns the SHA-1 of data in hex, the way chunks and files are
// named.
func Checksum(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// PutChunks stores uploaded chunks after checking their checksums.
func (s *Service) PutChunks(ctx context.Context, orgID uuid.UUID, chunks []store.Chunk) error {
	for _, c := range chunks {
		if Checksum(c.Data) != strings.ToLower(c.Checksum) {
			return fmt.Errorf("chunk %s does not match its checksum", c.Checksum)
		}
	}
	return s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		pending, err := store.PendingChunkBytes(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if pending > maxPendingChunkBytes {
			return ErrChunkQuota
		}
		return store.PutChunks(ctx, tx, orgID, chunks)
	})
}

// fits refuses uploads that would take a project over its quota.
func fits(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, size int64) error {
	used, err := store.ProjectSymbolBytes(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if used+size > MaxProjectBytes {
		return errAssembly{fmt.Errorf("the project's source maps and debug files would exceed %d GiB; delete old uploads first",
			MaxProjectBytes>>30)}
	}
	return nil
}

// slot waits for one of the assembly slots; release it with the returned
// function.
func (s *Service) slot(ctx context.Context) (func(), error) {
	select {
	case s.assembling <- struct{}{}:
		return func() { <-s.assembling }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// upload is an assembled upload in a temporary file.
type upload struct {
	*os.File
	size int64
}

// close removes the temporary file.
func (u *upload) close() {
	_ = u.File.Close()
	_ = os.Remove(u.Name())
}

// gather puts an upload together from its chunks in a temporary file, or
// reports the chunks still missing. Sizes are checked before any data is
// read, and chunks are copied one at a time: memory does not grow with the
// file. The caller closes the upload.
func (s *Service) gather(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, checksum string, chunks []string) (*upload, []string, error) {
	if len(chunks) == 0 {
		return nil, nil, errAssembly{errors.New("no chunks given")}
	}
	sizes, err := store.ChunkSizes(ctx, tx, orgID, chunks)
	if err != nil {
		return nil, nil, err
	}
	missing := []string{}
	seen := map[string]bool{}
	var total int64
	for _, c := range chunks {
		n, ok := sizes[c]
		if !ok && !seen[c] {
			missing = append(missing, c)
		}
		seen[c] = true
		total += n
	}
	if len(missing) > 0 {
		return nil, missing, nil
	}
	if total > s.maxFile {
		return nil, nil, errAssembly{fmt.Errorf("the file is larger than %d MiB (INGEST_UPLOAD_MAX_MB)", s.maxFile>>20)}
	}
	if err := os.MkdirAll(s.tempDir, 0o700); err != nil {
		return nil, nil, err
	}
	f, err := os.CreateTemp(s.tempDir, tempPattern)
	if err != nil {
		return nil, nil, err
	}
	u := &upload{File: f}
	sum := sha1.New()
	w := io.MultiWriter(f, sum)
	for _, c := range chunks {
		data, err := store.ChunkData(ctx, tx, orgID, c)
		if err == nil {
			_, err = w.Write(data)
		}
		if err != nil {
			u.close()
			return nil, nil, err
		}
		u.size += int64(len(data))
	}
	if hex.EncodeToString(sum.Sum(nil)) != strings.ToLower(checksum) {
		u.close()
		return nil, nil, errAssembly{errors.New("the assembled file does not match its checksum")}
	}
	return u, nil, nil
}

// errAssembly marks errors that are the upload's fault; they become an
// "error" state instead of a server error.
type errAssembly struct{ error }

// AssembleBundle assembles an artifact bundle from uploaded chunks and
// stores it for each project. release and dist override the manifest's.
func (s *Service) AssembleBundle(ctx context.Context, orgID uuid.UUID, projects []uuid.UUID, checksum string, chunks []string,
	release, dist string) (Assembly, error) {
	done, err := s.slot(ctx)
	if err != nil {
		return Assembly{}, err
	}
	defer done()
	var out Assembly
	err = s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		todo := make([]uuid.UUID, 0, len(projects))
		for _, p := range projects {
			has, err := store.HasArtifactBundle(ctx, tx, p, checksum)
			if err != nil {
				return err
			}
			if !has {
				todo = append(todo, p)
			}
		}
		if len(todo) == 0 {
			out.State = StateOK
			metrics.SymbolUploadsDuplicate.Inc()
			return nil
		}
		up, missing, err := s.gather(ctx, tx, orgID, checksum, chunks)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			out.State, out.Missing = StateNotFound, missing
			return nil
		}
		defer up.close()
		b, err := openBundle(up, up.size)
		if err != nil {
			return errAssembly{err}
		}
		info := b.info
		if release != "" {
			info.Release = release
		}
		if dist != "" {
			info.Dist = dist
		}
		info.OrgID, info.Checksum, info.SizeBytes = orgID, strings.ToLower(checksum), up.size
		stored := make([]*store.ArtifactBundle, 0, len(todo))
		for _, p := range todo {
			if err := fits(ctx, tx, p, up.size); err != nil {
				return err
			}
			pb := info
			pb.ProjectID = p
			created, err := store.CreateArtifactBundle(ctx, tx, &pb)
			if err != nil {
				return err
			}
			if created {
				stored = append(stored, &pb)
			}
		}
		// One file in memory at a time, stored for every project.
		n, err := b.files(s.maxFile, func(f *store.ArtifactFile) error {
			for _, pb := range stored {
				if err := store.InsertArtifactFile(ctx, tx, pb, f); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, pb := range stored {
			if err := store.SetArtifactBundleFileCount(ctx, tx, pb.ID, n); err != nil {
				return err
			}
		}
		out.State = StateOK
		metrics.SymbolUploadsStored.Inc()
		return store.DeleteChunks(ctx, tx, orgID, chunks)
	})
	var bad errAssembly
	if errors.As(err, &bad) {
		metrics.SymbolUploadsRejected.Inc()
		return Assembly{State: StateError, Detail: bad.Error()}, nil
	}
	if err != nil {
		return Assembly{}, err
	}
	s.Invalidate(projects...)
	return out, nil
}

// AssembleDebugFile assembles a debug information file or a Dart
// obfuscation map from uploaded chunks and stores it for the project.
func (s *Service) AssembleDebugFile(ctx context.Context, orgID, projectID uuid.UUID, checksum, name, debugID string,
	chunks []string) (Assembly, error) {
	done, err := s.slot(ctx)
	if err != nil {
		return Assembly{}, err
	}
	defer done()
	var out Assembly
	checksum = strings.ToLower(checksum)
	err = s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		existing, err := store.DebugFileByChecksum(ctx, tx, projectID, checksum)
		if err == nil {
			out.State, out.File = StateOK, existing
			metrics.SymbolUploadsDuplicate.Inc()
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		up, missing, err := s.gather(ctx, tx, orgID, checksum, chunks)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			out.State, out.Missing = StateNotFound, missing
			return nil
		}
		defer up.close()
		if err := fits(ctx, tx, projectID, up.size); err != nil {
			return err
		}
		// Debug files are parsed and stored whole; their size is bounded
		// by the file limit.
		data := make([]byte, up.size)
		if _, err := up.ReadAt(data, 0); err != nil {
			return err
		}
		f, err := DescribeDebugFile(data, name, debugID)
		if err != nil {
			return errAssembly{err}
		}
		f.OrgID, f.ProjectID, f.Checksum, f.Data = orgID, projectID, checksum, data
		stored, _, err := store.InsertDebugFile(ctx, tx, f)
		if err != nil {
			return err
		}
		out.State, out.File = StateOK, stored
		metrics.SymbolUploadsStored.Inc()
		return store.DeleteChunks(ctx, tx, orgID, chunks)
	})
	var bad errAssembly
	if errors.As(err, &bad) {
		metrics.SymbolUploadsRejected.Inc()
		return Assembly{State: StateError, Detail: bad.Error()}, nil
	}
	if err != nil {
		return Assembly{}, err
	}
	s.Invalidate(projectID)
	return out, nil
}

// DescribeDebugFile identifies an uploaded debug file: an ELF or Mach-O
// object (its own debug id wins over the one sent) or a Dart obfuscation
// map (which takes the debug id of the build it belongs to).
func DescribeDebugFile(data []byte, name, debugID string) (*store.DebugFile, error) {
	name = strings.TrimSpace(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == "/" {
		name = "debug-file"
	}
	if len(name) > 200 {
		name = name[:200]
	}
	if native.IsDartSymbolMap(data) {
		if _, err := native.ParseDartSymbolMap(data); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(debugID)
		if err != nil {
			return nil, errors.New("a Dart obfuscation map needs the debug id of its build (sentry-cli dart-symbol-map upload sends it)")
		}
		return &store.DebugFile{DebugID: id, Kind: KindDartSymbolMap, Name: name, Features: []string{"mapping"}}, nil
	}
	o, err := native.Open(data)
	if errors.Is(err, native.ErrUnsupported) {
		return nil, errors.New("unsupported file: izci reads ELF and Mach-O files with DWARF debug information " +
			"(Flutter and Dart --split-debug-info output) and Dart obfuscation maps")
	}
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(o.DebugID)
	if err != nil {
		if id, err = uuid.Parse(debugID); err != nil {
			return nil, errors.New("the file has no build id or UUID and no debug id was sent")
		}
	}
	f := &store.DebugFile{DebugID: id, CodeID: o.CodeID, Kind: string(o.Kind), Name: name, Arch: o.Arch, Features: []string{}}
	if o.Debug {
		f.Features = append(f.Features, "debug")
	}
	if o.Symbols {
		f.Features = append(f.Features, "symtab")
	}
	return f, nil
}

// manifest is manifest.json of a Sentry artifact bundle.
type manifest struct {
	Files   map[string]manifestFile `json:"files"`
	DebugID string                  `json:"debug_id"`
	Release string                  `json:"release"`
	Dist    string                  `json:"dist"`
}

type manifestFile struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

var (
	mappingURLRe = regexp.MustCompile(`(?m)^[ \t]*//[#@][ \t]*sourceMappingURL=([^\s'"]+)[ \t]*$`)
	debugIDRe    = regexp.MustCompile(`(?m)^[ \t]*//[#@][ \t]*debugId=([0-9A-Fa-f-]{32,36})[ \t]*$`)
)

// bundle is a Sentry artifact bundle: a ZIP archive with manifest.json
// listing minified scripts, source maps and sources with their URLs and
// headers (debug-id, sourcemap).
type bundle struct {
	entries  map[string]*zip.File
	manifest manifest
	// info holds what the manifest says about the bundle: release, dist
	// and bundle id.
	info store.ArtifactBundle
}

// openBundle reads the manifest of an artifact bundle.
func openBundle(r io.ReaderAt, size int64) (*bundle, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("the bundle is not a ZIP archive: %w", err)
	}
	b := &bundle{entries: map[string]*zip.File{}}
	for _, f := range zr.File {
		b.entries[f.Name] = f
	}
	mf, ok := b.entries["manifest.json"]
	if !ok {
		return nil, errors.New("the bundle has no manifest.json")
	}
	raw, err := readEntry(mf, 16<<20)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &b.manifest); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	if len(b.manifest.Files) == 0 {
		return nil, errors.New("the bundle lists no files")
	}
	if len(b.manifest.Files) > maxBundleFiles {
		return nil, fmt.Errorf("the bundle lists more than %d files", maxBundleFiles)
	}
	b.info = store.ArtifactBundle{Release: strings.TrimSpace(b.manifest.Release), Dist: strings.TrimSpace(b.manifest.Dist)}
	if id, err := uuid.Parse(b.manifest.DebugID); err == nil {
		b.info.BundleID = &id
	}
	return b, nil
}

// files reads the scripts, source maps and sources the manifest lists one
// at a time (each at most maxFile bytes), checks them and hands them to
// keep; only one file is in memory at once. It returns how many files it
// kept. Problems of the bundle come back as errAssembly, those of keep as
// they are.
func (b *bundle) files(maxFile int64, keep func(*store.ArtifactFile) error) (int, error) {
	limit := max(minUnpackedBytes, 2*maxFile)
	var total int64
	n := 0
	emit := func(f *store.ArtifactFile) error {
		n++
		return keep(f)
	}
	for name, info := range b.manifest.Files {
		kind := artifactKind(info.Type)
		if kind == 0 {
			continue
		}
		entry, ok := b.entries[name]
		if !ok {
			return 0, errAssembly{fmt.Errorf("manifest.json lists %s, which the archive lacks", name)}
		}
		content, err := readEntry(entry, maxFile)
		if err != nil {
			return 0, errAssembly{err}
		}
		total += int64(len(content))
		if total > limit {
			return 0, errAssembly{fmt.Errorf("the bundle unpacks to more than %d MiB", limit>>20)}
		}
		headers := map[string]string{}
		for k, v := range info.Headers {
			headers[strings.ToLower(k)] = strings.TrimSpace(v)
		}
		u := strings.TrimSpace(info.URL)
		if u == "" {
			u = "~/" + strings.TrimPrefix(name, "files/")
		}
		f := store.ArtifactFile{URL: u, Kind: kind, Data: content}
		if id, err := uuid.Parse(headers["debug-id"]); err == nil {
			f.DebugID = &id
		}
		switch kind {
		case store.ArtifactMinified:
			f.SourceMap = firstNonEmpty(headers["sourcemap"], headers["x-sourcemap"])
			tail := content[max(0, len(content)-64<<10):]
			if f.SourceMap == "" {
				if all := mappingURLRe.FindAllSubmatch(tail, -1); len(all) > 0 {
					f.SourceMap = string(all[len(all)-1][1])
				}
			}
			if f.DebugID == nil {
				if mt := debugIDRe.FindSubmatch(tail); mt != nil {
					if id, err := uuid.Parse(string(mt[1])); err == nil {
						f.DebugID = &id
					}
				}
			}
			// Development builds inline the map as a data URL.
			if inline, ok := decodeDataURL(f.SourceMap); ok {
				if err := emit(&store.ArtifactFile{URL: u + ".map", Kind: store.ArtifactSourceMap, DebugID: f.DebugID, Data: inline}); err != nil {
					return 0, err
				}
				f.SourceMap = path.Base(u) + ".map"
			}
		case store.ArtifactSourceMap:
			sm, err := sourcemap.Parse(content)
			if err != nil {
				return 0, errAssembly{fmt.Errorf("%s: %w", u, err)}
			}
			if f.DebugID == nil && sm.DebugID != "" {
				if id, err := uuid.Parse(sm.DebugID); err == nil {
					f.DebugID = &id
				}
			}
		}
		if err := emit(&f); err != nil {
			return 0, err
		}
	}
	if n == 0 {
		return 0, errAssembly{errors.New("the bundle has no scripts or source maps")}
	}
	return n, nil
}

func artifactKind(t string) int16 {
	switch strings.ToLower(t) {
	case "minified_source":
		return store.ArtifactMinified
	case "source_map":
		return store.ArtifactSourceMap
	case "source":
		return store.ArtifactSource
	}
	return 0
}

func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%s is larger than %d MiB", f.Name, limit>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than %d MiB", f.Name, limit>>20)
	}
	return b, nil
}

// decodeDataURL decodes an inline source map
// ("data:application/json;base64,…" or percent-encoded).
func decodeDataURL(s string) ([]byte, bool) {
	rest, ok := strings.CutPrefix(s, "data:")
	if !ok {
		return nil, false
	}
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return nil, false
	}
	if strings.HasSuffix(meta, ";base64") {
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(payload)
		}
		return b, err == nil
	}
	b, err := url.PathUnescape(payload)
	return []byte(b), err == nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
