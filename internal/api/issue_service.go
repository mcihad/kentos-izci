package api

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/realtime"
	"github.com/mcihad/kentos-izci/internal/search"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// IssueServer implements izci.api.v1.IssueService.
type IssueServer struct {
	*base
	apiv1connect.UnimplementedIssueServiceHandler
}

var _ apiv1connect.IssueServiceHandler = (*IssueServer)(nil)

const issueSelect = `
	SELECT i.id, coalesce(i.short_seq, 0), i.project_id, p.slug, p.name, p.platform, i.title, i.culprit, i.type, i.value,
	       i.level, i.platform, i.status, i.substatus, i.priority, i.first_seen, i.last_seen, i.times_seen,
	       i.users_count, i.customers_count, i.assignee_id, u.name, u.email, u.avatar_url, i.unhandled, i.ai_state
	FROM issues i
	JOIN projects p ON p.id = i.project_id
	LEFT JOIN users u ON u.id = i.assignee_id`

// issueRow is a scanned issue summary before series are attached.
type issueRow struct {
	summary   *apiv1.IssueSummary
	id        uuid.UUID
	substatus int16
}

func (b *base) scanIssue(row pgx.Row, orgSlug string, now time.Time) (*issueRow, error) {
	var (
		id, projectID                      uuid.UUID
		seq                                int32
		slug, pname, pplatform             string
		title, culprit, typ, val, platform string
		level, status, substatus, priority int16
		firstSeen, lastSeen                time.Time
		times                              int64
		users, customers                   int32
		assignee                           *uuid.UUID
		aname, aemail, aavatar             *string
		unhandled                          bool
		aiState                            int16
	)
	if err := row.Scan(&id, &seq, &projectID, &slug, &pname, &pplatform, &title, &culprit, &typ, &val,
		&level, &platform, &status, &substatus, &priority, &firstSeen, &lastSeen, &times,
		&users, &customers, &assignee, &aname, &aemail, &aavatar, &unhandled, &aiState); err != nil {
		return nil, err
	}
	shortID := ingest.ShortID(slug, seq)
	s := &apiv1.IssueSummary{
		Id: id.String(), ShortId: shortID,
		Project: &apiv1.ProjectRef{Id: projectID.String(), Slug: slug, Name: pname, Platform: pplatform},
		Title:   title, Culprit: culprit, Type: typ, Value: val,
		Level: ingestv1.Level(level), Platform: platform, Status: apiv1.IssueStatus(status),
		Priority: apiv1.Priority(priority), FirstSeen: ts(firstSeen), LastSeen: ts(lastSeen),
		TimesSeen: uint64(times), UsersCount: uint32(users), CustomersCount: uint32(customers),
		Unhandled: unhandled, AiState: apiv1.AiState(aiState),
		Permalink: b.cfg.PublicURL + "/o/" + orgSlug + "/issues/" + shortID,
	}
	if s.Status == apiv1.IssueStatus_ISSUE_STATUS_UNRESOLVED {
		switch {
		case substatus == 3:
			s.Substatus = apiv1.IssueSubstatus_ISSUE_SUBSTATUS_REGRESSED
		case substatus == 4:
			s.Substatus = apiv1.IssueSubstatus_ISSUE_SUBSTATUS_ESCALATING
		case now.Sub(firstSeen) < 72*time.Hour:
			s.Substatus = apiv1.IssueSubstatus_ISSUE_SUBSTATUS_NEW
		default:
			s.Substatus = apiv1.IssueSubstatus_ISSUE_SUBSTATUS_ONGOING
		}
	}
	if assignee != nil {
		s.Assignee = &apiv1.UserRef{Id: assignee.String(), Name: deref(aname), Email: deref(aemail), AvatarUrl: deref(aavatar)}
	}
	return &issueRow{summary: s, id: id, substatus: substatus}, nil
}

// loadIssues runs an issue query and attaches series for window w.
func (b *base) loadIssues(ctx context.Context, tx pgx.Tx, t *auth.Tenant, w window, sql string, args ...any) ([]*apiv1.IssueSummary, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	var list []*issueRow
	now := time.Now()
	for rows.Next() {
		r, err := b.scanIssue(rows, t.Org.Slug, now)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(list))
	for i, r := range list {
		ids[i] = r.id
	}
	series, err := issueSeries(ctx, tx, w, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*apiv1.IssueSummary, len(list))
	for i, r := range list {
		counts := series[r.id]
		r.summary.Series, r.summary.PeriodCount = w.series(counts), sum(counts)
		out[i] = r.summary
	}
	return out, nil
}

// ListIssues implements IssueService.ListIssues.
func (s *IssueServer) ListIssues(ctx context.Context, req *connect.Request[apiv1.ListIssuesRequest]) (*connect.Response[apiv1.ListIssuesResponse], error) {
	m := req.Msg
	text := strings.TrimSpace(m.GetQuery())
	if text == "" {
		text = "is:unresolved"
	}
	q, err := search.Parse(text)
	if err != nil {
		var se *search.Error
		if errors.As(err, &se) {
			return nil, queryError(se.Msg, se.Pos, se.Token)
		}
		return nil, queryError(err.Error(), 0, "")
	}
	t := auth.TenantFrom(ctx)
	p := auth.PrincipalFrom(ctx)
	now := time.Now()
	w := periodWindow(m.GetStatsPeriod(), now, orgLocation(t.Org))

	b := &sqlb{}
	b.add("i.org_id = " + b.arg(t.OrgID()))
	if err := issueFilter(b, q, t.OrgID(), p.User.ID, now); err != nil {
		return nil, err
	}
	if len(m.GetProjects()) > 0 {
		b.add("p.slug = ANY(" + b.arg(lower(m.GetProjects())) + ")")
	}
	if len(m.GetEnvironments()) > 0 {
		b.add("EXISTS (SELECT 1 FROM issue_tags t WHERE t.issue_id = i.id AND t.key = 'environment' AND t.value = ANY(" + b.arg(lower(m.GetEnvironments())) + "))")
	}
	if len(m.GetCustomers()) > 0 {
		b.add("EXISTS (SELECT 1 FROM issue_tags t WHERE t.issue_id = i.id AND t.key = 'customer' AND t.value = ANY(" + b.arg(lower(m.GetCustomers())) + "))")
	}
	if !q.Has("last_seen") {
		b.add("i.last_seen >= " + b.arg(w.start))
	}
	b.add("p.deleted_at IS NULL")
	where, whereArgs := b.clause(), slices.Clone(b.args)

	order := "i.last_seen DESC, i.id DESC"
	switch m.GetSort() {
	case apiv1.IssueSort_ISSUE_SORT_FIRST_SEEN:
		order = "i.first_seen DESC, i.id DESC"
	case apiv1.IssueSort_ISSUE_SORT_EVENTS:
		order = "(SELECT coalesce(sum(h.count), 0) FROM issue_hourly h WHERE h.issue_id = i.id AND h.hour >= " + b.arg(w.start.Truncate(time.Hour)) + ") DESC, i.last_seen DESC, i.id DESC"
	case apiv1.IssueSort_ISSUE_SORT_USERS:
		order = "i.users_count DESC, i.last_seen DESC, i.id DESC"
	case apiv1.IssueSort_ISSUE_SORT_PRIORITY:
		order = "i.priority DESC, i.last_seen DESC, i.id DESC"
	}
	limit := pageSize(m.GetPageSize())
	offset, _ := strconv.Atoi(m.GetPageToken())
	if offset < 0 || offset > 100000 {
		offset = 0
	}

	out := &apiv1.ListIssuesResponse{}
	err = s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		issues, err := s.loadIssues(ctx, tx, t, w,
			issueSelect+" WHERE "+where+" ORDER BY "+order+" LIMIT "+strconv.Itoa(limit+1)+" OFFSET "+strconv.Itoa(offset), b.args...)
		if err != nil {
			return err
		}
		if len(issues) > limit {
			issues = issues[:limit]
			out.NextPageToken = strconv.Itoa(offset + limit)
		}
		out.Issues = issues
		var total uint32
		err = tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM issues i JOIN projects p ON p.id = i.project_id WHERE `+where+` LIMIT 10000) x`, whereArgs...).Scan(&total)
		out.TotalEstimate = total
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list issues", err)
	}
	return connect.NewResponse(out), nil
}

// resolveIssueID accepts a UUID or a short id ("KALEM-1A").
func (s *IssueServer) resolveIssueID(ctx context.Context, tx pgx.Tx, t *auth.Tenant, ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	slug, seq, ok := ingest.ParseShortID(ref)
	if !ok {
		return uuid.Nil, errNotFound("issue")
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT i.id FROM issues i JOIN projects p ON p.id = i.project_id
		WHERE p.org_id = $1 AND p.slug = $2 AND i.short_seq = $3`, t.OrgID(), slug, seq).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errNotFound("issue")
	}
	return id, err
}

// GetIssue implements IssueService.GetIssue.
func (s *IssueServer) GetIssue(ctx context.Context, req *connect.Request[apiv1.GetIssueRequest]) (*connect.Response[apiv1.GetIssueResponse], error) {
	t := auth.TenantFrom(ctx)
	w := periodWindow(req.Msg.GetStatsPeriod(), time.Now(), orgLocation(t.Org))
	var out apiv1.GetIssueResponse
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		id, err := s.resolveIssueID(ctx, tx, t, req.Msg.GetIssue())
		if err != nil {
			return err
		}
		issues, err := s.loadIssues(ctx, tx, t, w, issueSelect+" WHERE i.id = $1 AND i.org_id = $2", id, t.OrgID())
		if err != nil {
			return err
		}
		if len(issues) == 0 {
			return errNotFound("issue")
		}
		issue := &apiv1.Issue{Summary: issues[0]}
		var (
			grouping, meta               []byte
			resolvedAt, snoozeUntil      *time.Time
			resolvedBy, lastEvent        *uuid.UUID
			snoozeCount, snoozeBase      *int64
			untilEscalating, nextRelease bool
			timesSeen                    int64
		)
		err = tx.QueryRow(ctx, `SELECT first_release, last_release, resolved_at, resolved_by, resolved_in_release,
			resolved_in_next_release, snooze_until, snooze_count, snooze_base, snooze_until_escalating, grouping, metadata,
			last_event_id, times_seen FROM issues WHERE id = $1`, id).Scan(
			&issue.FirstRelease, &issue.LastRelease, &resolvedAt, &resolvedBy, &issue.ResolvedInRelease, &nextRelease,
			&snoozeUntil, &snoozeCount, &snoozeBase, &untilEscalating, &grouping, &meta, &lastEvent, &timesSeen)
		if err != nil {
			return err
		}
		issue.ResolvedAt = tsp(resolvedAt)
		if resolvedBy != nil {
			var u apiv1.UserRef
			if tx.QueryRow(ctx, `SELECT id::text, name, email, avatar_url FROM users WHERE id = $1`, *resolvedBy).Scan(&u.Id, &u.Name, &u.Email, &u.AvatarUrl) == nil {
				issue.ResolvedBy = &u
			}
		}
		if issue.Summary.GetStatus() == apiv1.IssueStatus_ISSUE_STATUS_IGNORED {
			sn := &apiv1.Snooze{Until: tsp(snoozeUntil), UntilEscalating: untilEscalating}
			if snoozeCount != nil {
				sn.UntilCount = uint64(*snoozeCount)
				if snoozeBase != nil {
					sn.CountSince = uint64(timesSeen - *snoozeBase)
				}
			}
			issue.Snooze = sn
		}
		var g struct {
			Version    uint32   `json:"version"`
			Strategy   string   `json:"strategy"`
			Components []string `json:"components"`
		}
		_ = json.Unmarshal(grouping, &g)
		var fp []byte
		_ = tx.QueryRow(ctx, `SELECT fingerprint FROM issues WHERE id = $1`, id).Scan(&fp)
		issue.Grouping = &apiv1.Grouping{Version: g.Version, Strategy: g.Strategy, Components: g.Components, Fingerprint: hexString(fp)}
		var loc ingest.Location
		_ = json.Unmarshal(meta, &loc)
		issue.Location = &apiv1.IssueLocation{Filename: loc.Filename, Function: loc.Function, Module: loc.Module, Lineno: loc.Lineno}
		if lastEvent != nil {
			issue.LatestEventId = lastEvent.String()
		}
		prs, err := tx.Query(ctx, `SELECT id, pull_request FROM ai_runs WHERE issue_id = $1 AND pull_request IS NOT NULL ORDER BY created_at DESC`, id)
		if err != nil {
			return err
		}
		defer prs.Close()
		for prs.Next() {
			var runID uuid.UUID
			var raw []byte
			if err := prs.Scan(&runID, &raw); err != nil {
				return err
			}
			var pr apiv1.PullRequestRef
			if jsonIn(raw, &pr) == nil {
				pr.AiRunId = runID.String()
				issue.PullRequests = append(issue.PullRequests, &pr)
			}
		}
		out.Issue = issue
		return prs.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get issue", err)
	}
	return connect.NewResponse(&out), nil
}

// UpdateIssues implements IssueService.UpdateIssues.
func (s *IssueServer) UpdateIssues(ctx context.Context, req *connect.Request[apiv1.UpdateIssuesRequest]) (*connect.Response[apiv1.UpdateIssuesResponse], error) {
	m := req.Msg
	t := auth.TenantFrom(ctx)
	p := auth.PrincipalFrom(ctx)
	ids := make([]uuid.UUID, 0, len(m.GetIssueIds()))
	for _, raw := range m.GetIssueIds() {
		id, _ := uuid.Parse(raw)
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	w := periodWindow(apiv1.StatsPeriod_STATS_PERIOD_LAST_24_HOURS, time.Now(), orgLocation(t.Org))
	var out apiv1.UpdateIssuesResponse
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		type change struct {
			kind int16
			data map[string]string
		}
		var changes []change
		if m.Status != nil {
			var err error
			switch m.GetStatus() {
			case apiv1.IssueStatus_ISSUE_STATUS_RESOLVED:
				inRelease := m.GetResolve().GetInRelease()
				err = store.ResolveIssues(ctx, tx, t.OrgID(), ids, p.User.ID, inRelease, m.GetResolve().GetInNextRelease())
				changes = append(changes, change{store.ActivityStatusChanged, map[string]string{"to": store.StatusName(store.IssueResolved), "release": inRelease}})
			case apiv1.IssueStatus_ISSUE_STATUS_IGNORED:
				ig := m.GetIgnore()
				var o store.IgnoreOptions
				if d := ig.GetDuration().AsDuration(); d > 0 {
					u := time.Now().Add(d)
					o.Until = &u
				}
				if c := ig.GetCount(); c > 0 {
					v := int64(c)
					o.Count = &v
				}
				o.UntilEscalating = ig.GetUntilEscalating()
				err = store.IgnoreIssues(ctx, tx, t.OrgID(), ids, o)
				changes = append(changes, change{store.ActivityStatusChanged, map[string]string{"to": store.StatusName(store.IssueIgnored)}})
			case apiv1.IssueStatus_ISSUE_STATUS_UNRESOLVED:
				err = store.ReopenIssues(ctx, tx, t.OrgID(), ids)
				changes = append(changes, change{store.ActivityStatusChanged, map[string]string{"to": store.StatusName(store.IssueUnresolved)}})
			default:
				return fieldError("status", "unknown status")
			}
			if err != nil {
				return err
			}
		}
		if m.AssigneeId != nil {
			var assignee *uuid.UUID
			name := ""
			if a := m.GetAssigneeId(); a != "" {
				id, err := uuid.Parse(a)
				if err != nil {
					return fieldError("assignee_id", "invalid user id")
				}
				if err := tx.QueryRow(ctx, `SELECT u.email FROM org_members m JOIN users u ON u.id = m.user_id
					WHERE m.org_id = $1 AND m.user_id = $2`, t.OrgID(), id).Scan(&name); err != nil {
					return fieldError("assignee_id", "assignee is not a member of the organization")
				}
				assignee = &id
			}
			if _, err := tx.Exec(ctx, `UPDATE issues SET assignee_id = $2, updated_at = now() WHERE id = ANY($1) AND org_id = $3`, ids, assignee, t.OrgID()); err != nil {
				return err
			}
			changes = append(changes, change{store.ActivityAssigned, map[string]string{"assignee": name}})
		}
		if m.Priority != nil {
			if _, err := tx.Exec(ctx, `UPDATE issues SET priority = $2, updated_at = now() WHERE id = ANY($1) AND org_id = $3`, ids, int16(m.GetPriority()), t.OrgID()); err != nil {
				return err
			}
			changes = append(changes, change{store.ActivityPriorityChanged, map[string]string{"to": m.GetPriority().String()}})
		}
		for _, c := range changes {
			if err := store.AddIssueActivity(ctx, tx, t.OrgID(), ids, c.kind, &p.User.ID, c.data); err != nil {
				return err
			}
		}
		issues, err := s.loadIssues(ctx, tx, t, w, issueSelect+" WHERE i.id = ANY($1) AND i.org_id = $2 ORDER BY i.last_seen DESC", ids, t.OrgID())
		out.Issues = issues
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update issues", err)
	}
	now := time.Now()
	for _, is := range out.Issues {
		pid, _ := uuid.Parse(is.GetProject().GetId())
		s.hub.Publish(realtime.Message{Kind: apiv1.LiveEventKind_LIVE_EVENT_KIND_ISSUE_UPDATED, OrgID: t.OrgID(), ProjectID: pid,
			Level: is.GetLevel(), Time: now, Issue: is})
	}
	return connect.NewResponse(&out), nil
}

// ListIssueEvents implements IssueService.ListIssueEvents.
func (s *IssueServer) ListIssueEvents(ctx context.Context, req *connect.Request[apiv1.ListIssueEventsRequest]) (*connect.Response[apiv1.ListIssueEventsResponse], error) {
	t := auth.TenantFrom(ctx)
	issueID, _ := uuid.Parse(req.Msg.GetIssueId())
	b := &sqlb{}
	b.add("e.issue_id = " + b.arg(issueID))
	b.add("e.org_id = " + b.arg(t.OrgID()))
	if text := strings.TrimSpace(req.Msg.GetQuery()); text != "" {
		q, err := search.Parse(text)
		if err != nil {
			var se *search.Error
			if errors.As(err, &se) {
				return nil, queryError(se.Msg, se.Pos, se.Token)
			}
			return nil, queryError(err.Error(), 0, "")
		}
		if err := eventFilter(b, q); err != nil {
			return nil, err
		}
	}
	if at, id, ok := decodeCursor(req.Msg.GetPageToken()); ok {
		b.add("(e.received_at, e.id) < (" + b.arg(at) + ", " + b.arg(id) + ")")
	}
	limit := pageSize(req.Msg.GetPageSize())
	out := &apiv1.ListIssueEventsResponse{}
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, eventSummarySelect+` WHERE `+b.clause()+` ORDER BY e.received_at DESC, e.id DESC LIMIT `+strconv.Itoa(limit+1), b.args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			ev, err := scanEventSummary(rows)
			if err != nil {
				return err
			}
			if len(out.Events) == limit {
				last := out.Events[len(out.Events)-1]
				lid, _ := uuid.Parse(last.Id)
				out.NextPageToken = encodeCursor(last.ReceivedAt.AsTime(), lid)
				break
			}
			out.Events = append(out.Events, ev)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list issue events", err)
	}
	return connect.NewResponse(out), nil
}

// eventFilter restricts events AS e with event-level search keys.
func eventFilter(b *sqlb, q *search.Query) error {
	cols := map[string]string{
		"environment": "e.environment", "release": "e.release", "customer": "e.customer_key", "tenant": "e.tenant_key",
		"user": "e.user_label", "browser": "e.browser", "os": "e.os", "server": "e.server_name", "transaction": "e.transaction",
		"level": "e.level",
	}
	for _, t := range q.Terms {
		col, ok := cols[t.Key]
		if !ok {
			return queryError("key not supported for events", t.Pos, t.Key)
		}
		var cond string
		if t.Key == "level" {
			var vals []int16
			for _, v := range t.Values {
				n, ok := levelNames[strings.ToLower(v)]
				if !ok {
					return queryError("unknown level", t.Pos, v)
				}
				vals = append(vals, n)
			}
			cond = col + " = ANY(" + b.arg(vals) + ")"
		} else if hasStar(t.Values) {
			cond = anyLike(b, col, t.Values)
		} else {
			vals := t.Values
			if t.Key == "customer" {
				vals = lower(vals)
			}
			cond = col + " = ANY(" + b.arg(vals) + ")"
		}
		if t.Negate {
			cond = "NOT (" + cond + ")"
		}
		b.add(cond)
	}
	for _, text := range q.Text {
		b.add("e.title ILIKE " + b.arg("%"+likeEscape(text)+"%"))
	}
	return nil
}

// GetIssueStats implements IssueService.GetIssueStats.
func (s *IssueServer) GetIssueStats(ctx context.Context, req *connect.Request[apiv1.GetIssueStatsRequest]) (*connect.Response[apiv1.GetIssueStatsResponse], error) {
	t := auth.TenantFrom(ctx)
	issueID, _ := uuid.Parse(req.Msg.GetIssueId())
	w := periodWindow(req.Msg.GetPeriod(), time.Now(), orgLocation(t.Org))
	var out apiv1.GetIssueStatsResponse
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		var counts []uint64
		if env := req.Msg.GetEnvironment(); env != "" {
			counts = make([]uint64, w.buckets)
			rows, err := tx.Query(ctx, `SELECT occurred_at FROM events WHERE issue_id = $1 AND environment = $2 AND occurred_at >= $3`, issueID, strings.ToLower(env), w.start)
			if err != nil {
				return err
			}
			for rows.Next() {
				var at time.Time
				if err := rows.Scan(&at); err != nil {
					rows.Close()
					return err
				}
				if i := w.bucketIndex(at); i >= 0 {
					counts[i]++
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		} else {
			series, err := issueSeries(ctx, tx, w, []uuid.UUID{issueID})
			if err != nil {
				return err
			}
			counts = series[issueID]
		}
		out.Series, out.Total = w.series(counts), sum(counts)
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "issue stats", err)
	}
	return connect.NewResponse(&out), nil
}

var defaultFacetKeys = []string{"customer", "tenant", "environment", "release", "browser", "os", "device", "server_name", "level", "user", "handled"}

// GetIssueTagFacets implements IssueService.GetIssueTagFacets.
func (s *IssueServer) GetIssueTagFacets(ctx context.Context, req *connect.Request[apiv1.GetIssueTagFacetsRequest]) (*connect.Response[apiv1.GetIssueTagFacetsResponse], error) {
	t := auth.TenantFrom(ctx)
	issueID, _ := uuid.Parse(req.Msg.GetIssueId())
	top := int(req.Msg.GetTopValues())
	if top <= 0 {
		top = 10
	}
	keys := req.Msg.GetKeys()
	var out apiv1.GetIssueTagFacetsResponse
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		if len(keys) == 0 {
			keys = slices.Clone(defaultFacetKeys)
			rows, err := tx.Query(ctx, `SELECT key FROM issue_tags WHERE issue_id = $1 AND NOT (key = ANY($2))
				GROUP BY key ORDER BY sum(count) DESC LIMIT 8`, issueID, defaultFacetKeys)
			if err != nil {
				return err
			}
			custom, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			keys = append(keys, custom...)
		}
		for _, key := range keys {
			f := &apiv1.TagFacet{Key: key}
			var total int64
			var unique int32
			if err := tx.QueryRow(ctx, `SELECT coalesce(sum(count), 0), count(*) FROM issue_tags WHERE issue_id = $1 AND key = $2`,
				issueID, key).Scan(&total, &unique); err != nil {
				return err
			}
			if unique == 0 {
				continue
			}
			f.Total, f.UniqueValues = uint64(total), uint32(unique)
			rows, err := tx.Query(ctx, `
				SELECT t.value, coalesce(c.name, ''), t.count, t.first_seen, t.last_seen
				FROM issue_tags t
				LEFT JOIN customers c ON t.key = 'customer' AND c.org_id = t.org_id AND c.key = t.value
				WHERE t.issue_id = $1 AND t.key = $2
				ORDER BY t.count DESC LIMIT $3`, issueID, key, top)
			if err != nil {
				return err
			}
			for rows.Next() {
				var v apiv1.TagValueCount
				var first, last time.Time
				var cnt int64
				if err := rows.Scan(&v.Value, &v.Label, &cnt, &first, &last); err != nil {
					rows.Close()
					return err
				}
				v.Count, v.FirstSeen, v.LastSeen = uint64(cnt), ts(first), ts(last)
				f.Values = append(f.Values, &v)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			out.Facets = append(out.Facets, f)
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "tag facets", err)
	}
	return connect.NewResponse(&out), nil
}

// ListIssueActivity implements IssueService.ListIssueActivity.
func (s *IssueServer) ListIssueActivity(ctx context.Context, req *connect.Request[apiv1.ListIssueActivityRequest]) (*connect.Response[apiv1.ListIssueActivityResponse], error) {
	t := auth.TenantFrom(ctx)
	issueID, _ := uuid.Parse(req.Msg.GetIssueId())
	limit := pageSize(req.Msg.GetPageSize())
	before, beforeID, has := decodeCursor(req.Msg.GetPageToken())
	out := &apiv1.ListIssueActivityResponse{}
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT a.id, a.kind, a.text, a.data, a.created_at, u.id, u.name, u.email, u.avatar_url
			FROM issue_activity a LEFT JOIN users u ON u.id = a.actor_id
			WHERE a.issue_id = $1 AND (NOT $2 OR (a.created_at, a.id) < ($3, $4))
			ORDER BY a.created_at DESC, a.id DESC LIMIT $5`, issueID, has, before, beforeID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a apiv1.IssueActivity
			var id uuid.UUID
			var kind int16
			var data map[string]string
			var created time.Time
			var uid *uuid.UUID
			var un, ue, ua *string
			if err := rows.Scan(&id, &kind, &a.Text, &data, &created, &uid, &un, &ue, &ua); err != nil {
				return err
			}
			a.Id, a.Kind, a.Data, a.CreatedAt = id.String(), apiv1.ActivityKind(kind), data, ts(created)
			if uid != nil {
				a.Actor = &apiv1.UserRef{Id: uid.String(), Name: deref(un), Email: deref(ue), AvatarUrl: deref(ua)}
			}
			if len(out.Activities) == limit {
				last := out.Activities[len(out.Activities)-1]
				lid, _ := uuid.Parse(last.Id)
				out.NextPageToken = encodeCursor(last.CreatedAt.AsTime(), lid)
				break
			}
			out.Activities = append(out.Activities, &a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list activity", err)
	}
	return connect.NewResponse(out), nil
}

// AddIssueComment implements IssueService.AddIssueComment.
func (s *IssueServer) AddIssueComment(ctx context.Context, req *connect.Request[apiv1.AddIssueCommentRequest]) (*connect.Response[apiv1.AddIssueCommentResponse], error) {
	t := auth.TenantFrom(ctx)
	p := auth.PrincipalFrom(ctx)
	issueID, _ := uuid.Parse(req.Msg.GetIssueId())
	var out apiv1.AddIssueCommentResponse
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		var id uuid.UUID
		var created time.Time
		err := tx.QueryRow(ctx, `
			INSERT INTO issue_activity (org_id, issue_id, kind, actor_id, text)
			SELECT org_id, id, 2, $2, $3 FROM issues WHERE id = $1
			RETURNING id, created_at`, issueID, p.User.ID, strings.TrimSpace(req.Msg.GetText())).Scan(&id, &created)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound("issue")
		}
		out.Activity = &apiv1.IssueActivity{Id: id.String(), Kind: apiv1.ActivityKind_ACTIVITY_KIND_COMMENT, Actor: toUserRef(p.User),
			Text: strings.TrimSpace(req.Msg.GetText()), CreatedAt: ts(created)}
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "add comment", err)
	}
	return connect.NewResponse(&out), nil
}

// searchKeys are the keys offered by autocompletion.
var searchKeys = []struct{ key, desc string }{
	{"is", "Issue state (unresolved, resolved, ignored, new, regressed, assigned, unhandled)"},
	{"level", "Severity (debug, info, warning, error, fatal)"},
	{"customer", "Customer key reported by the application"},
	{"tenant", "Tenant reported by the application"},
	{"project", "Project slug"},
	{"environment", "Deployment environment"},
	{"release", "Application release"},
	{"browser", "Browser name and major version"},
	{"os", "Operating system"},
	{"device", "Device model or type"},
	{"server", "Server name"},
	{"user", "Affected user"},
	{"assigned", "Assignee (me, none or an email)"},
	{"priority", "Priority (low, medium, high, critical)"},
	{"type", "Exception type"},
	{"file", "Source file of the top frame"},
	{"has", "Presence (ai, user, customer, assignee)"},
	{"first_seen", "First occurrence (-24h, +7d, >2026-09-01)"},
	{"last_seen", "Last occurrence (-1h, +30d)"},
	{"times_seen", "Number of events (>100)"},
}

var staticValues = map[string][]string{
	"is":         {"unresolved", "resolved", "ignored", "new", "regressed", "escalating", "assigned", "unassigned", "unhandled"},
	"level":      {"fatal", "error", "warning", "info", "debug"},
	"priority":   {"critical", "high", "medium", "low"},
	"has":        {"ai", "user", "customer", "assignee"},
	"assigned":   {"me", "none"},
	"first_seen": {"-1h", "-24h", "-7d", "-30d"},
	"last_seen":  {"-1h", "-24h", "-7d", "+30d"},
}

// SuggestSearch implements IssueService.SuggestSearch.
func (s *IssueServer) SuggestSearch(ctx context.Context, req *connect.Request[apiv1.SuggestSearchRequest]) (*connect.Response[apiv1.SuggestSearchResponse], error) {
	t := auth.TenantFrom(ctx)
	q := req.Msg.GetQuery()
	start, end := search.TermAt(q, int(req.Msg.GetCursor()))
	term := strings.TrimPrefix(q[start:end], "!")
	neg := strings.HasPrefix(q[start:end], "!")
	prefix := ""
	if neg {
		prefix = "!"
	}
	out := &apiv1.SuggestSearchResponse{ReplaceFrom: uint32(start), ReplaceTo: uint32(end)}
	key, value, hasColon := strings.Cut(term, ":")
	if !hasColon {
		for _, k := range searchKeys {
			if strings.HasPrefix(k.key, strings.ToLower(term)) {
				out.Suggestions = append(out.Suggestions, &apiv1.SearchSuggestion{Text: prefix + k.key + ":", Label: k.key, Description: k.desc,
					Kind: apiv1.SuggestionKind_SUGGESTION_KIND_KEY})
			}
		}
		return connect.NewResponse(out), nil
	}
	key = strings.ToLower(key)
	if vals, ok := staticValues[key]; ok {
		for _, v := range vals {
			if strings.HasPrefix(v, strings.ToLower(value)) {
				out.Suggestions = append(out.Suggestions, &apiv1.SearchSuggestion{Text: prefix + key + ":" + v, Label: v, Kind: apiv1.SuggestionKind_SUGGESTION_KIND_VALUE})
			}
		}
		return connect.NewResponse(out), nil
	}
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		var rows pgx.Rows
		var err error
		pattern := likeEscape(value) + "%"
		switch key {
		case "project":
			rows, err = tx.Query(ctx, `SELECT slug, name, 0::bigint FROM projects WHERE org_id = $1 AND deleted_at IS NULL AND slug ILIKE $2 ORDER BY slug LIMIT 20`, t.OrgID(), pattern)
		case "customer":
			rows, err = tx.Query(ctx, `SELECT key, name, 0::bigint FROM customers WHERE org_id = $1 AND (key ILIKE $2 OR name ILIKE $3)
				ORDER BY last_seen DESC LIMIT 20`, t.OrgID(), pattern, "%"+likeEscape(value)+"%")
		default:
			tagKey := key
			if k, ok := tagKeys[key]; ok {
				tagKey = k
			}
			rows, err = tx.Query(ctx, `SELECT value, '', sum(count)::bigint FROM issue_tags WHERE org_id = $1 AND key = $2 AND value ILIKE $3
				GROUP BY value ORDER BY sum(count) DESC LIMIT 20`, t.OrgID(), tagKey, pattern)
		}
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v, label string
			var n int64
			if err := rows.Scan(&v, &label, &n); err != nil {
				return err
			}
			text := v
			if strings.ContainsAny(v, " \t\",") {
				text = strconv.Quote(v)
			}
			if label == "" {
				label = v
			}
			out.Suggestions = append(out.Suggestions, &apiv1.SearchSuggestion{Text: prefix + key + ":" + text, Label: label,
				Kind: apiv1.SuggestionKind_SUGGESTION_KIND_VALUE, Count: uint64(n)})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "suggest search", err)
	}
	return connect.NewResponse(out), nil
}

func hexString(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2], out[i*2+1] = hexdigits[c>>4], hexdigits[c&15]
	}
	return string(out)
}

// PublishIssueUpdates broadcasts the current state of issues that changed
// outside the API (Telegram buttons) to live streams.
func (s *IssueServer) PublishIssueUpdates(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) {
	var issues []*apiv1.IssueSummary
	err := s.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		org, err := store.GetOrganization(ctx, tx, orgID)
		if err != nil {
			return err
		}
		w := periodWindow(apiv1.StatsPeriod_STATS_PERIOD_LAST_24_HOURS, time.Now(), orgLocation(org))
		issues, err = s.loadIssues(ctx, tx, &auth.Tenant{Org: org}, w, issueSelect+" WHERE i.id = ANY($1) AND i.org_id = $2", ids, orgID)
		return err
	})
	if err != nil {
		s.log.Warn("publish issue updates", "err", err)
		return
	}
	now := time.Now()
	for _, is := range issues {
		pid, _ := uuid.Parse(is.GetProject().GetId())
		s.hub.Publish(realtime.Message{Kind: apiv1.LiveEventKind_LIVE_EVENT_KIND_ISSUE_UPDATED, OrgID: orgID, ProjectID: pid,
			Level: is.GetLevel(), Time: now, Issue: is})
	}
}
