package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/testutil"
)

func TestServerRequirements(t *testing.T) {
	database := testutil.NewDB(t)
	info, err := database.CheckServer(context.Background(), true, false)
	if err != nil {
		t.Fatalf("CheckServer: %v", err)
	}
	if info.VersionNum < 180000 {
		t.Fatalf("PostgreSQL 18+ required, got %s", info.Version)
	}
	if info.Privileged() {
		t.Fatalf("test role %q must not bypass RLS", info.Role)
	}
}

func TestMigrationsApplied(t *testing.T) {
	database := testutil.NewDB(t)
	st, err := database.MigrateStatus(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st) == 0 {
		t.Fatal("expected at least one migration")
	}
	for _, s := range st {
		if !s.Applied {
			t.Errorf("migration %d (%s) not applied", s.Version, s.File)
		}
	}
}

func TestTenantScopes(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()
	org := uuid.New()

	var scoped, system string
	err := database.InOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT current_setting('izci.org_id', true), coalesce(current_setting('izci.system', true), '')`).Scan(&scoped, &system)
	})
	if err != nil {
		t.Fatalf("InOrg: %v", err)
	}
	if scoped != org.String() || system == "on" {
		t.Fatalf("InOrg must scope to the org only, got org=%q system=%q", scoped, system)
	}

	// The scope is transaction-local: a plain query afterwards sees nothing.
	var leaked string
	if err := database.Pool.QueryRow(ctx, `SELECT coalesce(current_setting('izci.org_id', true), '')`).Scan(&leaked); err != nil {
		t.Fatalf("query: %v", err)
	}
	if leaked != "" {
		t.Fatalf("tenant scope leaked outside the transaction: %q", leaked)
	}

	if err := database.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT current_setting('izci.system', true)`).Scan(&system)
	}); err != nil {
		t.Fatalf("InSystem: %v", err)
	}
	if system != "on" {
		t.Fatalf("InSystem must enable system scope, got %q", system)
	}

	if err := database.InOrg(ctx, uuid.Nil, func(pgx.Tx) error { return nil }); err == nil {
		t.Fatal("InOrg with a nil org id must fail")
	}
}
