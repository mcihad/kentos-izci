// Package symbols resolves minified JavaScript and native (Flutter and Dart
// AOT) stack traces with the source maps and debug information files that
// sentry-cli and the Sentry bundler plugins upload, and assembles those
// uploads.
//
// Resolution runs during ingest, before grouping, so issues group by the
// original functions and files. It never rejects an event: problems (a
// missing source map, an unreadable debug file) are recorded on the event
// as processing errors and shown with it.
package symbols

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"
	"google.golang.org/protobuf/proto"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Options tune the service.
type Options struct {
	// CacheBytes bounds the memory of parsed source maps, debug information
	// indexes and obfuscation maps (default 256 MiB).
	CacheBytes int
	// MaxFileBytes bounds an uploaded file (a bundle archive or a debug
	// file) and each file inside a bundle (default DefaultMaxFileBytes).
	// An assembly holds about this much memory.
	MaxFileBytes int64
	// TempDir holds uploads while they are assembled (default: the
	// system's temporary directory).
	TempDir string
}

// Service resolves stack traces and assembles uploads.
type Service struct {
	db    *db.DB
	log   *slog.Logger
	cache *sizedCache
	// refs remembers where lookups led (nil: nothing uploaded) so that
	// events of the same release do not query the database each time.
	refs  *expirable.LRU[string, *fileRef]
	stats *expirable.LRU[uuid.UUID, store.SymbolStats]
	loads singleflight.Group
	// assembling bounds concurrent assemblies, which hold up to maxFile
	// bytes in memory each.
	assembling chan struct{}
	maxFile    int64
	tempDir    string

	mu      sync.Mutex
	touched map[uuid.UUID]time.Time
}

// fileRef is the stored file a lookup found.
type fileRef struct {
	id uuid.UUID
	// parent is the artifact bundle of a source map (for last_used_at); the
	// debug file itself otherwise.
	parent uuid.UUID
	debug  bool
	// sourceMap is the reference of the minified script to its map.
	sourceMap string
}

// NewService creates the service.
func NewService(database *db.DB, log *slog.Logger, opt Options) *Service {
	if opt.CacheBytes <= 0 {
		opt.CacheBytes = 256 << 20
	}
	if opt.MaxFileBytes <= 0 {
		opt.MaxFileBytes = DefaultMaxFileBytes
	}
	if opt.TempDir == "" {
		opt.TempDir = os.TempDir()
	}
	if err := os.MkdirAll(opt.TempDir, 0o700); err != nil {
		log.Warn("upload directory not usable, using the system's temporary directory",
			slog.String("dir", opt.TempDir), slog.Any("err", err))
		opt.TempDir = os.TempDir()
	}
	removeStaleUploads(opt.TempDir, log)
	return &Service{
		db:         database,
		log:        log,
		cache:      newSizedCache(opt.CacheBytes),
		refs:       expirable.NewLRU[string, *fileRef](20000, nil, time.Minute),
		stats:      expirable.NewLRU[uuid.UUID, store.SymbolStats](10000, nil, 30*time.Second),
		assembling: make(chan struct{}, 2),
		maxFile:    opt.MaxFileBytes,
		tempDir:    opt.TempDir,
		touched:    map[uuid.UUID]time.Time{},
	}
}

// MaxFileBytes is the largest file the service accepts, told to clients.
func (s *Service) MaxFileBytes() int64 { return s.maxFile }

// tempPattern names the temporary files of assemblies.
const tempPattern = "izci-upload-*"

// removeStaleUploads deletes temporary files that assemblies cut short
// (a crash) left behind; recent ones may belong to another instance.
func removeStaleUploads(dir string, log *slog.Logger) {
	names, _ := filepath.Glob(filepath.Join(dir, tempPattern))
	for _, n := range names {
		if fi, err := os.Stat(n); err == nil && time.Since(fi.ModTime()) > time.Hour {
			if err := os.Remove(n); err != nil {
				log.Warn("remove stale upload", slog.String("file", n), slog.Any("err", err))
			}
		}
	}
}

// CacheBytes returns the memory held by parsed files.
func (s *Service) CacheBytes() int { return s.cache.bytes() }

// Symbolicate resolves the stack traces of an event in place: minified
// script frames through source maps, native frames of Flutter and Dart AOT
// builds through debug information files, and obfuscated Dart exception
// types through obfuscation maps. Frames as sent are kept in raw_frames.
func (s *Service) Symbolicate(ctx context.Context, orgID, projectID uuid.UUID, ev *ingestv1.Event) {
	stacks := stacktraces(ev)
	if len(stacks) == 0 {
		return
	}
	scripts, natives := false, false
	for _, st := range stacks {
		for _, f := range st.GetFrames() {
			scripts = scripts || isScriptFrame(ev, f)
			natives = natives || isNativeFrame(f)
		}
	}
	natives = natives && len(nativeImages(ev)) > 0
	if !scripts && !natives {
		return
	}
	stats, err := s.projectStats(ctx, orgID, projectID)
	if err != nil {
		s.log.Warn("symbols: project stats", slog.Any("err", err))
		return
	}
	scripts = scripts && stats.ArtifactBundles > 0
	natives = natives && stats.DebugFiles > 0
	if !scripts && !natives {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p := &processor{Service: s, ctx: ctx, org: orgID, project: projectID, ev: ev,
		scripts: map[string]*scriptMap{}, reported: map[string]bool{}}
	for _, st := range stacks {
		raw := cloneFrames(st.GetFrames())
		changed := false
		if natives && p.native(st) {
			changed = true
		}
		if scripts && p.javascript(st) {
			changed = true
		}
		if changed {
			st.RawFrames = raw
		}
	}
	if natives {
		p.deobfuscate()
	}
}

func stacktraces(ev *ingestv1.Event) []*ingestv1.Stacktrace {
	var out []*ingestv1.Stacktrace
	for _, ex := range ev.GetExceptions() {
		if st := ex.GetStacktrace(); st != nil {
			out = append(out, st)
		}
	}
	for _, th := range ev.GetThreads() {
		if st := th.GetStacktrace(); st != nil {
			out = append(out, st)
		}
	}
	return out
}

func cloneFrames(frames []*ingestv1.Frame) []*ingestv1.Frame {
	out := make([]*ingestv1.Frame, len(frames))
	for i, f := range frames {
		out[i] = proto.CloneOf(f)
	}
	return out
}

func (s *Service) projectStats(ctx context.Context, orgID, projectID uuid.UUID) (store.SymbolStats, error) {
	if st, ok := s.stats.Get(projectID); ok {
		return st, nil
	}
	var st store.SymbolStats
	err := s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var err error
		st, err = store.ProjectSymbolStats(ctx, tx, projectID)
		return err
	})
	if err != nil {
		return st, err
	}
	s.stats.Add(projectID, st)
	return st, nil
}

// Invalidate forgets cached lookups after uploads or deletions.
func (s *Service) Invalidate(projectIDs ...uuid.UUID) {
	for _, id := range projectIDs {
		s.stats.Remove(id)
	}
	s.refs.Purge()
}

// touch records that a bundle or debug file resolved frames, at most once
// an hour per file and instance.
func (s *Service) touch(orgID uuid.UUID, ref *fileRef) {
	s.mu.Lock()
	if time.Since(s.touched[ref.parent]) < time.Hour {
		s.mu.Unlock()
		return
	}
	s.touched[ref.parent] = time.Now()
	if len(s.touched) > 50000 {
		for id, t := range s.touched {
			if time.Since(t) > time.Hour {
				delete(s.touched, id)
			}
		}
	}
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
			if ref.debug {
				return store.TouchDebugFiles(ctx, tx, []uuid.UUID{ref.parent})
			}
			return store.TouchArtifactBundles(ctx, tx, []uuid.UUID{ref.parent})
		})
		if err != nil {
			s.log.Warn("symbols: record use", slog.Any("err", err))
		}
	}()
}

// load returns a parsed file from the cache or parses it once, however
// many events ask at the same time.
func (s *Service) load(ctx context.Context, orgID uuid.UUID, ref *fileRef, parse func([]byte) (any, int, error)) (any, error) {
	key := ref.id.String()
	if v, ok := s.cache.get(key); ok {
		if err, bad := v.(error); bad {
			return nil, err
		}
		return v, nil
	}
	v, err, _ := s.loads.Do(key, func() (any, error) {
		var data []byte
		err := s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
			var err error
			if ref.debug {
				data, err = store.DebugFileData(ctx, tx, ref.id)
			} else {
				data, err = store.ArtifactFileData(ctx, tx, ref.id)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		v, size, err := parse(data)
		if err != nil {
			// Remember unreadable files briefly instead of parsing them
			// for every event.
			s.cache.add(key, err, 256)
			return nil, err
		}
		s.cache.add(key, v, size)
		return v, nil
	})
	return v, err
}

// processor carries the state of one event's resolution.
type processor struct {
	*Service
	ctx      context.Context
	org      uuid.UUID
	project  uuid.UUID
	ev       *ingestv1.Event
	scripts  map[string]*scriptMap
	reported map[string]bool
}

// fail records a processing error once per kind and target.
func (p *processor) fail(kind ingestv1.ProcessingErrorKind, target, format string, args ...any) {
	key := kind.String() + "\x00" + target
	if p.reported[key] || len(p.ev.ProcessingErrors) >= 20 {
		return
	}
	p.reported[key] = true
	p.ev.ProcessingErrors = append(p.ev.ProcessingErrors, &ingestv1.ProcessingError{Kind: kind, Target: target,
		Detail: fmt.Sprintf(format, args...)})
}

// lookup runs a cached database lookup.
func (p *processor) lookup(key string, find func(tx pgx.Tx) (*fileRef, error)) *fileRef {
	key = p.project.String() + "\x00" + key
	if ref, ok := p.refs.Get(key); ok {
		return ref
	}
	var ref *fileRef
	err := p.db.InOrg(p.ctx, p.org, func(tx pgx.Tx) error {
		var err error
		ref, err = find(tx)
		return err
	})
	if err != nil {
		p.log.Warn("symbols: lookup", slog.Any("err", err))
		return nil
	}
	p.refs.Add(key, ref)
	return ref
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
