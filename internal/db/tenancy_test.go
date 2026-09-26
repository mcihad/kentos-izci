package db_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/testutil"
)

// notTenantScoped lists tables that carry an org_id column but are
// intentionally NOT protected by row-level security, because they are read
// before any tenant is known or across tenants by design. Every other table
// with org_id must have RLS enabled and forced — adding a table here needs a
// reason.
var notTenantScoped = map[string]string{
	"org_members":     "looked up by user across organizations (switcher, authorization)",
	"personal_tokens": "resolved by token hash before the organization is known",
	"link_codes":      "redeemed by the Telegram bot, which only knows the code",
}

// TestEveryTenantTableIsProtected is the tripwire for rule 3 of AGENTS.md:
// it fails when a table with an org_id column lacks forced RLS and the
// tenant policy.
func TestEveryTenantTableIsProtected(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()

	rows, err := database.Pool.Query(ctx, `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid AND p.polname = 'tenant_isolation')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'org_id' AND NOT a.attisdropped
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relispartition`)
	if err != nil {
		t.Fatalf("query catalog: %v", err)
	}
	type tbl struct {
		name                  string
		rls, force, hasPolicy bool
	}
	tables, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (tbl, error) {
		var x tbl
		err := r.Scan(&x.name, &x.rls, &x.force, &x.hasPolicy)
		return x, err
	})
	if err != nil {
		t.Fatalf("scan catalog: %v", err)
	}
	if len(tables) < 20 {
		t.Fatalf("found only %d tables with org_id: the catalog scan itself looks broken", len(tables))
	}
	for _, x := range tables {
		if _, ok := notTenantScoped[x.name]; ok {
			continue
		}
		if !x.rls || !x.force || !x.hasPolicy {
			t.Errorf("table %s has org_id but rls=%v force=%v policy=%v — call izci_enable_tenant_rls in its migration",
				x.name, x.rls, x.force, x.hasPolicy)
		}
	}
}

type fixture struct {
	orgA, orgB         uuid.UUID
	projectA, projectB uuid.UUID
	issueA, issueB     uuid.UUID
}

func seedTwoTenants(t *testing.T, database *db.DB) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{orgA: uuid.New(), orgB: uuid.New(), projectA: uuid.New(), projectB: uuid.New(), issueA: uuid.New(), issueB: uuid.New()}
	now := time.Now().UTC()
	if _, err := database.EnsureEventPartitions(ctx, now.AddDate(0, 0, -1), now.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("partitions: %v", err)
	}
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		for i, org := range []uuid.UUID{f.orgA, f.orgB} {
			slug := []string{"alpha", "beta"}[i]
			project := []uuid.UUID{f.projectA, f.projectB}[i]
			issue := []uuid.UUID{f.issueA, f.issueB}[i]
			if _, err := tx.Exec(ctx, `INSERT INTO organizations (id, slug, name) VALUES ($1, $2, $2)`, org, slug); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO projects (id, org_id, slug, name) VALUES ($1, $2, 'app', 'App')`, project, org); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO issues (id, org_id, project_id, fingerprint, title, level, first_seen, last_seen)
				VALUES ($1, $2, $3, $4, 'boom', 4, now(), now())`, issue, org, project, issue[:]); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO events (id, received_at, org_id, project_id, issue_id, event_id, occurred_at, level, payload)
				VALUES ($1, now(), $2, $3, $4, $5, now(), 4, '{}')`, uuid.Must(uuid.NewV7()), org, project, issue, uuid.New()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return f
}

func count(t *testing.T, ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, table string) int {
	t.Helper()
	var n int
	if err := q.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestTenantIsolation(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()
	f := seedTwoTenants(t, database)

	tables := []string{"projects", "issues", "events"}

	// Scoped to A: only A's rows are visible.
	err := database.InOrg(ctx, f.orgA, func(tx pgx.Tx) error {
		for _, table := range tables {
			if n := count(t, ctx, tx, table); n != 1 {
				t.Errorf("org A sees %d rows in %s, want 1", n, table)
			}
		}
		var title string
		if err := tx.QueryRow(ctx, `SELECT title FROM issues WHERE id = $1`, f.issueB).Scan(&title); err == nil {
			t.Error("org A can read org B's issue")
		}
		tag, err := tx.Exec(ctx, `UPDATE issues SET title = 'hijacked' WHERE id = $1`, f.issueB)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			t.Error("org A updated org B's issue")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InOrg A: %v", err)
	}

	// Writing a row for another tenant violates the WITH CHECK clause.
	err = database.InOrg(ctx, f.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO projects (org_id, slug, name) VALUES ($1, 'sneaky', 'Sneaky')`, f.orgB)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("inserting into another tenant must violate RLS, got %v", err)
	}

	// Without any scope nothing is visible (fail closed).
	for _, table := range tables {
		if n := count(t, ctx, database.Pool, table); n != 0 {
			t.Errorf("unscoped query sees %d rows in %s, want 0", n, table)
		}
	}

	// System scope sees everything.
	err = database.InSystem(ctx, func(tx pgx.Tx) error {
		for _, table := range tables {
			if n := count(t, ctx, tx, table); n != 2 {
				t.Errorf("system scope sees %d rows in %s, want 2", n, table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InSystem: %v", err)
	}
}

func TestEventPartitions(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()
	today := time.Now().UTC()

	created, err := database.EnsureEventPartitions(ctx, today.AddDate(0, 0, -3), today.AddDate(0, 0, 2))
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(created) != 6 {
		t.Fatalf("want 6 new partitions, got %v", created)
	}
	again, err := database.EnsureEventPartitions(ctx, today.AddDate(0, 0, -3), today.AddDate(0, 0, 2))
	if err != nil || len(again) != 0 {
		t.Fatalf("second ensure must be a no-op, got %v, %v", again, err)
	}

	var compression string
	if err := database.Pool.QueryRow(ctx, `
		SELECT attcompression::text FROM pg_attribute
		WHERE attrelid = $1::regclass AND attname = 'payload'`, db.PartitionName(today)).Scan(&compression); err != nil {
		t.Fatalf("compression: %v", err)
	}
	if compression != "l" {
		t.Errorf("payload compression = %q, want lz4 (l)", compression)
	}

	// An event lands in today's partition, not in the default one.
	seedTwoTenants(t, database)
	var part string
	err = database.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT tableoid::regclass::text FROM events LIMIT 1`).Scan(&part)
	})
	if err != nil {
		t.Fatalf("locate event: %v", err)
	}
	if part != db.PartitionName(today) {
		t.Errorf("event stored in %s, want %s", part, db.PartitionName(today))
	}

	dropped, err := database.DropEventPartitionsBefore(ctx, today.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("drop: %v", err)
	}
	want := []string{db.PartitionName(today.AddDate(0, 0, -3)), db.PartitionName(today.AddDate(0, 0, -2))}
	if !slices.Equal(dropped, want) {
		t.Fatalf("dropped %v, want %v", dropped, want)
	}
	parts, err := database.ListEventPartitions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(parts) != 4 || parts[0].Name != db.PartitionName(today.AddDate(0, 0, -1)) {
		t.Fatalf("unexpected partitions after drop: %+v", parts)
	}
}
