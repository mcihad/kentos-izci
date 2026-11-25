package symbols_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/symbols"
)

func checksum(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

// putChunks uploads data in chunks of n bytes and returns their checksums.
func putChunks(t *testing.T, s *symbols.Service, orgID uuid.UUID, data []byte, n int) []string {
	t.Helper()
	var chunks []store.Chunk
	var sums []string
	for i := 0; i < len(data); i += n {
		p := data[i:min(len(data), i+n)]
		chunks = append(chunks, store.Chunk{Checksum: checksum(p), Data: p})
		sums = append(sums, checksum(p))
	}
	if err := s.PutChunks(context.Background(), orgID, chunks); err != nil {
		t.Fatal(err)
	}
	return sums
}

type entry struct {
	name, typ, url string
	data           []byte
}

func zipBundle(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]any{}
	for _, e := range entries {
		w, _ := zw.Create(e.name)
		_, _ = w.Write(e.data)
		files[e.name] = map[string]any{"type": e.typ, "url": e.url}
	}
	w, _ := zw.Create("manifest.json")
	_ = json.NewEncoder(w).Encode(map[string]any{"files": files})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestUploadLimits checks that uploads are assembled within their limits
// through a temporary file, and that bundles are read one file at a time.
func TestUploadLimits(t *testing.T) {
	e := ingesttest.New(t, ingesttest.Options{})
	ctx := context.Background()
	dir := t.TempDir()
	// Files an earlier crash left behind go; recent ones may belong to
	// another instance.
	stale, recent := filepath.Join(dir, "izci-upload-1"), filepath.Join(dir, "izci-upload-2")
	for _, f := range []string{stale, recent} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(stale, old, old)
	s := symbols.NewService(e.DB, slog.New(slog.DiscardHandler), symbols.Options{MaxFileBytes: 1 << 20, TempDir: dir})
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a stale upload was kept")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatal("a recent upload was removed")
	}
	_ = os.Remove(recent)
	if s.MaxFileBytes() != 1<<20 {
		t.Fatalf("max file bytes: %d", s.MaxFileBytes())
	}
	project := []uuid.UUID{e.Project.ID}

	// A file over the limit is refused by its chunk sizes.
	big := bytes.Repeat([]byte("0123456789abcdef"), 80<<10)
	res, err := s.AssembleDebugFile(ctx, e.Org.ID, e.Project.ID, checksum(big), "app.symbols", "", putChunks(t, s, e.Org.ID, big, 512<<10))
	if err != nil || res.State != symbols.StateError || !strings.Contains(res.Detail, "larger than 1 MiB") {
		t.Fatalf("file over the limit: %+v %v", res, err)
	}

	// A small archive whose script unpacks beyond the limit is refused.
	bomb := zipBundle(t, entry{"files/app.js", "minified_source", "~/app.js", bytes.Repeat([]byte("a"), 2<<20)})
	res, err = s.AssembleBundle(ctx, e.Org.ID, project, checksum(bomb), putChunks(t, s, e.Org.ID, bomb, 512<<10), "1.0.0", "")
	if err != nil || res.State != symbols.StateError || !strings.Contains(res.Detail, "files/app.js is larger than 1 MiB") {
		t.Fatalf("file in a bundle over the limit: %+v %v", res, err)
	}
	// A wrong checksum is the upload's fault.
	small := zipBundle(t, entry{"files/x.js", "minified_source", "~/x.js", []byte("x")})
	res, err = s.AssembleBundle(ctx, e.Org.ID, project, strings.Repeat("0", 40), putChunks(t, s, e.Org.ID, small, 512<<10), "1.0.0", "")
	if err != nil || res.State != symbols.StateError || !strings.Contains(res.Detail, "checksum") {
		t.Fatalf("wrong checksum: %+v %v", res, err)
	}

	// Within the limits the bundle is stored file by file; an inline map
	// becomes a file of its own.
	sm := `{"version":3,"sources":["app.ts"],"names":[],"mappings":"AAAA"}`
	script := "console.log(1);\n//# sourceMappingURL=data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(sm)) + "\n"
	good := zipBundle(t,
		entry{"files/app.js", "minified_source", "~/app.js", []byte(script)},
		entry{"files/lib.js.map", "source_map", "~/lib.js.map", []byte(sm)},
	)
	res, err = s.AssembleBundle(ctx, e.Org.ID, project, checksum(good), putChunks(t, s, e.Org.ID, good, 256), "1.0.0", "")
	if err != nil || res.State != symbols.StateOK {
		t.Fatalf("bundle: %+v %v", res, err)
	}
	var count, files int
	err = e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT file_count FROM artifact_bundles WHERE project_id = $1`, e.Project.ID).Scan(&count); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM artifact_files WHERE project_id = $1`, e.Project.ID).Scan(&files)
	})
	if err != nil || count != 3 || files != 3 {
		t.Fatalf("stored files: count %d, rows %d, %v", count, files, err)
	}

	// Assemblies leave nothing in the temporary directory.
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}
