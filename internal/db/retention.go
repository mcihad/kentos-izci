package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RetentionResult reports what a retention pass removed.
type RetentionResult struct {
	Partitions []string
	Rows       map[string]int64
}

// ApplyRetention removes data older than cutoff: whole event partitions,
// then counters, facets and issues not seen since, and expired
// housekeeping rows (sessions, link codes, sign-in challenges, delivered
// notifications).
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
		{"issue_subjects", `DELETE FROM issue_subjects s WHERE NOT EXISTS (SELECT 1 FROM issues i WHERE i.id = s.issue_id AND i.last_seen >= $1)`},
		{"issues", `DELETE FROM issues WHERE last_seen < $1`},
		{"notification_outbox", `DELETE FROM notification_outbox WHERE created_at < $1 AND (sent_at IS NOT NULL OR next_attempt_at = 'infinity')`},
		{"event_attachments", `DELETE FROM event_attachments WHERE created_at < $1`},
		{"user_feedback", `DELETE FROM user_feedback WHERE created_at < $1`},
		// Inbox entries live at most 90 days.
		{"user_notifications", `DELETE FROM user_notifications WHERE created_at < greatest($1, now() - interval '90 days')`},
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
		{"project_filtered_daily", `DELETE FROM project_filtered_daily WHERE day < least($1::timestamptz, now() - interval '31 days')::date`},
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
		"sessions":         `DELETE FROM sessions WHERE expires_at < now()`,
		"link_codes":       `DELETE FROM link_codes WHERE expires_at < now() - interval '1 day'`,
		"login_challenges": `DELETE FROM login_challenges WHERE expires_at < now()`,
		"auth_attempts":    `DELETE FROM auth_attempts WHERE window_start < now() - interval '1 hour'`,
		"pending_signups":  `DELETE FROM pending_signups WHERE expires_at < now()`,
		"password_resets":  `DELETE FROM password_resets WHERE expires_at < now()`,
		// Devices unused for a year are new again.
		"user_devices": `DELETE FROM user_devices WHERE last_seen < now() - interval '1 year'`,
		// Two-factor setups that were started but never confirmed.
		"totp_pending": `UPDATE users SET totp_pending_secret = NULL, totp_pending_at = NULL
			WHERE totp_pending_at < now() - interval '1 day'`,
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
				`DELETE FROM issue_subjects s USING issues i WHERE i.id = s.issue_id AND i.project_id = $1`,
				`DELETE FROM notification_threads t USING issues i WHERE i.id = t.issue_id AND i.project_id = $1`,
				`DELETE FROM deleted_issues WHERE project_id = $1`,
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

// purgeBatch is how many events one statement of the purge jobs deletes:
// small enough to keep transactions short.
const purgeBatch = 5000

// deleteEventsBatch deletes up to purgeBatch events matching cond (alias
// e) with their attachments, in its own transaction, and reports how many
// it deleted.
func (d *DB) deleteEventsBatch(ctx context.Context, cond string, args ...any) (int64, error) {
	var n int64
	err := d.InSystem(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, fmt.Sprintf(`
			WITH v AS (SELECT e.id, e.received_at, e.project_id, e.event_id FROM events e WHERE %s LIMIT %d),
			a AS (DELETE FROM event_attachments x USING v WHERE x.project_id = v.project_id AND x.event_id = v.event_id)
			DELETE FROM events x USING v WHERE x.id = v.id AND x.received_at = v.received_at`, cond, purgeBatch), args...)
		n = tag.RowsAffected()
		return err
	})
	return n, err
}

// deleteAllEvents runs deleteEventsBatch until no event matches.
func (d *DB) deleteAllEvents(ctx context.Context, cond string, args ...any) (int64, error) {
	var total int64
	for {
		n, err := d.deleteEventsBatch(ctx, cond, args...)
		total += n
		if err != nil || n < purgeBatch {
			return total, err
		}
	}
}

// PurgeDeletedIssues removes the events (with attachments), counters,
// facets and affected users of deleted issues, at most limit issues per
// call; it returns how many issues it finished.
func (d *DB) PurgeDeletedIssues(ctx context.Context, limit int) (int, error) {
	var ids []uuid.UUID
	err := d.InSystem(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT issue_id FROM deleted_issues ORDER BY deleted_at LIMIT $1`, limit)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	if err != nil {
		return 0, err
	}
	for i, id := range ids {
		if _, err := d.deleteAllEvents(ctx, "e.issue_id = $1", id); err != nil {
			return i, fmt.Errorf("purge issue %s: %w", id, err)
		}
		err := d.InSystem(ctx, func(tx pgx.Tx) error {
			for _, sql := range []string{
				`DELETE FROM issue_hourly WHERE issue_id = $1`,
				`DELETE FROM issue_tags WHERE issue_id = $1`,
				`DELETE FROM issue_users WHERE issue_id = $1`,
				`DELETE FROM issue_subjects WHERE issue_id = $1`,
				`DELETE FROM notification_threads WHERE issue_id = $1`,
				`DELETE FROM deleted_issues WHERE issue_id = $1`,
			} {
				if _, err := tx.Exec(ctx, sql, id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return i, fmt.Errorf("purge issue %s: %w", id, err)
		}
	}
	return len(ids), nil
}

// PurgeDeletedOrganizations removes organizations marked deleted once
// PurgeDeletedProjects has removed their projects, at most limit per call.
// Tables without a foreign key to organizations are cleared first; deleting
// the organization row cascades to the rest.
func (d *DB) PurgeDeletedOrganizations(ctx context.Context, limit int) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := d.InSystem(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id FROM organizations o
			WHERE deleted_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.org_id = o.id)
			ORDER BY deleted_at LIMIT $1`, limit)
		if err != nil {
			return err
		}
		if ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		for _, id := range ids {
			for _, sql := range []string{
				`DELETE FROM notification_outbox WHERE org_id = $1`,
				`DELETE FROM upload_chunks WHERE org_id = $1`,
				`DELETE FROM owner_aliases WHERE org_id = $1`,
				`DELETE FROM deleted_issues WHERE org_id = $1`,
				`DELETE FROM organizations WHERE id = $1`,
			} {
				if _, err := tx.Exec(ctx, sql, id); err != nil {
					return fmt.Errorf("purge organization %s: %w", id, err)
				}
			}
		}
		return nil
	})
	return ids, err
}

// ProjectRetention is a project that keeps its data for less time than
// the server.
type ProjectRetention struct {
	ProjectID uuid.UUID
	Days      int
}

// ShortRetentionProjects lists the projects whose organization plan,
// organization setting or project setting keeps data for fewer than
// serverDays days.
func (d *DB) ShortRetentionProjects(ctx context.Context, serverDays int) ([]ProjectRetention, error) {
	var out []ProjectRetention
	err := d.InSystem(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, days FROM (
			  SELECT p.id, least($1::int,
			    nullif(nullif(o.limits ->> 'retention_days', '')::int, 0),
			    nullif(nullif(o.settings ->> 'retention_days', '')::int, 0),
			    nullif(nullif(p.settings -> 'privacy' ->> 'retention_days', '')::int, 0)) AS days
			  FROM projects p JOIN organizations o ON o.id = p.org_id
			  WHERE p.deleted_at IS NULL AND o.deleted_at IS NULL
			) r WHERE days > 0 AND days < $1`, serverDays)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ProjectRetention])
		return err
	})
	return out, err
}

// ApplyProjectRetention removes a project's data older than cutoff, like
// ApplyRetention does for the whole server: events (a batch at a time),
// attachments, feedback, counters, facets, issues not seen since and
// finished check-ins.
func (d *DB) ApplyProjectRetention(ctx context.Context, projectID uuid.UUID, cutoff time.Time) (int64, error) {
	events, err := d.deleteAllEvents(ctx, "e.project_id = $1 AND e.received_at < $2", projectID, cutoff)
	if err != nil {
		return events, err
	}
	err = d.InSystem(ctx, func(tx pgx.Tx) error {
		for _, sql := range []string{
			`DELETE FROM event_attachments WHERE project_id = $1 AND created_at < $2`,
			`DELETE FROM user_feedback WHERE project_id = $1 AND created_at < $2`,
			`DELETE FROM issue_hourly WHERE project_id = $1 AND hour < $2`,
			`DELETE FROM project_hourly WHERE project_id = $1 AND hour < $2`,
			`DELETE FROM customer_hourly WHERE project_id = $1 AND hour < $2`,
			`DELETE FROM issue_tags WHERE project_id = $1 AND last_seen < $2`,
			`DELETE FROM issue_users u USING issues i WHERE i.id = u.issue_id AND i.project_id = $1 AND i.last_seen < $2`,
			`DELETE FROM issue_subjects s USING issues i WHERE i.id = s.issue_id AND i.project_id = $1 AND i.last_seen < $2`,
			`DELETE FROM notification_threads t USING issues i WHERE i.id = t.issue_id AND i.project_id = $1 AND i.last_seen < $2`,
			`DELETE FROM issues WHERE project_id = $1 AND last_seen < $2`,
			`DELETE FROM release_sessions WHERE project_id = $1 AND hour < $2`,
			`DELETE FROM release_session_users WHERE project_id = $1 AND day < $2::date`,
			`DELETE FROM monitor_checkins c USING monitors m
			  WHERE m.id = c.monitor_id AND m.project_id = $1 AND c.created_at < $2 AND c.status <> 'in_progress'`,
		} {
			if _, err := tx.Exec(ctx, sql, projectID, cutoff); err != nil {
				return fmt.Errorf("project retention: %w", err)
			}
		}
		return nil
	})
	return events, err
}
