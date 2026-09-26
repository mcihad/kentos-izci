package api

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
)

// window is the bucket layout of a stats period.
type window struct {
	start    time.Time
	interval time.Duration
	buckets  int
	// minutes is true for the last-hour window, whose buckets come from the
	// events table instead of the hourly counters.
	minutes bool
}

func (w window) end() time.Time { return w.start.Add(w.interval * time.Duration(w.buckets)) }

// periodWindow computes buckets ending at now, with day boundaries in loc.
func periodWindow(p apiv1.StatsPeriod, now time.Time, loc *time.Location) window {
	if loc == nil {
		loc = time.UTC
	}
	switch p {
	case apiv1.StatsPeriod_STATS_PERIOD_LAST_HOUR:
		end := now.Truncate(time.Minute).Add(time.Minute)
		return window{start: end.Add(-60 * time.Minute), interval: time.Minute, buckets: 60, minutes: true}
	case apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS:
		end := now.Truncate(time.Hour).Add(time.Hour)
		for end.Hour()%6 != 0 {
			end = end.Add(time.Hour)
		}
		return window{start: end.Add(-28 * 6 * time.Hour), interval: 6 * time.Hour, buckets: 28}
	case apiv1.StatsPeriod_STATS_PERIOD_LAST_14_DAYS, apiv1.StatsPeriod_STATS_PERIOD_LAST_30_DAYS, apiv1.StatsPeriod_STATS_PERIOD_LAST_90_DAYS:
		days := map[apiv1.StatsPeriod]int{
			apiv1.StatsPeriod_STATS_PERIOD_LAST_14_DAYS: 14, apiv1.StatsPeriod_STATS_PERIOD_LAST_30_DAYS: 30, apiv1.StatsPeriod_STATS_PERIOD_LAST_90_DAYS: 90,
		}[p]
		local := now.In(loc)
		midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		return window{start: midnight.AddDate(0, 0, -(days - 1)), interval: 24 * time.Hour, buckets: days}
	}
	end := now.Truncate(time.Hour).Add(time.Hour)
	return window{start: end.Add(-24 * time.Hour), interval: time.Hour, buckets: 24}
}

func (w window) series(counts []uint64) *apiv1.Series {
	return &apiv1.Series{Start: ts(w.start), Interval: durationpb.New(w.interval), Counts: counts}
}

// bucketIndex maps a time to its bucket (-1 when outside).
func (w window) bucketIndex(t time.Time) int {
	if t.Before(w.start) {
		return -1
	}
	i := int(t.Sub(w.start) / w.interval)
	if i >= w.buckets {
		return -1
	}
	return i
}

// issueSeries loads per-issue series and period totals for a page of
// issues.
func issueSeries(ctx context.Context, tx pgx.Tx, w window, ids []uuid.UUID) (map[uuid.UUID][]uint64, error) {
	out := make(map[uuid.UUID][]uint64, len(ids))
	for _, id := range ids {
		out[id] = make([]uint64, w.buckets)
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows pgx.Rows
	var err error
	if w.minutes {
		rows, err = tx.Query(ctx, `
			SELECT issue_id, date_trunc('minute', occurred_at), count(*) FROM events
			WHERE issue_id = ANY($1) AND received_at >= $2::timestamptz - interval '5 minutes' AND occurred_at >= $2 AND occurred_at < $3
			GROUP BY 1, 2`, ids, w.start, w.end())
	} else {
		rows, err = tx.Query(ctx, `
			SELECT issue_id, hour, count FROM issue_hourly
			WHERE issue_id = ANY($1) AND hour >= $2 AND hour < $3`, ids, w.start.Truncate(time.Hour), w.end())
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var at time.Time
		var n int64
		if err := rows.Scan(&id, &at, &n); err != nil {
			return nil, err
		}
		if i := w.bucketIndex(at); i >= 0 {
			out[id][i] += uint64(n)
		}
	}
	return out, rows.Err()
}

func sum(counts []uint64) uint64 {
	var s uint64
	for _, c := range counts {
		s += c
	}
	return s
}

// orgLocation returns the organization's timezone.
func orgLocation(org *store.Organization) *time.Location {
	if tz := org.Settings.GetTimezone(); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}
