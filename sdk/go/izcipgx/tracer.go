// Package izcipgx records pgx queries as izci breadcrumbs and attaches the
// failing statement (with its PostgreSQL error code) to the scope, so an
// error captured afterwards shows exactly which query failed.
//
//	cfg, _ := pgxpool.ParseConfig(url)
//	cfg.ConnConfig.Tracer = &izcipgx.Tracer{}
//
// It is a separate module so that the SDK itself does not depend on pgx.
package izcipgx

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	izci "github.com/mcihad/kentos-izci/sdk/go"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Tracer implements pgx.QueryTracer.
type Tracer struct {
	// MaxStatement truncates statements in breadcrumbs (default 1000).
	MaxStatement int
	// SkipBreadcrumbs disables query breadcrumbs (failing queries are still
	// attached to the scope).
	SkipBreadcrumbs bool
}

type startKey struct{}

type start struct {
	at  time.Time
	sql string
}

// TraceQueryStart implements pgx.QueryTracer.
func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, startKey{}, start{at: time.Now(), sql: data.SQL})
}

// TraceQueryEnd implements pgx.QueryTracer.
func (t *Tracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	s, ok := ctx.Value(startKey{}).(start)
	if !ok {
		return
	}
	elapsed := time.Since(s.at)
	stmt := strings.TrimSpace(s.sql)
	limit := t.MaxStatement
	if limit <= 0 {
		limit = 1000
	}
	if len(stmt) > limit {
		stmt = stmt[:limit] + "…"
	}
	if !t.SkipBreadcrumbs {
		lvl := izci.LevelInfo
		d := map[string]any{"db.system": "postgresql", "duration_ms": float64(elapsed.Microseconds()) / 1000, "rows": data.CommandTag.RowsAffected()}
		if data.Err != nil {
			lvl = izci.LevelError
			d["error"] = data.Err.Error()
		}
		izci.AddBreadcrumb(ctx, izci.Breadcrumb{Type: "query", Category: "db.sql", Level: lvl, Message: stmt, Data: d})
	}
	if data.Err == nil || errors.Is(data.Err, pgx.ErrNoRows) {
		return
	}
	db := &ingestv1.DatabaseContext{
		System: "postgresql", Statement: stmt, DurationMs: float64(elapsed.Microseconds()) / 1000,
		Operation: operation(stmt),
	}
	if conn != nil {
		cfg := conn.Config()
		db.Name, db.Host = cfg.Database, cfg.Host
	}
	var pgErr *pgconn.PgError
	if errors.As(data.Err, &pgErr) {
		db.ErrorCode = pgErr.Code
	}
	if scope := izci.FromContext(ctx); scope != nil {
		scope.SetDatabase(db)
	} else {
		izci.ConfigureScope(func(s *izci.Scope) { s.SetDatabase(db) })
	}
}

func operation(stmt string) string {
	f := strings.Fields(stmt)
	if len(f) == 0 {
		return ""
	}
	return strings.ToUpper(f[0])
}
