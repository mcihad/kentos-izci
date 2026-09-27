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
		{"notification_outbox", `DELETE FROM notification_outbox WHERE created_at < $1 AND (sent_at IS NOT NULL OR next_attempt_at = 'infinity')`},
		{"event_attachments", `DELETE FROM event_attachments WHERE created_at < $1`},
		{"user_feedback", `DELETE FROM user_feedback WHERE created_at < $1`},
		{"release_sessions", `DELETE FROM release_sessions WHERE hour < $1`},
		{"monitor_checkins", `DELETE FROM monitor_checkins WHERE created_at < $1 AND status <> 'in_progress'`},
		// Source maps and debug files stay while they resolve frames, and
		// at least 90 days after upload or last use.
		{"artifact_bundles", `DELETE FROM artifact_bundles WHERE coalesce(last_used_at, created_at) < least($1, now() - interval '90 days')`},
		{"debug_files", `DELETE FROM debug_files WHERE coalesce(last_used_at, created_at) < least($1, now() - interval '90 days')`},
		// Chunks of uploads that were never assembled.
		{"upload_chunks", `DELETE FROM upload_chunks WHERE created_at < greatest($1, now() - interval '1 hour')`},
		{"release_session_users", `DELETE FROM release_session_users WHERE day < $1::date`},
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

// PurgeDeletedProjects removes projects marked deleted, at most limit per
// call. Tables without foreign keys are cleared first; deleting the project
// row then cascades to issues, keys, releases, rules and repositories.
func (d *DB) PurgeDeletedProjects(ctx context.Context, limit int) ([]string, error) {
	var slugs []string
	err := d.InSystem(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, slug FROM projects WHERE deleted_at IS NOT NULL ORDER BY deleted_at LIMIT $1`, limit)
		if err != nil {
			return err
		}
		type victim struct {
			id   [16]byte
			slug string
		}
		var list []victim
		for rows.Next() {
			var v victim
			if err := rows.Scan(&v.id, &v.slug); err != nil {
				rows.Close()
				return err
			}
			list = append(list, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, v := range list {
			for _, sql := range []string{
				`DELETE FROM events WHERE project_id = $1`,
				`DELETE FROM issue_hourly WHERE project_id = $1`,
				`DELETE FROM project_hourly WHERE project_id = $1`,
				`DELETE FROM customer_hourly WHERE project_id = $1`,
				`DELETE FROM issue_tags WHERE project_id = $1`,
				`DELETE FROM issue_users u USING issues i WHERE i.id = u.issue_id AND i.project_id = $1`,
				`DELETE FROM notification_threads t USING issues i WHERE i.id = t.issue_id AND i.project_id = $1`,
				`DELETE FROM projects WHERE id = $1`,
			} {
				if _, err := tx.Exec(ctx, sql, v.id); err != nil {
					return fmt.Errorf("purge project %s: %w", v.slug, err)
				}
			}
			slugs = append(slugs, v.slug)
		}
		return nil
	})
	return slugs, err
}
