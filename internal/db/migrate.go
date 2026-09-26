package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// MigrationsFS exposes the embedded SQL migrations.
func MigrationsFS() fs.FS {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		panic(err) // the embed pattern guarantees the directory exists
	}
	return sub
}

func (d *DB) provider() (*goose.Provider, *sql.DB, error) {
	sqlDB := stdlib.OpenDBFromPool(d.Pool)
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, MigrationsFS(),
		goose.WithTableName("schema_migrations"),
	)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("create migration provider: %w", err)
	}
	return p, sqlDB, nil
}

// MigrateUp applies all pending migrations.
func (d *DB) MigrateUp(ctx context.Context) error {
	p, sqlDB, err := d.provider()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	results, err := p.Up(ctx)
	for _, r := range results {
		d.log.Info("migration applied", slog.Int64("version", r.Source.Version), slog.String("file", r.Source.Path), slog.Duration("took", r.Duration))
	}
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// MigrateDown rolls back the most recent migration.
func (d *DB) MigrateDown(ctx context.Context) error {
	p, sqlDB, err := d.provider()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	r, err := p.Down(ctx)
	if r != nil {
		d.log.Info("migration rolled back", slog.Int64("version", r.Source.Version), slog.String("file", r.Source.Path))
	}
	if err != nil {
		return fmt.Errorf("roll back migration: %w", err)
	}
	return nil
}

// MigrationStatus describes one migration.
type MigrationStatus struct {
	Version int64
	File    string
	Applied bool
}

// MigrateStatus lists all migrations and whether they are applied.
func (d *DB) MigrateStatus(ctx context.Context) ([]MigrationStatus, error) {
	p, sqlDB, err := d.provider()
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	st, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration status: %w", err)
	}
	out := make([]MigrationStatus, 0, len(st))
	for _, s := range st {
		out = append(out, MigrationStatus{
			Version: s.Source.Version,
			File:    s.Source.Path,
			Applied: s.State == goose.StateApplied,
		})
	}
	return out, nil
}
