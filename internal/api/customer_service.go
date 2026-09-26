package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
)

// CustomerServer implements izci.api.v1.CustomerService.
type CustomerServer struct {
	*base
	apiv1connect.UnimplementedCustomerServiceHandler
}

var _ apiv1connect.CustomerServiceHandler = (*CustomerServer)(nil)

// customerStats computes per-customer statistics for the window starting at
// $2, optionally restricted to the projects in $3 (NULL = all). Health: CRITICAL when an unresolved issue seen in the period is fatal,
// unhandled or escalating; DEGRADED when one is at least an error.
const customerStats = `
	WITH ev AS (
	  SELECT h.customer_key, sum(h.count)::bigint AS events, count(DISTINCT h.project_id)::int AS projects
	  FROM customer_hourly h JOIN projects p ON p.id = h.project_id AND p.deleted_at IS NULL
	  WHERE h.org_id = $1 AND h.hour >= $2 AND ($3::uuid[] IS NULL OR h.project_id = ANY($3)) GROUP BY 1
	), iss AS (
	  SELECT t.value AS customer_key,
	         count(*) FILTER (WHERE i.status = 1)::int AS unresolved,
	         count(*) FILTER (WHERE t.first_seen >= $2)::int AS new_issues,
	         bool_or(i.status = 1 AND t.last_seen >= $2 AND (i.level >= 5 OR i.unhandled OR i.substatus = 4)) AS critical,
	         bool_or(i.status = 1 AND t.last_seen >= $2 AND i.level >= 4) AS degraded
	  FROM issue_tags t
	  JOIN issues i ON i.id = t.issue_id
	  JOIN projects p ON p.id = i.project_id AND p.deleted_at IS NULL
	  WHERE t.org_id = $1 AND t.key = 'customer' AND ($3::uuid[] IS NULL OR i.project_id = ANY($3)) GROUP BY 1
	)
	SELECT c.id, c.key, c.name, c.attributes, c.notes, c.first_seen, c.last_seen,
	       coalesce(ev.events, 0), coalesce(ev.projects, 0), coalesce(iss.unresolved, 0), coalesce(iss.new_issues, 0),
	       coalesce(iss.critical, false), coalesce(iss.degraded, false)
	FROM customers c
	LEFT JOIN ev ON ev.customer_key = c.key
	LEFT JOIN iss ON iss.customer_key = c.key`

func scanCustomerSummary(row pgx.Row) (*apiv1.CustomerSummary, error) {
	var (
		id                   uuid.UUID
		c                    apiv1.Customer
		first, last          time.Time
		events               int64
		projects, open, news int32
		critical, degraded   bool
	)
	if err := row.Scan(&id, &c.Key, &c.Name, &c.Attributes, &c.Notes, &first, &last,
		&events, &projects, &open, &news, &critical, &degraded); err != nil {
		return nil, err
	}
	c.Id, c.FirstSeen, c.LastSeen = id.String(), ts(first), ts(last)
	s := &apiv1.CustomerSummary{
		Customer: &c, UnresolvedIssues: uint32(open), NewIssues: uint32(news), PeriodEvents: uint64(events),
		AffectedProjects: uint32(projects), Health: apiv1.Health_HEALTH_HEALTHY,
	}
	switch {
	case critical:
		s.Health = apiv1.Health_HEALTH_CRITICAL
	case degraded:
		s.Health = apiv1.Health_HEALTH_DEGRADED
	}
	return s, nil
}

// customerSeries returns event counts per customer (and per project when
// byProject) over w.
func customerSeries(ctx context.Context, tx pgx.Tx, w window, orgID uuid.UUID, keys []string, byProject bool) (map[string][]uint64, map[uuid.UUID][]uint64, error) {
	perCustomer := make(map[string][]uint64, len(keys))
	perProject := map[uuid.UUID][]uint64{}
	for _, k := range keys {
		perCustomer[k] = make([]uint64, w.buckets)
	}
	if len(keys) == 0 {
		return perCustomer, perProject, nil
	}
	var rows pgx.Rows
	var err error
	if w.minutes {
		rows, err = tx.Query(ctx, `
			SELECT customer_key, project_id, date_trunc('minute', occurred_at), count(*) FROM events
			WHERE org_id = $1 AND customer_key = ANY($2) AND received_at >= $3::timestamptz - interval '5 minutes' AND occurred_at >= $3
			GROUP BY 1, 2, 3`, orgID, keys, w.start)
	} else {
		rows, err = tx.Query(ctx, `
			SELECT customer_key, project_id, hour, count FROM customer_hourly
			WHERE org_id = $1 AND customer_key = ANY($2) AND hour >= $3`, orgID, keys, w.start.Truncate(time.Hour))
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var project uuid.UUID
		var at time.Time
		var n int64
		if err := rows.Scan(&key, &project, &at, &n); err != nil {
			return nil, nil, err
		}
		i := w.bucketIndex(at)
		if i < 0 {
			continue
		}
		perCustomer[key][i] += uint64(n)
		if byProject {
			if perProject[project] == nil {
				perProject[project] = make([]uint64, w.buckets)
			}
			perProject[project][i] += uint64(n)
		}
	}
	return perCustomer, perProject, rows.Err()
}

// ListCustomers implements CustomerService.ListCustomers.
func (s *CustomerServer) ListCustomers(ctx context.Context, req *connect.Request[apiv1.ListCustomersRequest]) (*connect.Response[apiv1.ListCustomersResponse], error) {
	m := req.Msg
	t := auth.TenantFrom(ctx)
	w := periodWindow(m.GetPeriod(), time.Now(), orgLocation(t.Org))
	limit := pageSize(m.GetPageSize())
	offset, _ := strconv.Atoi(m.GetPageToken())
	if offset < 0 || offset > 100000 {
		offset = 0
	}
	order := "coalesce(ev.events, 0) DESC, c.last_seen DESC, c.id"
	switch m.GetSort() {
	case apiv1.CustomerSort_CUSTOMER_SORT_ISSUES:
		order = "coalesce(iss.unresolved, 0) DESC, coalesce(ev.events, 0) DESC, c.id"
	case apiv1.CustomerSort_CUSTOMER_SORT_LAST_SEEN:
		order = "c.last_seen DESC, c.id"
	case apiv1.CustomerSort_CUSTOMER_SORT_NAME:
		order = "lower(CASE WHEN c.name = '' THEN c.key ELSE c.name END), c.id"
	}
	pattern := ""
	if q := strings.TrimSpace(m.GetQuery()); q != "" {
		pattern = "%" + likeEscape(q) + "%"
	}
	out := &apiv1.ListCustomersResponse{}
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, customerStats+`
			WHERE c.org_id = $1 AND ($4 = '' OR (c.key || ' ' || c.name) ILIKE $4)
			ORDER BY `+order+` LIMIT $5 OFFSET $6`, t.OrgID(), w.start, nil, pattern, limit+1, offset)
		if err != nil {
			return err
		}
		for rows.Next() {
			c, err := scanCustomerSummary(rows)
			if err != nil {
				rows.Close()
				return err
			}
			if len(out.Customers) == limit {
				out.NextPageToken = strconv.Itoa(offset + limit)
				break
			}
			out.Customers = append(out.Customers, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
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
			if w.minutes {
				c.PeriodEvents = sum(series[c.Customer.Key])
			}
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list customers", err)
	}
	return connect.NewResponse(out), nil
}

// GetCustomer implements CustomerService.GetCustomer.
func (s *CustomerServer) GetCustomer(ctx context.Context, req *connect.Request[apiv1.GetCustomerRequest]) (*connect.Response[apiv1.GetCustomerResponse], error) {
	t := auth.TenantFrom(ctx)
	period := req.Msg.GetPeriod()
	if period == apiv1.StatsPeriod_STATS_PERIOD_UNSPECIFIED {
		period = apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS
	}
	w := periodWindow(period, time.Now(), orgLocation(t.Org))
	key := strings.ToLower(strings.TrimSpace(req.Msg.GetKey()))
	out := &apiv1.GetCustomerResponse{}
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		summary, err := scanCustomerSummary(tx.QueryRow(ctx, customerStats+` WHERE c.org_id = $1 AND c.key = $4`, t.OrgID(), w.start, nil, key))
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound("customer")
		}
		if err != nil {
			return err
		}
		out.Summary = summary
		perCustomer, perProject, err := customerSeries(ctx, tx, w, t.OrgID(), []string{key}, true)
		if err != nil {
			return err
		}
		summary.Series = w.series(perCustomer[key])
		if w.minutes {
			summary.PeriodEvents = sum(perCustomer[key])
		}

		// Projects with events in the period or unresolved issues.
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.slug, p.name, p.platform,
			       (SELECT count(*) FROM issue_tags t JOIN issues i ON i.id = t.issue_id
			        WHERE t.org_id = p.org_id AND t.key = 'customer' AND t.value = $2 AND i.project_id = p.id AND i.status = 1)::int
			FROM projects p WHERE p.org_id = $1 AND p.deleted_at IS NULL ORDER BY p.slug`, t.OrgID(), key)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id uuid.UUID
			var ref apiv1.ProjectRef
			var open int32
			if err := rows.Scan(&id, &ref.Slug, &ref.Name, &ref.Platform, &open); err != nil {
				rows.Close()
				return err
			}
			ref.Id = id.String()
			counts := perProject[id]
			if open == 0 && sum(counts) == 0 {
				continue
			}
			if counts == nil {
				counts = make([]uint64, w.buckets)
			}
			out.Projects = append(out.Projects, &apiv1.CustomerProjectBreakdown{
				Project: &ref, UnresolvedIssues: uint32(open), PeriodEvents: sum(counts), Series: w.series(counts),
			})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		sortBreakdown(out.Projects)

		out.TopIssues, err = s.loadIssues(ctx, tx, t, w, issueSelect+`
			JOIN issue_tags ct ON ct.issue_id = i.id AND ct.key = 'customer' AND ct.value = $2
			WHERE i.org_id = $1 AND i.status = 1 AND p.deleted_at IS NULL
			ORDER BY ct.count DESC, i.last_seen DESC LIMIT 20`, t.OrgID(), key)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get customer", err)
	}
	return connect.NewResponse(out), nil
}

func sortBreakdown(list []*apiv1.CustomerProjectBreakdown) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].PeriodEvents > list[j-1].PeriodEvents; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// UpdateCustomer implements CustomerService.UpdateCustomer.
func (s *CustomerServer) UpdateCustomer(ctx context.Context, req *connect.Request[apiv1.UpdateCustomerRequest]) (*connect.Response[apiv1.UpdateCustomerResponse], error) {
	m := req.Msg
	key := strings.ToLower(strings.TrimSpace(m.GetKey()))
	var out apiv1.UpdateCustomerResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		var (
			id          uuid.UUID
			c           apiv1.Customer
			first, last time.Time
		)
		err := tx.QueryRow(ctx, `
			UPDATE customers SET
			  name = CASE WHEN $3::text IS NULL THEN name ELSE $3 END,
			  name_locked = name_locked OR $3::text IS NOT NULL,
			  notes = coalesce($4, notes)
			WHERE org_id = $1 AND key = $2
			RETURNING id, key, name, attributes, notes, first_seen, last_seen`,
			t.OrgID(), key, trimmedPtr(m.Name), m.Notes).Scan(&id, &c.Key, &c.Name, &c.Attributes, &c.Notes, &first, &last)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound("customer")
		}
		if err != nil {
			return err
		}
		c.Id, c.FirstSeen, c.LastSeen = id.String(), ts(first), ts(last)
		out.Customer = &c
		return s.audit(ctx, tx, "customer.update", "customer", c.Id, c.Key, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update customer", err)
	}
	return connect.NewResponse(&out), nil
}

func trimmedPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}
