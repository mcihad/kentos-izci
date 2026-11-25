package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/internal/testutil"
)

// TestSubjectQueriesUseIndexes guards the query shapes of data subject
// requests: under row-level security only leakproof conditions reach an
// index, so each step must stay answerable from one whatever the size of
// the events table.
func TestSubjectQueriesUseIndexes(t *testing.T) {
	database := testutil.NewDB(t)
	ctx := context.Background()
	err := database.InOrg(ctx, uuid.New(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
			return err
		}
		for _, tc := range []struct {
			sql   string
			args  []any
			index string
		}{
			{`SELECT DISTINCT s.issue_id FROM issue_subjects s JOIN issues i ON i.id = s.issue_id
			  WHERE s.org_id = $1 AND s.key_hash = ANY($2::bytea[])`, []any{uuid.New(), [][]byte{[]byte("12345678")}}, "issue_subjects_key_idx"},
			{`SELECT count(*) FROM events e WHERE e.org_id = $1 AND e.issue_id = ANY($2::uuid[])
			  AND izci_subject_keys(e.payload -> 'user', e.client_ip) && $3::text[]`,
				[]any{uuid.New(), []uuid.UUID{uuid.New()}, []string{"email:a@b.c"}}, "events_default_issue_id_received_at_idx"},
			{`SELECT count(*) FROM events e WHERE e.org_id = $1 AND e.customer_key = $2`, []any{uuid.New(), "sivas"},
				"events_default_org_id_customer_key_received_at_idx"},
		} {
			rows, err := tx.Query(ctx, "EXPLAIN "+tc.sql, tc.args...)
			if err != nil {
				return err
			}
			var plan strings.Builder
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					return err
				}
				plan.WriteString(line + "\n")
			}
			rows.Close()
			if !strings.Contains(plan.String(), tc.index) {
				t.Errorf("query does not use %s:\n%s", tc.index, plan.String())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
