package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/mcihad/kentos-izci/internal/db"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// moveBatch is how many events one statement of an issue move updates.
const moveBatch = 5000

// ProcessIssueMoves moves events between issues after merges and unmerges
// (issue_moves), oldest request first and one at a time, so that a merge
// and a later unmerge of the same fingerprint apply in order. After an
// unmerge it recomputes both issues from their events. One server instance
// works at a time; it returns how many requests it finished.
func ProcessIssueMoves(ctx context.Context, database *db.DB, limit int) (int, error) {
	conn, err := database.Pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('izci:issue-moves'))`).Scan(&locked); err != nil || !locked {
		return 0, err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('izci:issue-moves'))`)
	}()

	done := 0
	for done < limit {
		type move struct {
			id, org, project, from, to uuid.UUID
			hash                       []byte
			unmerge, gone              bool
		}
		var m move
		err := database.InSystem(ctx, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT id, org_id, project_id, from_issue, to_issue, hash, unmerge,
				       NOT EXISTS (SELECT 1 FROM issues i WHERE i.id = to_issue)
				FROM issue_moves ORDER BY created_at, id LIMIT 1`).
				Scan(&m.id, &m.org, &m.project, &m.from, &m.to, &m.hash, &m.unmerge, &m.gone)
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return done, nil
		}
		if err != nil {
			return done, err
		}
		if m.gone {
			// The target was deleted meanwhile: the events to move go too.
			err := database.InSystem(ctx, func(tx pgx.Tx) error {
				if !m.unmerge {
					if _, err := tx.Exec(ctx, `INSERT INTO deleted_issues (issue_id, org_id, project_id) VALUES ($1, $2, $3)
						ON CONFLICT DO NOTHING`, m.from, m.org, m.project); err != nil {
						return err
					}
				}
				_, err := tx.Exec(ctx, `DELETE FROM issue_moves WHERE id = $1`, m.id)
				return err
			})
			if err != nil {
				return done, err
			}
			done++
			continue
		}
		for {
			var n int64
			err := database.InSystem(ctx, func(tx pgx.Tx) error {
				var tag interface{ RowsAffected() int64 }
				var err error
				if m.unmerge {
					// The moved events now carry their new issue's own
					// fingerprint.
					tag, err = tx.Exec(ctx, `
						WITH v AS (SELECT id, received_at FROM events WHERE issue_id = $1 AND grouping_hash = $3 LIMIT `+fmt.Sprint(moveBatch)+`)
						UPDATE events e SET issue_id = $2, grouping_hash = NULL FROM v WHERE e.id = v.id AND e.received_at = v.received_at`,
						m.from, m.to, m.hash)
				} else {
					tag, err = tx.Exec(ctx, `
						WITH v AS (SELECT id, received_at FROM events WHERE issue_id = $1 LIMIT `+fmt.Sprint(moveBatch)+`)
						UPDATE events e SET issue_id = $2, grouping_hash = coalesce(e.grouping_hash, $3) FROM v
						WHERE e.id = v.id AND e.received_at = v.received_at`, m.from, m.to, m.hash)
				}
				if err == nil {
					n = tag.RowsAffected()
				}
				return err
			})
			if err != nil {
				return done, fmt.Errorf("move events of issue %s: %w", m.from, err)
			}
			if n < moveBatch {
				break
			}
		}
		if m.unmerge {
			err := database.InSystem(ctx, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
					UPDATE user_feedback f SET issue_id = e.issue_id FROM events e
					WHERE f.issue_id = $1 AND f.project_id = e.project_id AND f.event_id = e.event_id AND e.issue_id = $2`, m.from, m.to)
				return err
			})
			if err != nil {
				return done, err
			}
			for _, id := range []uuid.UUID{m.to, m.from} {
				if err := RecomputeIssue(ctx, database, id); err != nil {
					return done, fmt.Errorf("recompute issue %s: %w", id, err)
				}
			}
		}
		if err := database.InSystem(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM issue_moves WHERE id = $1`, m.id)
			return err
		}); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// tagStat is the distribution entry of one tag value.
type tagStat struct {
	count       int64
	first, last time.Time
}

// issueStats accumulates what RecomputeIssue rebuilds.
type issueStats struct {
	count              int64
	first, last        time.Time
	lastEvent          uuid.UUID
	firstRel, lastRel  string
	level              int16
	unhandled          bool
	hours              map[time.Time]int64
	tags               map[[2]string]*tagStat
	users, subjects    map[string]bool
	cursorAt           time.Time
	cursorID           uuid.UUID
	orgID, projectID   uuid.UUID
	scanned, malformed int
}

func (s *issueStats) add(id uuid.UUID, receivedAt, occurredAt time.Time, level int16, unhandled bool, p *Processed, ev *ingestv1.Event) {
	if s.count == 0 || occurredAt.Before(s.first) {
		s.first, s.firstRel = occurredAt, p.Release
	}
	if s.count == 0 || !occurredAt.Before(s.last) {
		s.last, s.lastRel, s.lastEvent = occurredAt, p.Release, id
	}
	s.count++
	s.level = max(s.level, level)
	s.unhandled = s.unhandled || unhandled
	s.hours[occurredAt.Truncate(time.Hour)]++
	p.Level = ingestv1.Level(level)
	for k, v := range facetTags(ev, p) {
		a := s.tags[[2]string{k, v}]
		if a == nil {
			a = &tagStat{first: occurredAt, last: occurredAt}
			s.tags[[2]string{k, v}] = a
		}
		a.count++
		if occurredAt.Before(a.first) {
			a.first = occurredAt
		}
		if occurredAt.After(a.last) {
			a.last = occurredAt
		}
	}
	if h := UserHash(ev.GetUser()); len(h) > 0 {
		s.users[string(h)] = true
	}
	for _, k := range SubjectKeys(ev.GetUser(), p.ClientIP) {
		s.subjects[string(SubjectKeyHash(k))] = true
	}
	if receivedAt.After(s.cursorAt) || (receivedAt.Equal(s.cursorAt) && id.String() > s.cursorID.String()) {
		s.cursorAt, s.cursorID = receivedAt, id
	}
}

// scan adds the issue's events after the cursor, a page at a time.
func (s *issueStats) scan(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, issueID uuid.UUID) error {
	for {
		rows, err := q.Query(ctx, `
			SELECT id, received_at, occurred_at, level, unhandled, environment, release, customer_key, tenant_key,
			       user_label, browser, os, server_name, transaction, coalesce(host(client_ip), ''), payload
			FROM events WHERE issue_id = $1 AND (received_at, id) > ($2, $3)
			ORDER BY received_at, id LIMIT 1000`, issueID, s.cursorAt, s.cursorID)
		if err != nil {
			return err
		}
		n := 0
		for rows.Next() {
			var (
				id        uuid.UUID
				recv, occ time.Time
				level     int16
				unhandled bool
				p         Processed
				ip        string
				payload   []byte
			)
			if err := rows.Scan(&id, &recv, &occ, &level, &unhandled, &p.Environment, &p.Release, &p.CustomerKey, &p.TenantKey,
				&p.UserLabel, &p.Browser, &p.OS, &p.ServerName, &p.Transaction, &ip, &payload); err != nil {
				rows.Close()
				return err
			}
			n++
			var ev ingestv1.Event
			if err := protojson.Unmarshal(payload, &ev); err != nil {
				s.malformed++
			}
			p.ClientIP, _ = netip.ParseAddr(ip)
			p.UA = ParseUserAgent(UserAgentOf(&ev))
			s.add(id, recv, occ, level, unhandled, &p, &ev)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		s.scanned += n
		if n < 1000 {
			return nil
		}
	}
}

// RecomputeIssue rebuilds an issue's counters, hourly series, facets,
// affected users and subjects from its stored events. Events stored while
// it scans are counted too: the final step locks the issue (which ingest
// also locks) and adds what arrived meanwhile.
func RecomputeIssue(ctx context.Context, database *db.DB, issueID uuid.UUID) error {
	s := &issueStats{hours: map[time.Time]int64{}, tags: map[[2]string]*tagStat{}, users: map[string]bool{}, subjects: map[string]bool{}}
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT org_id, project_id FROM issues WHERE id = $1`, issueID).Scan(&s.orgID, &s.projectID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := database.InSystem(ctx, func(tx pgx.Tx) error { return s.scan(ctx, tx, issueID) }); err != nil {
		return err
	}
	return database.InSystem(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM issues WHERE id = $1 FOR UPDATE`, issueID); err != nil {
			return err
		}
		if err := s.scan(ctx, tx, issueID); err != nil {
			return err
		}
		customers := 0
		for k := range s.tags {
			if k[0] == "customer" {
				customers++
			}
		}
		if s.count > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE issues SET times_seen = $2, first_seen = $3, last_seen = $4, last_event_id = $5, first_release = $6,
				  last_release = $7, level = $8, unhandled = $9, users_count = $10, customers_count = $11, updated_at = now()
				WHERE id = $1`, issueID, s.count, s.first, s.last, s.lastEvent, s.firstRel, s.lastRel, s.level, s.unhandled,
				len(s.users), customers); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE issues SET times_seen = 0, users_count = 0, customers_count = 0 WHERE id = $1`, issueID); err != nil {
			return err
		}
		for _, sql := range []string{
			`DELETE FROM issue_hourly WHERE issue_id = $1`,
			`DELETE FROM issue_tags WHERE issue_id = $1`,
			`DELETE FROM issue_users WHERE issue_id = $1`,
			`DELETE FROM issue_subjects WHERE issue_id = $1`,
		} {
			if _, err := tx.Exec(ctx, sql, issueID); err != nil {
				return err
			}
		}
		var hours []time.Time
		var counts []int64
		for h, c := range s.hours {
			hours, counts = append(hours, h), append(counts, c)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO issue_hourly (issue_id, hour, org_id, project_id, count)
			SELECT $1, h, $2, $3, c FROM unnest($4::timestamptz[], $5::bigint[]) AS t(h, c)`,
			issueID, s.orgID, s.projectID, hours, counts); err != nil {
			return err
		}
		var keys, values []string
		var tagCounts []int64
		var firsts, lasts []time.Time
		for k, a := range s.tags {
			keys, values, tagCounts = append(keys, k[0]), append(values, k[1]), append(tagCounts, a.count)
			firsts, lasts = append(firsts, a.first), append(lasts, a.last)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO issue_tags (issue_id, key, value, org_id, project_id, count, first_seen, last_seen)
			SELECT $1, k, v, $2, $3, c, f, l FROM unnest($4::text[], $5::text[], $6::bigint[], $7::timestamptz[], $8::timestamptz[])
			  AS t(k, v, c, f, l)`, issueID, s.orgID, s.projectID, keys, values, tagCounts, firsts, lasts); err != nil {
			return err
		}
		for table, set := range map[string]map[string]bool{"issue_users (issue_id, user_hash, org_id)": s.users,
			"issue_subjects (issue_id, key_hash, org_id)": s.subjects} {
			var hashes [][]byte
			for h := range set {
				hashes = append(hashes, []byte(h))
			}
			if _, err := tx.Exec(ctx, `INSERT INTO `+table+` SELECT $1, h, $2 FROM unnest($3::bytea[]) AS t(h)`,
				issueID, s.orgID, hashes); err != nil {
				return err
			}
		}
		return nil
	})
}
