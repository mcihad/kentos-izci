package api

import (
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/realtime"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// DashboardServer implements izci.api.v1.DashboardService.
type DashboardServer struct {
	*base
	apiv1connect.UnimplementedDashboardServiceHandler
}

var _ apiv1connect.DashboardServiceHandler = (*DashboardServer)(nil)

// liveHeartbeat is the keep-alive interval of WatchLive.
var liveHeartbeat = 20 * time.Second

// overviewScope is the resolved filter of an overview request.
type overviewScope struct {
	orgID     uuid.UUID
	projects  []uuid.UUID // nil = all
	envs      []string
	customers []string
}

// issueConds restricts issues AS i (joined with projects AS p).
func (sc overviewScope) issueConds(b *sqlb) {
	b.add("i.org_id = " + b.arg(sc.orgID))
	b.add("p.deleted_at IS NULL")
	if sc.projects != nil {
		b.add("i.project_id = ANY(" + b.arg(sc.projects) + ")")
	}
	if len(sc.envs) > 0 {
		b.add("EXISTS (SELECT 1 FROM issue_tags t WHERE t.issue_id = i.id AND t.key = 'environment' AND t.value = ANY(" + b.arg(sc.envs) + "))")
	}
	if len(sc.customers) > 0 {
		b.add("EXISTS (SELECT 1 FROM issue_tags t WHERE t.issue_id = i.id AND t.key = 'customer' AND t.value = ANY(" + b.arg(sc.customers) + "))")
	}
}

// eventConds restricts events AS e.
func (sc overviewScope) eventConds(b *sqlb) {
	b.add("e.org_id = " + b.arg(sc.orgID))
	if sc.projects != nil {
		b.add("e.project_id = ANY(" + b.arg(sc.projects) + ")")
	}
	if len(sc.envs) > 0 {
		b.add("e.environment = ANY(" + b.arg(sc.envs) + ")")
	}
	if len(sc.customers) > 0 {
		b.add("e.customer_key = ANY(" + b.arg(sc.customers) + ")")
	}
}

// fromEvents reports whether statistics must be computed from the events
// table: counters have neither environment nor customer-by-level
// dimensions, nor minute resolution.
func (sc overviewScope) fromEvents(w window) bool {
	return w.minutes || len(sc.envs) > 0 || len(sc.customers) > 0
}

// volume is the event volume of a window broken down by project and level.
type volume struct {
	byLevel   map[ingestv1.Level][]uint64
	byProject map[uuid.UUID][]uint64
	total     uint64
	previous  uint64
}

func (s *DashboardServer) volume(ctx context.Context, tx pgx.Tx, sc overviewScope, w window) (*volume, error) {
	v := &volume{byLevel: map[ingestv1.Level][]uint64{}, byProject: map[uuid.UUID][]uint64{}}
	prevStart := w.start.Add(-w.end().Sub(w.start))
	b := &sqlb{}
	var sql, prevSQL string
	var args []any
	if sc.fromEvents(w) {
		sc.eventConds(b)
		unit := "hour"
		if w.minutes {
			unit = "minute"
		}
		start := b.arg(w.start)
		where := b.clause()
		sql = `SELECT e.project_id, date_trunc('` + unit + `', e.occurred_at), e.level, count(*) FROM events e
			WHERE ` + where + ` AND e.received_at >= ` + start + `::timestamptz - interval '1 hour' AND e.occurred_at >= ` + start + `
			GROUP BY 1, 2, 3`
		args = slices.Clone(b.args)
		prev := b.arg(prevStart)
		prevSQL = `SELECT count(*) FROM events e WHERE ` + where + ` AND e.received_at >= ` + prev + `::timestamptz - interval '1 hour'
			AND e.occurred_at >= ` + prev + ` AND e.occurred_at < ` + start
	} else {
		b.add("h.org_id = " + b.arg(sc.orgID))
		if sc.projects != nil {
			b.add("h.project_id = ANY(" + b.arg(sc.projects) + ")")
		}
		start := b.arg(w.start.Truncate(time.Hour))
		where := b.clause()
		sql = `SELECT h.project_id, h.hour, h.level, h.count FROM project_hourly h WHERE ` + where + ` AND h.hour >= ` + start
		args = slices.Clone(b.args)
		prevSQL = `SELECT coalesce(sum(h.count), 0) FROM project_hourly h WHERE ` + where + ` AND h.hour >= ` + b.arg(prevStart.Truncate(time.Hour)) + ` AND h.hour < ` + start
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var project uuid.UUID
		var at time.Time
		var level int16
		var n int64
		if err := rows.Scan(&project, &at, &level, &n); err != nil {
			rows.Close()
			return nil, err
		}
		i := w.bucketIndex(at)
		if i < 0 {
			continue
		}
		l := ingestv1.Level(level)
		if v.byLevel[l] == nil {
			v.byLevel[l] = make([]uint64, w.buckets)
		}
		if v.byProject[project] == nil {
			v.byProject[project] = make([]uint64, w.buckets)
		}
		v.byLevel[l][i] += uint64(n)
		v.byProject[project][i] += uint64(n)
		v.total += uint64(n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var prev int64
	if err := tx.QueryRow(ctx, prevSQL, b.args...).Scan(&prev); err != nil {
		return nil, err
	}
	v.previous = uint64(prev)
	return v, nil
}

// GetOverview implements DashboardService.GetOverview.
func (s *DashboardServer) GetOverview(ctx context.Context, req *connect.Request[apiv1.GetOverviewRequest]) (*connect.Response[apiv1.GetOverviewResponse], error) {
	m := req.Msg
	t := auth.TenantFrom(ctx)
	now := time.Now()
	w := periodWindow(m.GetPeriod(), now, orgLocation(t.Org))
	sc := overviewScope{orgID: t.OrgID(), envs: lower(m.GetEnvironments()), customers: lower(m.GetCustomers())}
	out := &apiv1.GetOverviewResponse{Totals: &apiv1.OverviewTotals{}}
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		type projectInfo struct {
			ref *apiv1.ProjectRef
			id  uuid.UUID
		}
		var projects []projectInfo
		rows, err := tx.Query(ctx, `SELECT id, slug, name, platform FROM projects WHERE org_id = $1 AND deleted_at IS NULL ORDER BY slug`, t.OrgID())
		if err != nil {
			return err
		}
		for rows.Next() {
			var p projectInfo
			p.ref = &apiv1.ProjectRef{}
			if err := rows.Scan(&p.id, &p.ref.Slug, &p.ref.Name, &p.ref.Platform); err != nil {
				rows.Close()
				return err
			}
			p.ref.Id = p.id.String()
			if len(m.GetProjects()) > 0 && !slices.ContainsFunc(m.GetProjects(), func(s string) bool { return strings.EqualFold(s, p.ref.Slug) }) {
				continue
			}
			projects = append(projects, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(m.GetProjects()) > 0 {
			sc.projects = make([]uuid.UUID, len(projects))
			for i, p := range projects {
				sc.projects[i] = p.id
			}
		}

		vol, err := s.volume(ctx, tx, sc, w)
		if err != nil {
			return err
		}
		tot := out.Totals
		tot.Events, tot.PreviousEvents = vol.total, vol.previous
		for _, l := range []ingestv1.Level{ingestv1.Level_LEVEL_FATAL, ingestv1.Level_LEVEL_ERROR, ingestv1.Level_LEVEL_WARNING,
			ingestv1.Level_LEVEL_INFO, ingestv1.Level_LEVEL_DEBUG} {
			if counts := vol.byLevel[l]; counts != nil {
				out.EventsByLevel = append(out.EventsByLevel, &apiv1.LevelSeries{Level: l, Series: w.series(counts)})
			}
		}

		// Recent rate, users and customers.
		eb := &sqlb{}
		sc.eventConds(eb)
		var recent int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM events e WHERE `+eb.clause()+` AND e.received_at >= now() - interval '15 minutes'`,
			eb.args...).Scan(&recent); err != nil {
			return err
		}
		tot.EventsPerMinute = float64(recent) / 15
		if sc.fromEvents(w) {
			start := eb.arg(w.start)
			err = tx.QueryRow(ctx, `SELECT count(DISTINCT e.user_label) FILTER (WHERE e.user_label <> ''),
				count(DISTINCT e.customer_key) FILTER (WHERE e.customer_key <> '')
				FROM events e WHERE `+eb.clause()+` AND e.received_at >= `+start+`::timestamptz - interval '1 hour' AND e.occurred_at >= `+start,
				eb.args...).Scan(&tot.AffectedUsers, &tot.AffectedCustomers)
		} else {
			err = tx.QueryRow(ctx, `SELECT
				(SELECT count(DISTINCT t.value) FROM issue_tags t WHERE t.org_id = $1 AND t.key = 'user' AND t.last_seen >= $2
				   AND ($3::uuid[] IS NULL OR t.project_id = ANY($3))),
				(SELECT count(DISTINCT h.customer_key) FROM customer_hourly h WHERE h.org_id = $1 AND h.hour >= $4
				   AND ($3::uuid[] IS NULL OR h.project_id = ANY($3)))`,
				sc.orgID, w.start, sc.projects, w.start.Truncate(time.Hour)).Scan(&tot.AffectedUsers, &tot.AffectedCustomers)
		}
		if err != nil {
			return err
		}

		// Issue totals.
		ib := &sqlb{}
		sc.issueConds(ib)
		start := ib.arg(w.start)
		where := ib.clause()
		if err := tx.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE i.first_seen >= `+start+`),
			count(*) FILTER (WHERE i.status = 1),
			count(*) FILTER (WHERE EXISTS (SELECT 1 FROM issue_activity a WHERE a.issue_id = i.id AND a.kind = 6 AND a.created_at >= `+start+`))
			FROM issues i JOIN projects p ON p.id = i.project_id WHERE `+where, ib.args...).Scan(
			&tot.NewIssues, &tot.UnresolvedIssues, &tot.RegressedIssues); err != nil {
			return err
		}

		// Trending: largest growth against the previous window of equal length.
		prevStart := w.start.Add(-w.end().Sub(w.start))
		tb := &sqlb{}
		sc.issueConds(tb)
		cur, prev := tb.arg(w.start.Truncate(time.Hour)), tb.arg(prevStart.Truncate(time.Hour))
		trendRows, err := tx.Query(ctx, `
			SELECT i.id FROM issues i JOIN projects p ON p.id = i.project_id
			JOIN LATERAL (
			  SELECT coalesce(sum(h.count) FILTER (WHERE h.hour >= `+cur+`), 0) AS cur,
			         coalesce(sum(h.count) FILTER (WHERE h.hour < `+cur+`), 0) AS prev
			  FROM issue_hourly h WHERE h.issue_id = i.id AND h.hour >= `+prev+`
			) g ON g.cur > g.prev
			WHERE `+tb.clause()+` AND i.status = 1 AND i.last_seen >= `+cur+`
			ORDER BY g.cur - g.prev DESC, g.cur DESC LIMIT 10`, tb.args...)
		if err != nil {
			return err
		}
		trending, err := pgx.CollectRows(trendRows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		if len(trending) > 0 {
			list, err := s.loadIssues(ctx, tx, t, w, issueSelect+" WHERE i.id = ANY($1) AND i.org_id = $2", trending, t.OrgID())
			if err != nil {
				return err
			}
			pos := make(map[string]int, len(trending))
			for i, id := range trending {
				pos[id.String()] = i
			}
			slices.SortFunc(list, func(a, b *apiv1.IssueSummary) int { return pos[a.Id] - pos[b.Id] })
			out.TrendingIssues = list
		}

		nb := &sqlb{}
		sc.issueConds(nb)
		nb.add("i.first_seen >= " + nb.arg(w.start))
		if out.NewIssues, err = s.loadIssues(ctx, tx, t, w, issueSelect+" WHERE "+nb.clause()+" ORDER BY i.first_seen DESC LIMIT 10", nb.args...); err != nil {
			return err
		}

		// Project health.
		pb := &sqlb{}
		sc.issueConds(pb)
		pstart := pb.arg(w.start)
		prow, err := tx.Query(ctx, `SELECT i.project_id, count(*) FILTER (WHERE i.status = 1), count(*) FILTER (WHERE i.first_seen >= `+pstart+`)
			FROM issues i JOIN projects p ON p.id = i.project_id WHERE `+pb.clause()+` GROUP BY 1`, pb.args...)
		if err != nil {
			return err
		}
		issueCounts := map[uuid.UUID][2]int64{}
		for prow.Next() {
			var id uuid.UUID
			var open, news int64
			if err := prow.Scan(&id, &open, &news); err != nil {
				prow.Close()
				return err
			}
			issueCounts[id] = [2]int64{open, news}
		}
		prow.Close()
		if err := prow.Err(); err != nil {
			return err
		}
		for _, p := range projects {
			counts := vol.byProject[p.id]
			if counts == nil {
				counts = make([]uint64, w.buckets)
			}
			ic := issueCounts[p.id]
			out.Projects = append(out.Projects, &apiv1.ProjectHealth{
				Project: p.ref, Events: sum(counts), UnresolvedIssues: uint32(ic[0]), NewIssues: uint32(ic[1]), Series: w.series(counts),
			})
		}
		slices.SortStableFunc(out.Projects, func(a, b *apiv1.ProjectHealth) int {
			switch {
			case a.Events > b.Events:
				return -1
			case a.Events < b.Events:
				return 1
			}
			return int(b.UnresolvedIssues) - int(a.UnresolvedIssues)
		})

		// Most affected customers.
		var customerFilter []string
		if len(sc.customers) > 0 {
			customerFilter = sc.customers
		}
		crow, err := tx.Query(ctx, customerStats+`
			WHERE c.org_id = $1 AND ($4::text[] IS NULL OR c.key = ANY($4)) AND (ev.events > 0 OR iss.unresolved > 0)
			ORDER BY coalesce(ev.events, 0) DESC, coalesce(iss.unresolved, 0) DESC LIMIT 10`,
			sc.orgID, w.start, sc.projects, customerFilter)
		if err != nil {
			return err
		}
		for crow.Next() {
			c, err := scanCustomerSummary(crow)
			if err != nil {
				crow.Close()
				return err
			}
			out.Customers = append(out.Customers, c)
		}
		crow.Close()
		if err := crow.Err(); err != nil {
			return err
		}
		keys := make([]string, len(out.Customers))
		for i, c := range out.Customers {
			keys[i] = c.Customer.Key
		}
		series, _, err := customerSeries(ctx, tx, w, t.OrgID(), keys, false)
		if err != nil {
			return err
		}
		for _, c := range out.Customers {
			c.Series = w.series(series[c.Customer.Key])
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "overview", err)
	}
	return connect.NewResponse(out), nil
}

// WatchLive implements DashboardService.WatchLive.
func (s *DashboardServer) WatchLive(ctx context.Context, req *connect.Request[apiv1.WatchLiveRequest], stream *connect.ServerStream[apiv1.WatchLiveResponse]) error {
	t := auth.TenantFrom(ctx)
	projects, minLevel := req.Msg.GetProjects(), req.Msg.GetMinLevel()
	sub := s.hub.Subscribe(t.OrgID(), 256)
	defer s.hub.Unsubscribe(sub)

	heartbeat := func() error {
		return stream.Send(&apiv1.WatchLiveResponse{Kind: apiv1.LiveEventKind_LIVE_EVENT_KIND_HEARTBEAT, Time: ts(time.Now())})
	}
	// An immediate heartbeat flushes response headers through proxies and
	// tells the client the stream is established.
	if err := heartbeat(); err != nil {
		return nil
	}
	ticker := time.NewTicker(liveHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.done:
			return nil
		case <-ticker.C:
			if err := heartbeat(); err != nil {
				return nil
			}
		case m := <-sub.C:
			if !realtime.Matches(m, projects, minLevel) {
				continue
			}
			if err := stream.Send(&apiv1.WatchLiveResponse{Kind: m.Kind, Time: ts(m.Time), Issue: m.Issue, Event: m.Event}); err != nil {
				return nil
			}
		}
	}
}
