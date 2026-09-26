// Package db owns the PostgreSQL connection pool, schema migrations and the
// tenant-scoped transaction helpers that drive row-level security (RLS).
//
// Every tenant-owned table carries an org_id column and FORCE ROW LEVEL
// SECURITY with the policy
//
//	org_id = current_setting('izci.org_id')::uuid OR current_setting('izci.system') = 'on'
//
// Application code never talks to tenant tables outside of InOrg (one
// organization) or InSystem (trusted background paths such as the ingest
// writer and maintenance jobs). Because izci connects as a non-superuser role
// that owns the tables, the policies are enforced even for the owner.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxuuid "github.com/vgarvardt/pgx-google-uuid/v5"

	"github.com/mcihad/kentos-izci/internal/config"
)

// MinServerVersion is the oldest supported PostgreSQL (18.0): izci relies on
// uuidv7() and RETURNING old/new.
const MinServerVersion = 180000

// DB wraps the pgx pool with tenant-aware helpers.
type DB struct {
	Pool *pgxpool.Pool
	log  *slog.Logger
}

// ServerInfo describes the connected server and role.
type ServerInfo struct {
	Version    string
	VersionNum int
	Role       string
	Superuser  bool
	BypassRLS  bool
	Database   string
}

// Privileged reports whether row-level security is bypassed for this role.
func (s ServerInfo) Privileged() bool { return s.Superuser || s.BypassRLS }

// Open creates the pool and verifies connectivity.
func Open(ctx context.Context, cfg config.DBConfig, log *slog.Logger) (*DB, error) {
	return OpenURL(ctx, cfg.URL, cfg.MaxConns, log)
}

// OpenURL creates a pool for an explicit connection string.
func OpenURL(ctx context.Context, url string, maxConns int32, log *slog.Logger) (*DB, error) {
	pc, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if maxConns > 0 {
		pc.MaxConns = maxConns
	}
	pc.MinConns = 1
	pc.MaxConnIdleTime = 5 * time.Minute
	pc.HealthCheckPeriod = 30 * time.Second
	if pc.ConnConfig.RuntimeParams == nil {
		pc.ConnConfig.RuntimeParams = map[string]string{}
	}
	if _, ok := pc.ConnConfig.RuntimeParams["application_name"]; !ok {
		pc.ConnConfig.RuntimeParams["application_name"] = "izci"
	}
	// Store and compare timestamps in UTC regardless of server defaults.
	pc.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pc.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		pgxuuid.Register(conn.TypeMap())
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return &DB{Pool: pool, log: log}, nil
}

// Close releases all connections.
func (d *DB) Close() { d.Pool.Close() }

// Ping checks connectivity (used by /readyz).
func (d *DB) Ping(ctx context.Context) error { return d.Pool.Ping(ctx) }

// Inspect returns server version and role privileges.
func (d *DB) Inspect(ctx context.Context) (ServerInfo, error) {
	var s ServerInfo
	err := d.Pool.QueryRow(ctx, `
		SELECT current_setting('server_version'),
		       current_setting('server_version_num')::int,
		       r.rolname, r.rolsuper, r.rolbypassrls, current_database()
		FROM pg_roles r WHERE r.rolname = current_user`).
		Scan(&s.Version, &s.VersionNum, &s.Role, &s.Superuser, &s.BypassRLS, &s.Database)
	if err != nil {
		return s, fmt.Errorf("inspect server: %w", err)
	}
	return s, nil
}

// ErrPrivilegedRole is returned when the connected role bypasses RLS in
// production.
var ErrPrivilegedRole = errors.New("database role bypasses row-level security")

// CheckServer enforces the minimum PostgreSQL version and warns (or fails in
// production) when the role would bypass row-level security.
func (d *DB) CheckServer(ctx context.Context, production, allowPrivileged bool) (ServerInfo, error) {
	info, err := d.Inspect(ctx)
	if err != nil {
		return info, err
	}
	if info.VersionNum < MinServerVersion {
		return info, fmt.Errorf("PostgreSQL %s is not supported: izci requires PostgreSQL 18 or newer", info.Version)
	}
	if info.Privileged() {
		msg := "database role is a superuser or has BYPASSRLS: tenant isolation (RLS) is NOT enforced; run `izci db bootstrap` and connect as the dedicated izci role"
		if production && !allowPrivileged {
			return info, fmt.Errorf("%w: %s (set DB_ALLOW_PRIVILEGED_ROLE=true to override)", ErrPrivilegedRole, info.Role)
		}
		d.log.Warn(msg, "role", info.Role)
	}
	return info, nil
}

// InOrg runs fn inside a transaction whose row-level security scope is orgID.
// The setting is transaction-local, so pooled connections never leak scope.
func (d *DB) InOrg(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error {
	if orgID == uuid.Nil {
		return errors.New("db: InOrg called with a nil organization id")
	}
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('izci.org_id', $1, true)`, orgID.String()); err != nil {
			return fmt.Errorf("set tenant scope: %w", err)
		}
		return fn(tx)
	})
}

// InSystem runs fn in a transaction that may touch every tenant. Reserved for
// trusted server-side paths (ingest writer, maintenance jobs, migrations of
// data, instance administration).
func (d *DB) InSystem(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('izci.system', 'on', true)`); err != nil {
			return fmt.Errorf("set system scope: %w", err)
		}
		return fn(tx)
	})
}
