package api

import (
	"context"
	"sort"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
)

// GetFilterStats implements ProjectService.GetFilterStats.
func (s *ProjectServer) GetFilterStats(ctx context.Context, req *connect.Request[apiv1.GetFilterStatsRequest]) (*connect.Response[apiv1.GetFilterStatsResponse], error) {
	var out apiv1.GetFilterStatsResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		since := time.Now().UTC().AddDate(0, 0, -29).Format("2006-01-02")
		rows, err := tx.Query(ctx, `SELECT day::text, reason, count FROM project_filtered_daily
			WHERE project_id = $1 AND day >= $2::date ORDER BY day`, p.ID, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		byReason := map[apiv1.FilterReason]uint64{}
		byDay := map[string]uint64{}
		var days []string
		for rows.Next() {
			var day string
			var reason int16
			var n int64
			if err := rows.Scan(&day, &reason, &n); err != nil {
				return err
			}
			if _, ok := byDay[day]; !ok {
				days = append(days, day)
			}
			byReason[apiv1.FilterReason(reason)] += uint64(n)
			byDay[day] += uint64(n)
			out.Total += uint64(n)
		}
		for r, n := range byReason {
			out.Reasons = append(out.Reasons, &apiv1.FilterStat{Reason: r, Count: n})
		}
		sort.Slice(out.Reasons, func(i, j int) bool {
			if out.Reasons[i].Count != out.Reasons[j].Count {
				return out.Reasons[i].Count > out.Reasons[j].Count
			}
			return out.Reasons[i].Reason < out.Reasons[j].Reason
		})
		for _, d := range days {
			out.Days = append(out.Days, &apiv1.FilterDay{Day: d, Count: byDay[d]})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "filter stats", err)
	}
	return connect.NewResponse(&out), nil
}
