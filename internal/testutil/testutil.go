// Package testutil provides helpers for integration tests that need a real
// PostgreSQL database.
//
// Tests find the database through IZCI_TEST_DATABASE_URL (environment first,
// then the repository's .env). Each NewDB call creates a private schema, points
// the pool's search_path at it and applies all migrations, so test packages
// can run in parallel against the same database without interfering.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"

	"github.com/mcihad/kentos-izci/internal/db"
)

// DatabaseURL returns the integration-test database URL or skips the test.
func DatabaseURL(t testing.TB) string {
	t.Helper()
	if u := os.Getenv("IZCI_TEST_DATABASE_URL"); u != "" {
		return u
	}
	if root := RepoRoot(); root != "" {
		if vals, err := godotenv.Read(filepath.Join(root, ".env")); err == nil && vals["IZCI_TEST_DATABASE_URL"] != "" {
			return vals["IZCI_TEST_DATABASE_URL"]
		}
	}
	t.Skip("IZCI_TEST_DATABASE_URL is not set; skipping integration test")
	return ""
}

// RepoRoot walks up from the working directory to the directory holding
// go.work (the repository root). It returns "" when not found.
func RepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// NewDB returns a migrated database living in a fresh, private schema. The
// schema is dropped when the test finishes.
func NewDB(t testing.TB) *db.DB {
	t.Helper()
	url := DatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var b [6]byte
	_, _ = rand.Read(b[:])
	schema := "t_" + hex.EncodeToString(b[:])

	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	// Extensions are database-wide: keep pg_trgm in public so that it is
	// visible from every private test schema (and survives their drops).
	if _, err := admin.Exec(ctx, `
		DO $$
		BEGIN
		  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm') THEN
		    CREATE EXTENSION pg_trgm SCHEMA public;
		  ELSIF (SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
		         WHERE e.extname = 'pg_trgm') <> 'public' THEN
		    ALTER EXTENSION pg_trgm SET SCHEMA public;
		  END IF;
		END $$`); err != nil {
		t.Fatalf("ensure pg_trgm: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	_ = admin.Close(ctx)

	database, err := db.OpenURL(ctx, url+searchPathSuffix(url, schema), 8, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		if conn, err := pgx.Connect(cctx, url); err == nil {
			_, _ = conn.Exec(cctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
			_ = conn.Close(cctx)
		}
	})
	if err := database.MigrateUp(ctx); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
	return database
}

// searchPathSuffix returns the query fragment that points search_path at the
// private schema (public stays reachable for extension objects).
func searchPathSuffix(conn, schema string) string {
	sep := "?"
	if strings.Contains(conn, "?") {
		sep = "&"
	}
	return sep + "search_path=" + schema + ",public"
}
