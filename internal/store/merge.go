package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

// Merge and unmerge errors.
var (
	ErrMergeProjects = errors.New("issues of different projects cannot be merged")
	ErrMergeSelf     = errors.New("an issue cannot be merged into itself")
)

// MergeSource is an issue merged by MergeIssues.
type MergeSource struct {
	ID       uuid.UUID
	ShortSeq int32
}

type mergeIssue struct {
	id, project uuid.UUID
	fingerprint []byte
	seq         int32
}

// Activity kinds moved to the target of a merge: what members wrote or
// made for the issue (comments, AI analyses, branches, pull requests,
// tickets). The merged issue's own history goes with it.
var mergedActivity = []int16{
	int16(apiv1.ActivityKind_ACTIVITY_KIND_COMMENT), int16(apiv1.ActivityKind_ACTIVITY_KIND_AI_ANALYSIS),
	int16(apiv1.ActivityKind_ACTIVITY_KIND_BRANCH_CREATED), int16(apiv1.ActivityKind_ACTIVITY_KIND_PULL_REQUEST_OPENED),
	int16(apiv1.ActivityKind_ACTIVITY_KIND_TICKET_CREATED),
}

// MergeIssues merges sources into target. Counters, facets, affected users
// and subjects are combined; comments, feedback, followers, inbox entries,
// AI runs and tickets move; new events with a source's fingerprint join the
// target (merged_hashes). The sources' events are queued for
// ingest.ProcessIssueMoves and the sources deleted. ErrNotFound when an
// issue is unknown.
func MergeIssues(ctx context.Context, q Querier, orgID, target uuid.UUID, sources []uuid.UUID, by uuid.UUID) ([]MergeSource, error) {
	all := append([]uuid.UUID{target}, sources...)
	slices.SortFunc(all, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	all = slices.Compact(all)
	rows, err := q.Query(ctx, `SELECT id, project_id, fingerprint, coalesce(short_seq, 0) FROM issues WHERE id = ANY($1) ORDER BY id FOR UPDATE`, all)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]*mergeIssue{}
	for rows.Next() {
		var m mergeIssue
		if err := rows.Scan(&m.id, &m.project, &m.fingerprint, &m.seq); err != nil {
			rows.Close()
			return nil, err
		}
		byID[m.id] = &m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	t := byID[target]
	if t == nil {
		return nil, ErrNotFound
	}
	var out []MergeSource
	for _, id := range sources {
		s := byID[id]
		switch {
		case id == target:
			return nil, ErrMergeSelf
		case s == nil:
			return nil, ErrNotFound
		case s.project != t.project:
			return nil, ErrMergeProjects
		}
		if slices.ContainsFunc(out, func(m MergeSource) bool { return m.ID == id }) {
			continue
		}
		if err := mergeOne(ctx, q, orgID, t, s, by); err != nil {
			return nil, fmt.Errorf("merge issue %s: %w", id, err)
		}
		out = append(out, MergeSource{ID: s.id, ShortSeq: s.seq})
	}
	_, err = q.Exec(ctx, `
		UPDATE issues SET users_count = (SELECT count(*) FROM issue_users WHERE issue_id = $1),
		  customers_count = (SELECT count(*) FROM issue_tags WHERE issue_id = $1 AND key = 'customer'), updated_at = now()
		WHERE id = $1`, target)
	return out, err
}

func mergeOne(ctx context.Context, q Querier, orgID uuid.UUID, t, s *mergeIssue, by uuid.UUID) error {
	steps := []struct {
		sql  string
		args []any
	}{
		// Fingerprints merged into the source now lead to the target, and so
		// does the source's own.
		{`UPDATE merged_hashes SET issue_id = $1 WHERE issue_id = $2`, []any{t.id, s.id}},
		{`INSERT INTO merged_hashes (project_id, hash, issue_id, org_id, source_issue_id, source_short_seq, source, merged_by)
		  SELECT project_id, fingerprint, $1, org_id, id, short_seq,
		    jsonb_build_object('title', title, 'culprit', culprit, 'type', type, 'value', value, 'platform', platform,
		      'grouping', grouping, 'metadata', metadata, 'level', level),
		    $3
		  FROM issues WHERE id = $2
		  ON CONFLICT (project_id, hash) DO UPDATE SET issue_id = excluded.issue_id`, []any{t.id, s.id, by}},
		{`UPDATE issues a SET times_seen = a.times_seen + b.times_seen,
		    first_seen = least(a.first_seen, b.first_seen), last_seen = greatest(a.last_seen, b.last_seen),
		    level = greatest(a.level, b.level), unhandled = a.unhandled OR b.unhandled,
		    first_release = CASE WHEN b.first_seen < a.first_seen THEN b.first_release ELSE a.first_release END,
		    last_release = CASE WHEN b.last_seen > a.last_seen THEN b.last_release ELSE a.last_release END,
		    last_event_id = CASE WHEN b.last_seen > a.last_seen THEN b.last_event_id ELSE a.last_event_id END,
		    updated_at = now()
		  FROM issues b WHERE a.id = $1 AND b.id = $2`, []any{t.id, s.id}},
		{`INSERT INTO issue_hourly AS h (issue_id, hour, org_id, project_id, count)
		  SELECT $1, hour, org_id, project_id, count FROM issue_hourly WHERE issue_id = $2
		  ON CONFLICT (issue_id, hour) DO UPDATE SET count = h.count + excluded.count`, []any{t.id, s.id}},
		{`INSERT INTO issue_tags AS g (issue_id, key, value, org_id, project_id, count, first_seen, last_seen)
		  SELECT $1, key, value, org_id, project_id, count, first_seen, last_seen FROM issue_tags WHERE issue_id = $2
		  ON CONFLICT (issue_id, key, value) DO UPDATE SET count = g.count + excluded.count,
		    first_seen = least(g.first_seen, excluded.first_seen), last_seen = greatest(g.last_seen, excluded.last_seen)`, []any{t.id, s.id}},
		{`INSERT INTO issue_users (issue_id, user_hash, org_id) SELECT $1, user_hash, org_id FROM issue_users WHERE issue_id = $2
		  ON CONFLICT DO NOTHING`, []any{t.id, s.id}},
		{`INSERT INTO issue_subjects (issue_id, key_hash, org_id) SELECT $1, key_hash, org_id FROM issue_subjects WHERE issue_id = $2
		  ON CONFLICT DO NOTHING`, []any{t.id, s.id}},
		{`INSERT INTO issue_subscriptions (org_id, issue_id, user_id) SELECT org_id, $1, user_id FROM issue_subscriptions WHERE issue_id = $2
		  ON CONFLICT DO NOTHING`, []any{t.id, s.id}},
		{`UPDATE user_feedback SET issue_id = $1 WHERE issue_id = $2`, []any{t.id, s.id}},
		{`UPDATE issue_activity SET issue_id = $1 WHERE issue_id = $2 AND kind = ANY($3)`, []any{t.id, s.id, mergedActivity}},
		{`UPDATE user_notifications SET issue_id = $1 WHERE issue_id = $2`, []any{t.id, s.id}},
		{`UPDATE ai_runs SET issue_id = $1 WHERE issue_id = $2`, []any{t.id, s.id}},
		// Message threads and tickets move unless the target has its own on
		// the channel.
		{`UPDATE notification_threads x SET issue_id = $1 WHERE issue_id = $2
		  AND NOT EXISTS (SELECT 1 FROM notification_threads y WHERE y.channel_id = x.channel_id AND y.issue_id = $1)`, []any{t.id, s.id}},
		{`DELETE FROM notification_outbox WHERE issue_id = $1 AND sent_at IS NULL`, []any{s.id}},
		{`INSERT INTO issue_moves (org_id, project_id, from_issue, to_issue, hash) VALUES ($1, $2, $3, $4, $5)`,
			[]any{orgID, s.project, s.id, t.id, s.fingerprint}},
	}
	for _, st := range steps {
		if _, err := q.Exec(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	// What has no foreign key to issues goes with the source.
	for _, sql := range []string{
		`DELETE FROM issue_hourly WHERE issue_id = $1`,
		`DELETE FROM issue_tags WHERE issue_id = $1`,
		`DELETE FROM issue_users WHERE issue_id = $1`,
		`DELETE FROM issue_subjects WHERE issue_id = $1`,
		`DELETE FROM notification_threads WHERE issue_id = $1`,
		`DELETE FROM issues WHERE id = $1`,
	} {
		if _, err := q.Exec(ctx, sql, s.id); err != nil {
			return err
		}
	}
	return nil
}

// MergedIssue is a fingerprint merged into an issue.
type MergedIssue struct {
	Hash     []byte
	ShortSeq int32
	Title    string
	MergedBy *uuid.UUID
	MergedAt time.Time
}

// ListMergedIssues lists the fingerprints merged into an issue, newest
// first.
func ListMergedIssues(ctx context.Context, q Querier, issueID uuid.UUID) ([]MergedIssue, error) {
	rows, err := q.Query(ctx, `
		SELECT hash, coalesce(source_short_seq, 0), coalesce(source ->> 'title', ''), merged_by, merged_at
		FROM merged_hashes WHERE issue_id = $1 ORDER BY merged_at DESC LIMIT 200`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MergedIssue
	for rows.Next() {
		var m MergedIssue
		if err := rows.Scan(&m.Hash, &m.ShortSeq, &m.Title, &m.MergedBy, &m.MergedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MergedInto returns the issue a merged issue (by id, or by project and
// short sequence) now lives in; ErrNotFound when it was never merged.
func MergedInto(ctx context.Context, q Querier, id *uuid.UUID, projectID *uuid.UUID, seq int32) (uuid.UUID, error) {
	var target uuid.UUID
	var err error
	if id != nil {
		err = q.QueryRow(ctx, `SELECT issue_id FROM merged_hashes WHERE source_issue_id = $1 LIMIT 1`, *id).Scan(&target)
	} else {
		err = q.QueryRow(ctx, `SELECT issue_id FROM merged_hashes WHERE project_id = $1 AND source_short_seq = $2 LIMIT 1`,
			*projectID, seq).Scan(&target)
	}
	return target, notFound(err)
}

// UnmergeIssue moves a fingerprint merged into issueID out into a new
// issue, which gets back the short id, title and grouping the merged issue
// had. Events are queued for ingest.ProcessIssueMoves, which recomputes
// both issues afterwards. ErrNotFound when the fingerprint is not merged
// into the issue.
func UnmergeIssue(ctx context.Context, q Querier, orgID, issueID uuid.UUID, hash []byte) (uuid.UUID, error) {
	var project uuid.UUID
	var seq *int32
	var source []byte
	err := q.QueryRow(ctx, `
		DELETE FROM merged_hashes WHERE issue_id = $1 AND hash = $2
		RETURNING project_id, source_short_seq, source`, issueID, hash).Scan(&project, &seq, &source)
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	// The short id comes back unless another issue took it meanwhile.
	if seq != nil {
		var taken bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issues WHERE project_id = $1 AND short_seq = $2)`, project, *seq).
			Scan(&taken); err != nil {
			return uuid.Nil, err
		}
		if taken {
			seq = nil
		}
	}
	if seq == nil {
		var next int32
		if err := q.QueryRow(ctx, `UPDATE projects SET issue_seq = issue_seq + 1 WHERE id = $1 RETURNING issue_seq`, project).
			Scan(&next); err != nil {
			return uuid.Nil, err
		}
		seq = &next
	}
	var id uuid.UUID
	err = q.QueryRow(ctx, `
		INSERT INTO issues (org_id, project_id, short_seq, fingerprint, grouping, title, culprit, type, value, platform,
		                    level, first_seen, last_seen, times_seen, metadata)
		SELECT a.org_id, a.project_id, $3, $4, coalesce(s -> 'grouping', '{}'), coalesce(s ->> 'title', a.title),
		       coalesce(s ->> 'culprit', ''), coalesce(s ->> 'type', ''), coalesce(s ->> 'value', ''),
		       coalesce(s ->> 'platform', a.platform), coalesce((s ->> 'level')::smallint, a.level),
		       a.first_seen, a.last_seen, 0, coalesce(s -> 'metadata', '{}')
		FROM issues a, (SELECT $5::jsonb AS s) src WHERE a.id = $1 AND a.project_id = $2
		RETURNING id`, issueID, project, *seq, hash, source).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	_, err = q.Exec(ctx, `INSERT INTO issue_moves (org_id, project_id, from_issue, to_issue, hash, unmerge) VALUES ($1, $2, $3, $4, $5, true)`,
		orgID, project, issueID, id, hash)
	return id, err
}
