package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RetentionResult reports what a retention pass removed.
type RetentionResult struct {
	Partitions []string
	Rows       map[string]int64
}

// ApplyRetention removes data older than cutoff: whole event partitions,
// then counters, facets and issues not seen since, and expired
// housekeeping rows (sessions, link codes, delivered notifications).
func (d *DB) ApplyRetention(ctx context.Context, cutoff time.Time) (RetentionResult, error) {
	res := RetentionResult{Rows: map[string]int64{}}
	dropped, err := d.DropEventPartitionsBefore(ctx, cutoff)
	res.Partitions = dropped
	if err != nil {
		return res, err
	}
	stmts := []struct {
		name string
		sql  string
	}{
		{"issue_hourly", `DELETE FROM issue_hourly WHERE hour < $1`},
		{"project_hourly", `DELETE FROM project_hourly WHERE hour < $1`},
		{"customer_hourly", `DELETE FROM customer_hourly WHERE hour < $1`},
		{"issue_tags", `DELETE FROM issue_tags WHERE last_seen < $1`},
		{"issue_users", `DELETE FROM issue_users u WHERE NOT EXISTS (SELECT 1 FROM issues i WHERE i.id = u.issue_id AND i.last_seen >= $1)`},
		{"issues", `DELETE FROM issues WHERE last_seen < $1`},
		{"notification_outbox", `DELETE FROM notification_outbox WHERE sent_at IS NOT NULL AND sent_at < $1`},
		{"usage_daily", `DELETE FROM usage_daily WHERE day < ($1::timestamptz - interval '400 days')::date`},
	}
	err = d.InSystem(ctx, func(tx pgx.Tx) error {
		for _, s := range stmts {
			tag, err := tx.Exec(ctx, s.sql, cutoff)
			if err != nil {
				return fmt.Errorf("retention %s: %w", s.name, err)
			}
			res.Rows[s.name] = tag.RowsAffected()
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	// Instance-wide housekeeping outside tenant scope.
	for name, sql := range map[string]string{
		"sessions":   `DELETE FROM sessions WHERE expires_at < now()`,
		"link_codes": `DELETE FROM link_codes WHERE expires_at < now() - interval '1 day'`,
	} {
		tag, err := d.Pool.Exec(ctx, sql)
		if err != nil {
			return res, fmt.Errorf("retention %s: %w", name, err)
		}
		res.Rows[name] = tag.RowsAffected()
	}
	return res, nil
}
