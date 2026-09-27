package store

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
)

// SessionCounts are the release health counters of one project, release,
// environment and hour.
type SessionCounts struct {
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	Release     string
	Environment string
	Hour        time.Time
	Started     int
	Errored     int
	Crashed     int
	Abnormal    int
}

// SessionUser is a user (hashed session distinct id) seen in a release on a
// day.
type SessionUser struct {
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	Release     string
	Environment string
	Day         time.Time
	Hash        []byte
	Crashed     bool
}

// AddSessionCounts adds counters, locking rows in key order so that
// concurrent writers cannot deadlock.
func AddSessionCounts(ctx context.Context, q Querier, rows []SessionCounts) error {
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.ProjectID != b.ProjectID {
			return a.ProjectID.String() < b.ProjectID.String()
		}
		if a.Release != b.Release {
			return a.Release < b.Release
		}
		if a.Environment != b.Environment {
			return a.Environment < b.Environment
		}
		return a.Hour.Before(b.Hour)
	})
	var (
		org, proj                      = make([]uuid.UUID, len(rows)), make([]uuid.UUID, len(rows))
		rel, env                       = make([]string, len(rows)), make([]string, len(rows))
		hour                           = make([]time.Time, len(rows))
		started, errored, crashed, abn = make([]int32, len(rows)), make([]int32, len(rows)), make([]int32, len(rows)), make([]int32, len(rows))
	)
	for i, r := range rows {
		org[i], proj[i], rel[i], env[i], hour[i] = r.OrgID, r.ProjectID, r.Release, r.Environment, r.Hour
		started[i], errored[i], crashed[i], abn[i] = int32(r.Started), int32(r.Errored), int32(r.Crashed), int32(r.Abnormal)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO release_sessions AS s (org_id, project_id, release, environment, hour, started, errored, crashed, abnormal)
		SELECT * FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::text[], $5::timestamptz[], $6::int[], $7::int[], $8::int[], $9::int[])
		ON CONFLICT (project_id, release, environment, hour) DO UPDATE SET
		  started = s.started + excluded.started, errored = s.errored + excluded.errored,
		  crashed = s.crashed + excluded.crashed, abnormal = s.abnormal + excluded.abnormal`,
		org, proj, rel, env, hour, started, errored, crashed, abn)
	return err
}

// AddSessionUsers records users; a user who crashed once stays crashed for
// that day.
func AddSessionUsers(ctx context.Context, q Querier, rows []SessionUser) error {
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.ProjectID != b.ProjectID {
			return a.ProjectID.String() < b.ProjectID.String()
		}
		if a.Release != b.Release {
			return a.Release < b.Release
		}
		if a.Environment != b.Environment {
			return a.Environment < b.Environment
		}
		if !a.Day.Equal(b.Day) {
			return a.Day.Before(b.Day)
		}
		return string(a.Hash) < string(b.Hash)
	})
	var (
		org, proj = make([]uuid.UUID, len(rows)), make([]uuid.UUID, len(rows))
		rel, env  = make([]string, len(rows)), make([]string, len(rows))
		day       = make([]time.Time, len(rows))
		hash      = make([][]byte, len(rows))
		crashed   = make([]bool, len(rows))
	)
	for i, r := range rows {
		org[i], proj[i], rel[i], env[i], day[i], hash[i], crashed[i] = r.OrgID, r.ProjectID, r.Release, r.Environment, r.Day, r.Hash, r.Crashed
	}
	_, err := q.Exec(ctx, `
		INSERT INTO release_session_users AS u (org_id, project_id, release, environment, day, user_hash, crashed)
		SELECT * FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::text[], $5::date[], $6::bytea[], $7::bool[])
		ON CONFLICT (project_id, release, environment, day, user_hash) DO UPDATE SET crashed = u.crashed OR excluded.crashed
		WHERE excluded.crashed AND NOT u.crashed`,
		org, proj, rel, env, day, hash, crashed)
	return err
}

// ReleaseSeen is a release (and environment) that reported sessions.
type ReleaseSeen struct {
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	Release     string
	Environment string
	At          time.Time
}

// TouchReleases makes releases and environments that only reported sessions
// appear in release lists.
func TouchReleases(ctx context.Context, q Querier, seen []ReleaseSeen) error {
	if len(seen) == 0 {
		return nil
	}
	sort.Slice(seen, func(i, j int) bool {
		a, b := seen[i], seen[j]
		if a.ProjectID != b.ProjectID {
			return a.ProjectID.String() < b.ProjectID.String()
		}
		if a.Release != b.Release {
			return a.Release < b.Release
		}
		return a.Environment < b.Environment
	})
	var (
		org, proj = make([]uuid.UUID, len(seen)), make([]uuid.UUID, len(seen))
		rel, env  = make([]string, len(seen)), make([]string, len(seen))
		at        = make([]time.Time, len(seen))
	)
	for i, s := range seen {
		org[i], proj[i], rel[i], env[i], at[i] = s.OrgID, s.ProjectID, s.Release, s.Environment, s.At
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO releases AS r (org_id, project_id, version, first_seen, last_seen)
		SELECT o, p, v, min(t), max(t) FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::timestamptz[]) AS x(o, p, v, t)
		GROUP BY o, p, v ORDER BY p, v
		ON CONFLICT (project_id, version) DO UPDATE SET
		  first_seen = least(r.first_seen, excluded.first_seen), last_seen = greatest(r.last_seen, excluded.last_seen)`,
		org, proj, rel, at); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO environments AS e (org_id, project_id, name, first_seen, last_seen)
		SELECT o, p, n, min(t), max(t) FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::timestamptz[]) AS x(o, p, n, t)
		WHERE n <> '' GROUP BY o, p, n ORDER BY p, n
		ON CONFLICT (project_id, name) DO UPDATE SET last_seen = greatest(e.last_seen, excluded.last_seen)`,
		org, proj, env, at)
	return err
}

// Health is the release health of one release over a window.
type Health struct {
	Sessions, Errored, Crashed, Abnormal int64
	Users, CrashedUsers                  int64
}

// ReleaseHealth returns the health of the given releases of a project since
// a time, and the project's total sessions in that window (for adoption).
func ReleaseHealth(ctx context.Context, q Querier, projectID uuid.UUID, versions []string, since time.Time) (map[string]*Health, int64, error) {
	out := map[string]*Health{}
	var total int64
	if err := q.QueryRow(ctx, `SELECT coalesce(sum(started), 0) FROM release_sessions WHERE project_id = $1 AND hour >= $2`,
		projectID, since).Scan(&total); err != nil {
		return nil, 0, err
	}
	if len(versions) == 0 || total == 0 {
		return out, total, nil
	}
	rows, err := q.Query(ctx, `
		SELECT release, sum(started), sum(errored), sum(crashed), sum(abnormal)
		FROM release_sessions WHERE project_id = $1 AND release = ANY($2) AND hour >= $3
		GROUP BY release`, projectID, versions, since)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var v string
		var h Health
		if err := rows.Scan(&v, &h.Sessions, &h.Errored, &h.Crashed, &h.Abnormal); err != nil {
			rows.Close()
			return nil, 0, err
		}
		out[v] = &h
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows, err = q.Query(ctx, `
		SELECT release, count(*), count(*) FILTER (WHERE crashed) FROM (
		  SELECT release, user_hash, bool_or(crashed) AS crashed FROM release_session_users
		  WHERE project_id = $1 AND release = ANY($2) AND day >= $3::date
		  GROUP BY release, user_hash
		) u GROUP BY release`, projectID, versions, since)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		var users, crashed int64
		if err := rows.Scan(&v, &users, &crashed); err != nil {
			return nil, 0, err
		}
		if h := out[v]; h != nil {
			h.Users, h.CrashedUsers = users, crashed
		}
	}
	return out, total, rows.Err()
}
