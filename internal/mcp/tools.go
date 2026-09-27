package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

var periods = map[string]apiv1.StatsPeriod{
	"1h": apiv1.StatsPeriod_STATS_PERIOD_LAST_HOUR, "24h": apiv1.StatsPeriod_STATS_PERIOD_LAST_24_HOURS,
	"7d": apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS, "14d": apiv1.StatsPeriod_STATS_PERIOD_LAST_14_DAYS,
	"30d": apiv1.StatsPeriod_STATS_PERIOD_LAST_30_DAYS, "90d": apiv1.StatsPeriod_STATS_PERIOD_LAST_90_DAYS,
}

func period(s string, fallback apiv1.StatsPeriod) (apiv1.StatsPeriod, error) {
	if s == "" {
		return fallback, nil
	}
	p, ok := periods[strings.ToLower(s)]
	if !ok {
		return 0, fmt.Errorf("period must be one of 1h, 24h, 7d, 14d, 30d, 90d (got %q)", s)
	}
	return p, nil
}

func limit(n, def, most int) uint32 {
	if n <= 0 {
		return uint32(def)
	}
	return uint32(min(n, most))
}

var (
	readOnly    = &mcpsdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolp(false)}
	changes     = &mcpsdk.ToolAnnotations{DestructiveHint: boolp(false), IdempotentHint: true, OpenWorldHint: boolp(false)}
	appends     = &mcpsdk.ToolAnnotations{DestructiveHint: boolp(false), OpenWorldHint: boolp(false)}
	publishesPR = &mcpsdk.ToolAnnotations{DestructiveHint: boolp(false), IdempotentHint: true, OpenWorldHint: boolp(true)}
)

func boolp(b bool) *bool { return &b }

// scoped resolves the caller's credentials and organization for a call.
func (s *server) scoped(ctx context.Context, req *mcpsdk.CallToolRequest, org string) (context.Context, error) {
	var extra *mcpsdk.RequestExtra
	if req != nil {
		extra = req.Extra
	}
	c, err := s.callerCredentials(extra, org)
	if err != nil {
		return nil, err
	}
	if c.Org == "" {
		// Tokens bound to an organization need no header; otherwise the
		// user's only organization is the obvious choice.
		res, err := s.api.auth.GetSession(withCredentials(ctx, c), connect.NewRequest(&apiv1.GetSessionRequest{}))
		if err != nil {
			return nil, apiError(err)
		}
		if m := res.Msg.GetSession().GetMemberships(); len(m) == 1 {
			c.Org = m[0].GetOrganization().GetSlug()
		}
	}
	return withCredentials(ctx, c), nil
}

// issueID resolves a short id or UUID to the issue's UUID.
func (s *server) issueID(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("issue is required (short id such as KALEM-1A, or UUID)")
	}
	res, err := s.api.issues.GetIssue(ctx, connect.NewRequest(&apiv1.GetIssueRequest{Issue: ref}))
	if err != nil {
		return "", apiError(err)
	}
	return res.Msg.GetIssue().GetSummary().GetId(), nil
}

// ── Organizations and projects ─────────────────────────────────────────────

type listOrganizationsIn struct{}

// Organization is an organization the token can access.
type Organization struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role" jsonschema:"viewer, member, admin or owner"`
}

type listOrganizationsOut struct {
	Organizations []Organization `json:"organizations"`
}

func (s *server) listOrganizations(ctx context.Context, req *mcpsdk.CallToolRequest, _ listOrganizationsIn) (*mcpsdk.CallToolResult, listOrganizationsOut, error) {
	c, err := s.callerCredentials(req.Extra, "")
	if err != nil {
		return nil, listOrganizationsOut{}, err
	}
	res, err := s.api.auth.GetSession(withCredentials(ctx, Credentials{Token: c.Token}), connect.NewRequest(&apiv1.GetSessionRequest{}))
	if err != nil {
		return nil, listOrganizationsOut{}, apiError(err)
	}
	var out listOrganizationsOut
	for _, m := range res.Msg.GetSession().GetMemberships() {
		out.Organizations = append(out.Organizations, Organization{Slug: m.GetOrganization().GetSlug(), Name: m.GetOrganization().GetName(),
			Role: enumName(m.GetRole().String(), "ROLE_")})
	}
	return nil, out, nil
}

type listProjectsIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
}

// Project is a monitored application.
type Project struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Platform         string `json:"platform,omitempty"`
	UnresolvedIssues uint32 `json:"unresolved_issues"`
	Events24h        uint64 `json:"events_24h"`
	LastEventAt      string `json:"last_event_at,omitempty"`
	Repository       string `json:"repository,omitempty" jsonschema:"linked source repository (owner/name)"`
}

type listProjectsOut struct {
	Projects []Project `json:"projects"`
}

func (s *server) listProjects(ctx context.Context, req *mcpsdk.CallToolRequest, in listProjectsIn) (*mcpsdk.CallToolResult, listProjectsOut, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, listProjectsOut{}, err
	}
	res, err := s.api.projects.ListProjects(ctx, connect.NewRequest(&apiv1.ListProjectsRequest{}))
	if err != nil {
		return nil, listProjectsOut{}, apiError(err)
	}
	var out listProjectsOut
	for _, p := range res.Msg.GetProjects() {
		pr := Project{Slug: p.GetSlug(), Name: p.GetName(), Platform: p.GetPlatform(), UnresolvedIssues: p.GetStats().GetUnresolvedIssues(),
			Events24h: p.GetStats().GetEvents_24H(), LastEventAt: ts(p.GetStats().GetLastEventAt())}
		if r := p.GetRepository(); r != nil {
			pr.Repository = r.GetOwner() + "/" + r.GetName()
		}
		out.Projects = append(out.Projects, pr)
	}
	return nil, out, nil
}

type projectSetupIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Project      string `json:"project" jsonschema:"project slug"`
}

type projectSetupOut struct {
	Project           Project `json:"project"`
	BaseURL           string  `json:"base_url" jsonschema:"base URL of the Connect/JSON ingest API"`
	GRPCTarget        string  `json:"grpc_target" jsonschema:"host:port for native gRPC clients"`
	GRPCTLS           bool    `json:"grpc_tls"`
	SentryDSNTemplate string  `json:"sentry_dsn_template" jsonschema:"DSN for Sentry SDKs; replace <key> with an ingest key of the project"`
	Notes             string  `json:"notes"`
}

func (s *server) projectSetup(ctx context.Context, req *mcpsdk.CallToolRequest, in projectSetupIn) (*mcpsdk.CallToolResult, projectSetupOut, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, projectSetupOut{}, err
	}
	p, err := s.api.projects.GetProject(ctx, connect.NewRequest(&apiv1.GetProjectRequest{Slug: in.Project}))
	if err != nil {
		return nil, projectSetupOut{}, apiError(err)
	}
	e, err := s.api.projects.GetIngestEndpoints(ctx, connect.NewRequest(&apiv1.GetIngestEndpointsRequest{Project: in.Project}))
	if err != nil {
		return nil, projectSetupOut{}, apiError(err)
	}
	pr := p.Msg.GetProject()
	out := projectSetupOut{
		Project: Project{Slug: pr.GetSlug(), Name: pr.GetName(), Platform: pr.GetPlatform(), UnresolvedIssues: pr.GetStats().GetUnresolvedIssues(),
			Events24h: pr.GetStats().GetEvents_24H(), LastEventAt: ts(pr.GetStats().GetLastEventAt())},
		BaseURL: e.Msg.GetBaseUrl(), GRPCTarget: e.Msg.GetGrpcTarget(), GRPCTLS: e.Msg.GetGrpcTls(), SentryDSNTemplate: e.Msg.GetSentryDsnTemplate(),
		Notes: "Send events with izci.ingest.v1.IngestService/SendEvent (gRPC or Connect JSON) and the header " +
			"\"Authorization: Bearer izk_…\" (an ingest key of the project), or point a Sentry SDK at the DSN. " +
			"Ingest keys are created in the project settings (Anahtarlar).",
	}
	if r := pr.GetRepository(); r != nil {
		out.Project.Repository = r.GetOwner() + "/" + r.GetName()
	}
	return nil, out, nil
}

// ── Issues ─────────────────────────────────────────────────────────────────

type searchIssuesIn struct {
	Organization string   `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Query        string   `json:"query,omitempty" jsonschema:"search language, e.g. 'is:unresolved level:>=error customer:sivas release:1.4 \"timeout\"'. Keys: is: (unresolved resolved ignored new regressed escalating assigned unassigned), level:, project:, environment:, release:, customer:, tenant:, user:, browser:, os:, assigned:me, has:, tag keys (region:x); free text matches title and culprit. Default is:unresolved"`
	Projects     []string `json:"projects,omitempty" jsonschema:"project slugs to limit the search to"`
	Environments []string `json:"environments,omitempty"`
	Customers    []string `json:"customers,omitempty" jsonschema:"customer keys"`
	Period       string   `json:"period,omitempty" jsonschema:"1h, 24h, 7d, 14d, 30d or 90d; counts and sorting by events use it (default 24h)"`
	Sort         string   `json:"sort,omitempty" jsonschema:"last_seen (default), first_seen, events, users or priority"`
	Limit        int      `json:"limit,omitempty" jsonschema:"maximum issues to return, 1-50 (default 20)"`
	Cursor       string   `json:"cursor,omitempty" jsonschema:"next_cursor of a previous call"`
}

type searchIssuesOut struct {
	Issues        []IssueItem `json:"issues"`
	TotalEstimate uint32      `json:"total_estimate" jsonschema:"approximate number of matching issues"`
	NextCursor    string      `json:"next_cursor,omitempty"`
}

var sorts = map[string]apiv1.IssueSort{
	"": apiv1.IssueSort_ISSUE_SORT_LAST_SEEN, "last_seen": apiv1.IssueSort_ISSUE_SORT_LAST_SEEN, "first_seen": apiv1.IssueSort_ISSUE_SORT_FIRST_SEEN,
	"events": apiv1.IssueSort_ISSUE_SORT_EVENTS, "users": apiv1.IssueSort_ISSUE_SORT_USERS, "priority": apiv1.IssueSort_ISSUE_SORT_PRIORITY,
}

func (s *server) searchIssues(ctx context.Context, req *mcpsdk.CallToolRequest, in searchIssuesIn) (*mcpsdk.CallToolResult, searchIssuesOut, error) {
	sort, ok := sorts[strings.ToLower(in.Sort)]
	if !ok {
		return nil, searchIssuesOut{}, fmt.Errorf("sort must be last_seen, first_seen, events, users or priority (got %q)", in.Sort)
	}
	p, err := period(in.Period, apiv1.StatsPeriod_STATS_PERIOD_LAST_24_HOURS)
	if err != nil {
		return nil, searchIssuesOut{}, err
	}
	ctx, err = s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, searchIssuesOut{}, err
	}
	res, err := s.api.issues.ListIssues(ctx, connect.NewRequest(&apiv1.ListIssuesRequest{Query: in.Query, Sort: sort, StatsPeriod: p,
		Projects: in.Projects, Environments: in.Environments, Customers: in.Customers, PageSize: limit(in.Limit, 20, 50), PageToken: in.Cursor}))
	if err != nil {
		return nil, searchIssuesOut{}, apiError(err)
	}
	out := searchIssuesOut{Issues: []IssueItem{}, TotalEstimate: res.Msg.GetTotalEstimate(), NextCursor: res.Msg.GetNextPageToken()}
	for _, i := range res.Msg.GetIssues() {
		out.Issues = append(out.Issues, issueItem(i))
	}
	return nil, out, nil
}

type getIssueIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Issue        string `json:"issue" jsonschema:"short id (e.g. KALEM-1A) or UUID"`
}

type getIssueOut struct {
	Issue             IssueItem      `json:"issue"`
	FirstRelease      string         `json:"first_release,omitempty"`
	LastRelease       string         `json:"last_release,omitempty"`
	ResolvedInRelease string         `json:"resolved_in_release,omitempty"`
	Location          string         `json:"location,omitempty" jsonschema:"top in-app frame: file:line in function"`
	Grouping          []string       `json:"grouping,omitempty" jsonschema:"components the grouping fingerprint was built from"`
	Tags              []TagSummary   `json:"tags,omitempty" jsonschema:"most common values of tags such as customer, release, environment, browser"`
	RecommendedEvent  *Event         `json:"recommended_event,omitempty" jsonschema:"the most informative recent event, with its stack trace"`
	PullRequests      []*PullRequest `json:"pull_requests,omitempty"`
	Owners            []IssueOwner   `json:"owners,omitempty" jsonschema:"members owning the issue by the project's ownership rules and CODEOWNERS; the first one is whom automatic assignment picks"`
	SuspectCommit     *SuspectCommit `json:"suspect_commit,omitempty" jsonschema:"the commit that last changed the failing lines (git blame), when it was found"`
}

// IssueOwner is a member who owns an issue.
type IssueOwner struct {
	Email string   `json:"email"`
	Name  string   `json:"name,omitempty"`
	Rules []string `json:"rules" jsonschema:"ownership rules naming the member, e.g. path:src/billing/*"`
}

// SuspectCommit is the commit that last changed an issue's failing lines.
type SuspectCommit struct {
	SHA         string `json:"sha"`
	Message     string `json:"message"`
	URL         string `json:"url,omitempty"`
	Author      string `json:"author" jsonschema:"author name and e-mail address as in the commit"`
	CommittedAt string `json:"committed_at,omitempty"`
	Location    string `json:"location" jsonschema:"repository file and line the commit changed"`
}

func (s *server) getIssue(ctx context.Context, req *mcpsdk.CallToolRequest, in getIssueIn) (*mcpsdk.CallToolResult, getIssueOut, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, getIssueOut{}, err
	}
	out, err := s.issueDetail(ctx, in.Issue)
	return nil, out, err
}

func (s *server) issueDetail(ctx context.Context, ref string) (getIssueOut, error) {
	res, err := s.api.issues.GetIssue(ctx, connect.NewRequest(&apiv1.GetIssueRequest{Issue: strings.TrimSpace(ref)}))
	if err != nil {
		return getIssueOut{}, apiError(err)
	}
	is := res.Msg.GetIssue()
	out := getIssueOut{Issue: issueItem(is.GetSummary()), FirstRelease: is.GetFirstRelease(), LastRelease: is.GetLastRelease(),
		ResolvedInRelease: is.GetResolvedInRelease(), Grouping: is.GetGrouping().GetComponents()}
	if l := is.GetLocation(); l.GetFilename() != "" || l.GetFunction() != "" {
		out.Location = fmt.Sprintf("%s:%d in %s", l.GetFilename(), l.GetLineno(), firstNonEmpty(l.GetFunction(), "?"))
	}
	for _, pr := range is.GetPullRequests() {
		out.PullRequests = append(out.PullRequests, pullRequest(pr))
	}
	id := is.GetSummary().GetId()
	if facets, err := s.api.issues.GetIssueTagFacets(ctx, connect.NewRequest(&apiv1.GetIssueTagFacetsRequest{IssueId: id})); err == nil {
		for _, f := range facets.Msg.GetFacets() {
			t := TagSummary{Key: f.GetKey(), Total: f.GetTotal(), Unique: f.GetUniqueValues()}
			for _, v := range f.GetValues()[:min(5, len(f.GetValues()))] {
				t.Top = append(t.Top, TagCount{Value: v.GetValue(), Label: v.GetLabel(), Count: v.GetCount()})
			}
			out.Tags = append(out.Tags, t)
		}
	}
	ev, err := s.api.events.GetEvent(ctx, connect.NewRequest(&apiv1.GetEventRequest{IssueId: id, Pick: apiv1.EventPick_EVENT_PICK_RECOMMENDED}))
	if err == nil {
		v := eventView(ev.Msg.GetEvent())
		out.RecommendedEvent = &v
	}
	if own, err := s.api.ownership.GetIssueOwners(ctx, connect.NewRequest(&apiv1.GetIssueOwnersRequest{IssueId: id})); err == nil {
		for _, o := range own.Msg.GetOwners() {
			out.Owners = append(out.Owners, IssueOwner{Email: o.GetUser().GetEmail(), Name: o.GetUser().GetName(), Rules: o.GetRules()})
		}
		if sc := own.Msg.GetSuspectCommit(); sc.GetState() == apiv1.SuspectCommitState_SUSPECT_COMMIT_STATE_FOUND {
			out.SuspectCommit = &SuspectCommit{SHA: sc.GetSha(), Message: sc.GetMessage(), URL: sc.GetUrl(),
				Author: strings.TrimSpace(sc.GetAuthorName() + " <" + sc.GetAuthorEmail() + ">"), CommittedAt: ts(sc.GetCommittedAt()),
				Location: fmt.Sprintf("%s:%d", sc.GetPath(), sc.GetLine())}
		}
	}
	return out, nil
}

type listIssueEventsIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Issue        string `json:"issue" jsonschema:"short id (e.g. KALEM-1A) or UUID"`
	Query        string `json:"query,omitempty" jsonschema:"filters such as 'environment:production customer:sivas release:1.4.2 user:42'"`
	Limit        int    `json:"limit,omitempty" jsonschema:"1-50 (default 20)"`
	Cursor       string `json:"cursor,omitempty"`
}

// EventRow is an event in a list.
type EventRow struct {
	ID          string `json:"id" jsonschema:"event id for get_event"`
	ReceivedAt  string `json:"received_at"`
	Level       string `json:"level"`
	Environment string `json:"environment,omitempty"`
	Release     string `json:"release,omitempty"`
	Customer    string `json:"customer,omitempty"`
	Tenant      string `json:"tenant,omitempty"`
	User        string `json:"user,omitempty"`
	URL         string `json:"url,omitempty"`
	Browser     string `json:"browser,omitempty"`
	OS          string `json:"os,omitempty"`
	Server      string `json:"server,omitempty"`
}

type listIssueEventsOut struct {
	Events     []EventRow `json:"events"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

func (s *server) listIssueEvents(ctx context.Context, req *mcpsdk.CallToolRequest, in listIssueEventsIn) (*mcpsdk.CallToolResult, listIssueEventsOut, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, listIssueEventsOut{}, err
	}
	id, err := s.issueID(ctx, in.Issue)
	if err != nil {
		return nil, listIssueEventsOut{}, err
	}
	res, err := s.api.issues.ListIssueEvents(ctx, connect.NewRequest(&apiv1.ListIssueEventsRequest{IssueId: id, Query: in.Query,
		PageSize: limit(in.Limit, 20, 50), PageToken: in.Cursor}))
	if err != nil {
		return nil, listIssueEventsOut{}, apiError(err)
	}
	out := listIssueEventsOut{Events: []EventRow{}, NextCursor: res.Msg.GetNextPageToken()}
	for _, e := range res.Msg.GetEvents() {
		out.Events = append(out.Events, EventRow{ID: e.GetId(), ReceivedAt: ts(e.GetReceivedAt()), Level: level(e.GetLevel()),
			Environment: e.GetEnvironment(), Release: e.GetRelease(), Customer: firstNonEmpty(e.GetCustomer().GetName(), e.GetCustomer().GetKey()),
			Tenant: e.GetTenant(), User: e.GetUser(), URL: e.GetUrl(), Browser: e.GetBrowser(), OS: e.GetOs(), Server: e.GetServerName()})
	}
	return nil, out, nil
}

type getEventIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Event        string `json:"event" jsonschema:"event id (from list_issue_events or an issue's recommended_event)"`
	Raw          bool   `json:"raw,omitempty" jsonschema:"return the full stored event as JSON instead of the summary"`
}

func (s *server) getEvent(ctx context.Context, req *mcpsdk.CallToolRequest, in getEventIn) (*mcpsdk.CallToolResult, any, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, nil, err
	}
	if in.Raw {
		res, err := s.api.events.GetRawEvent(ctx, connect.NewRequest(&apiv1.GetRawEventRequest{Id: in.Event}))
		if err != nil {
			return nil, nil, apiError(err)
		}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: res.Msg.GetJson()}}}, nil, nil
	}
	res, err := s.api.events.GetEvent(ctx, connect.NewRequest(&apiv1.GetEventRequest{Id: in.Event}))
	if err != nil {
		return nil, nil, apiError(err)
	}
	return nil, eventView(res.Msg.GetEvent()), nil
}

type issueStatsIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Issue        string `json:"issue" jsonschema:"short id (e.g. KALEM-1A) or UUID"`
	Period       string `json:"period,omitempty" jsonschema:"1h, 24h, 7d, 14d, 30d or 90d (default 24h)"`
	Environment  string `json:"environment,omitempty"`
}

// Bucket is one interval of an event series.
type Bucket struct {
	Start string `json:"start"`
	Count uint64 `json:"count"`
}

type issueStatsOut struct {
	Total    uint64   `json:"total"`
	Interval string   `json:"interval" jsonschema:"length of each bucket, e.g. 1h0m0s"`
	Buckets  []Bucket `json:"buckets"`
}

func (s *server) issueStats(ctx context.Context, req *mcpsdk.CallToolRequest, in issueStatsIn) (*mcpsdk.CallToolResult, issueStatsOut, error) {
	p, err := period(in.Period, apiv1.StatsPeriod_STATS_PERIOD_LAST_24_HOURS)
	if err != nil {
		return nil, issueStatsOut{}, err
	}
	ctx, err = s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, issueStatsOut{}, err
	}
	id, err := s.issueID(ctx, in.Issue)
	if err != nil {
		return nil, issueStatsOut{}, err
	}
	res, err := s.api.issues.GetIssueStats(ctx, connect.NewRequest(&apiv1.GetIssueStatsRequest{IssueId: id, Period: p, Environment: in.Environment}))
	if err != nil {
		return nil, issueStatsOut{}, apiError(err)
	}
	series := res.Msg.GetSeries()
	step := series.GetInterval().AsDuration()
	out := issueStatsOut{Total: res.Msg.GetTotal(), Interval: step.String(), Buckets: []Bucket{}}
	start := series.GetStart().AsTime()
	for i, c := range series.GetCounts() {
		out.Buckets = append(out.Buckets, Bucket{Start: start.Add(time.Duration(i) * step).UTC().Format(time.RFC3339), Count: c})
	}
	return nil, out, nil
}

type updateIssueIn struct {
	Organization         string   `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Issues               []string `json:"issues" jsonschema:"short ids or UUIDs, 1-100"`
	Status               string   `json:"status,omitempty" jsonschema:"resolved, ignored or unresolved; unchanged when empty"`
	ResolveInNextRelease bool     `json:"resolve_in_next_release,omitempty" jsonschema:"with status=resolved: resolved only from the next release on"`
	IgnoreMinutes        int      `json:"ignore_minutes,omitempty" jsonschema:"with status=ignored: ignore for this many minutes (0 = until changed)"`
	Assignee             string   `json:"assignee,omitempty" jsonschema:"member email, 'me', or 'none' to unassign; unchanged when empty"`
	Priority             string   `json:"priority,omitempty" jsonschema:"low, medium, high or critical; unchanged when empty"`
}

type updateIssueOut struct {
	Issues []IssueItem `json:"issues"`
}

func (s *server) updateIssue(ctx context.Context, req *mcpsdk.CallToolRequest, in updateIssueIn) (*mcpsdk.CallToolResult, updateIssueOut, error) {
	if len(in.Issues) == 0 || len(in.Issues) > 100 {
		return nil, updateIssueOut{}, errors.New("issues must list 1 to 100 issues")
	}
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, updateIssueOut{}, err
	}
	r := &apiv1.UpdateIssuesRequest{}
	for _, ref := range in.Issues {
		id, err := s.issueID(ctx, ref)
		if err != nil {
			return nil, updateIssueOut{}, err
		}
		r.IssueIds = append(r.IssueIds, id)
	}
	switch strings.ToLower(in.Status) {
	case "":
	case "resolved":
		r.Status = apiv1.IssueStatus_ISSUE_STATUS_RESOLVED.Enum()
		r.Resolve = &apiv1.ResolveOptions{InNextRelease: in.ResolveInNextRelease}
	case "ignored":
		r.Status = apiv1.IssueStatus_ISSUE_STATUS_IGNORED.Enum()
		if in.IgnoreMinutes > 0 {
			r.Ignore = &apiv1.IgnoreOptions{Duration: durationpb.New(time.Duration(in.IgnoreMinutes) * time.Minute)}
		}
	case "unresolved":
		r.Status = apiv1.IssueStatus_ISSUE_STATUS_UNRESOLVED.Enum()
	default:
		return nil, updateIssueOut{}, fmt.Errorf("status must be resolved, ignored or unresolved (got %q)", in.Status)
	}
	if p := strings.ToLower(in.Priority); p != "" {
		v, ok := apiv1.Priority_value["PRIORITY_"+strings.ToUpper(p)]
		if !ok || v == 0 {
			return nil, updateIssueOut{}, fmt.Errorf("priority must be low, medium, high or critical (got %q)", in.Priority)
		}
		r.Priority = apiv1.Priority(v).Enum()
	}
	if a := strings.TrimSpace(in.Assignee); a != "" {
		id, err := s.memberID(ctx, a)
		if err != nil {
			return nil, updateIssueOut{}, err
		}
		r.AssigneeId = &id
	}
	res, err := s.api.issues.UpdateIssues(ctx, connect.NewRequest(r))
	if err != nil {
		return nil, updateIssueOut{}, apiError(err)
	}
	out := updateIssueOut{Issues: []IssueItem{}}
	for _, i := range res.Msg.GetIssues() {
		out.Issues = append(out.Issues, issueItem(i))
	}
	return nil, out, nil
}

// memberID resolves "me", "none" or a member's email to a user id.
func (s *server) memberID(ctx context.Context, who string) (string, error) {
	switch strings.ToLower(who) {
	case "none":
		return "", nil
	case "me":
		res, err := s.api.auth.GetSession(ctx, connect.NewRequest(&apiv1.GetSessionRequest{}))
		if err != nil {
			return "", apiError(err)
		}
		return res.Msg.GetSession().GetUser().GetId(), nil
	}
	res, err := s.api.orgs.ListMembers(ctx, connect.NewRequest(&apiv1.ListMembersRequest{}))
	if err != nil {
		return "", apiError(err)
	}
	for _, m := range res.Msg.GetMembers() {
		if strings.EqualFold(m.GetUser().GetEmail(), who) {
			return m.GetUser().GetId(), nil
		}
	}
	return "", fmt.Errorf("%s is not a member of the organization", who)
}

type addCommentIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Issue        string `json:"issue" jsonschema:"short id (e.g. KALEM-1A) or UUID"`
	Text         string `json:"text" jsonschema:"comment text (Markdown), up to 10000 characters"`
}

type addCommentOut struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

func (s *server) addComment(ctx context.Context, req *mcpsdk.CallToolRequest, in addCommentIn) (*mcpsdk.CallToolResult, addCommentOut, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, addCommentOut{}, err
	}
	id, err := s.issueID(ctx, in.Issue)
	if err != nil {
		return nil, addCommentOut{}, err
	}
	res, err := s.api.issues.AddIssueComment(ctx, connect.NewRequest(&apiv1.AddIssueCommentRequest{IssueId: id, Text: in.Text}))
	if err != nil {
		return nil, addCommentOut{}, apiError(err)
	}
	return nil, addCommentOut{ID: res.Msg.GetActivity().GetId(), CreatedAt: ts(res.Msg.GetActivity().GetCreatedAt())}, nil
}

// ── Customers and releases ─────────────────────────────────────────────────

type listCustomersIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Query        string `json:"query,omitempty" jsonschema:"text matched against customer key and name"`
	Period       string `json:"period,omitempty" jsonschema:"1h, 24h, 7d, 14d, 30d or 90d (default 7d)"`
	Sort         string `json:"sort,omitempty" jsonschema:"events (default), issues, last_seen or name"`
	Limit        int    `json:"limit,omitempty" jsonschema:"1-50 (default 20)"`
	Cursor       string `json:"cursor,omitempty"`
}

// CustomerItem is a customer with its health in a period.
type CustomerItem struct {
	Key              string `json:"key" jsonschema:"customer key for get_customer_overview and customer: searches"`
	Name             string `json:"name,omitempty"`
	Health           string `json:"health" jsonschema:"healthy, degraded (errors) or critical (fatal, unhandled or escalating issues)"`
	UnresolvedIssues uint32 `json:"unresolved_issues"`
	NewIssues        uint32 `json:"new_issues"`
	PeriodEvents     uint64 `json:"period_events"`
	Projects         uint32 `json:"affected_projects"`
	LastSeen         string `json:"last_seen,omitempty"`
}

func customerItem(c *apiv1.CustomerSummary) CustomerItem {
	return CustomerItem{Key: c.GetCustomer().GetKey(), Name: c.GetCustomer().GetName(), Health: enumName(c.GetHealth().String(), "HEALTH_"),
		UnresolvedIssues: c.GetUnresolvedIssues(), NewIssues: c.GetNewIssues(), PeriodEvents: c.GetPeriodEvents(),
		Projects: c.GetAffectedProjects(), LastSeen: ts(c.GetCustomer().GetLastSeen())}
}

type listCustomersOut struct {
	Customers  []CustomerItem `json:"customers"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

var customerSorts = map[string]apiv1.CustomerSort{
	"": apiv1.CustomerSort_CUSTOMER_SORT_EVENTS, "events": apiv1.CustomerSort_CUSTOMER_SORT_EVENTS, "issues": apiv1.CustomerSort_CUSTOMER_SORT_ISSUES,
	"last_seen": apiv1.CustomerSort_CUSTOMER_SORT_LAST_SEEN, "name": apiv1.CustomerSort_CUSTOMER_SORT_NAME,
}

func (s *server) listCustomers(ctx context.Context, req *mcpsdk.CallToolRequest, in listCustomersIn) (*mcpsdk.CallToolResult, listCustomersOut, error) {
	sort, ok := customerSorts[strings.ToLower(in.Sort)]
	if !ok {
		return nil, listCustomersOut{}, fmt.Errorf("sort must be events, issues, last_seen or name (got %q)", in.Sort)
	}
	p, err := period(in.Period, apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS)
	if err != nil {
		return nil, listCustomersOut{}, err
	}
	ctx, err = s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, listCustomersOut{}, err
	}
	res, err := s.api.customers.ListCustomers(ctx, connect.NewRequest(&apiv1.ListCustomersRequest{Query: in.Query, Period: p, Sort: sort,
		PageSize: limit(in.Limit, 20, 50), PageToken: in.Cursor}))
	if err != nil {
		return nil, listCustomersOut{}, apiError(err)
	}
	out := listCustomersOut{Customers: []CustomerItem{}, NextCursor: res.Msg.GetNextPageToken()}
	for _, c := range res.Msg.GetCustomers() {
		out.Customers = append(out.Customers, customerItem(c))
	}
	return nil, out, nil
}

type customerOverviewIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Customer     string `json:"customer" jsonschema:"customer key"`
	Period       string `json:"period,omitempty" jsonschema:"1h, 24h, 7d, 14d, 30d or 90d (default 7d)"`
}

// CustomerProject is a customer's activity in one project.
type CustomerProject struct {
	Project          string `json:"project"`
	UnresolvedIssues uint32 `json:"unresolved_issues"`
	PeriodEvents     uint64 `json:"period_events"`
}

type customerOverviewOut struct {
	Customer   CustomerItem      `json:"customer"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Notes      string            `json:"notes,omitempty"`
	Projects   []CustomerProject `json:"projects"`
	TopIssues  []IssueItem       `json:"top_issues"`
}

func (s *server) customerOverview(ctx context.Context, req *mcpsdk.CallToolRequest, in customerOverviewIn) (*mcpsdk.CallToolResult, customerOverviewOut, error) {
	p, err := period(in.Period, apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS)
	if err != nil {
		return nil, customerOverviewOut{}, err
	}
	ctx, err = s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, customerOverviewOut{}, err
	}
	res, err := s.api.customers.GetCustomer(ctx, connect.NewRequest(&apiv1.GetCustomerRequest{Key: in.Customer, Period: p}))
	if err != nil {
		return nil, customerOverviewOut{}, apiError(err)
	}
	m := res.Msg
	out := customerOverviewOut{Customer: customerItem(m.GetSummary()), Attributes: m.GetSummary().GetCustomer().GetAttributes(),
		Notes: m.GetSummary().GetCustomer().GetNotes(), Projects: []CustomerProject{}, TopIssues: []IssueItem{}}
	for _, pb := range m.GetProjects() {
		out.Projects = append(out.Projects, CustomerProject{Project: pb.GetProject().GetSlug(), UnresolvedIssues: pb.GetUnresolvedIssues(),
			PeriodEvents: pb.GetPeriodEvents()})
	}
	for _, i := range m.GetTopIssues() {
		out.TopIssues = append(out.TopIssues, issueItem(i))
	}
	return nil, out, nil
}

type listReleasesIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Project      string `json:"project" jsonschema:"project slug"`
	Limit        int    `json:"limit,omitempty" jsonschema:"1-50 (default 20)"`
	Cursor       string `json:"cursor,omitempty"`
}

// ReleaseItem is a release of a project.
type ReleaseItem struct {
	Version   string   `json:"version"`
	Commit    string   `json:"commit,omitempty"`
	FirstSeen string   `json:"first_seen"`
	LastSeen  string   `json:"last_seen"`
	NewIssues uint32   `json:"new_issues" jsonschema:"issues first seen in this release"`
	Deploys   []string `json:"deploys,omitempty" jsonschema:"environment@time of deploys"`
}

type listReleasesOut struct {
	Releases   []ReleaseItem `json:"releases"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (s *server) listReleases(ctx context.Context, req *mcpsdk.CallToolRequest, in listReleasesIn) (*mcpsdk.CallToolResult, listReleasesOut, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, listReleasesOut{}, err
	}
	res, err := s.api.projects.ListReleases(ctx, connect.NewRequest(&apiv1.ListReleasesRequest{Project: in.Project,
		PageSize: limit(in.Limit, 20, 50), PageToken: in.Cursor}))
	if err != nil {
		return nil, listReleasesOut{}, apiError(err)
	}
	out := listReleasesOut{Releases: []ReleaseItem{}, NextCursor: res.Msg.GetNextPageToken()}
	for _, r := range res.Msg.GetReleases() {
		it := ReleaseItem{Version: r.GetVersion(), Commit: r.GetCommitSha(), FirstSeen: ts(r.GetFirstSeen()), LastSeen: ts(r.GetLastSeen()),
			NewIssues: r.GetNewIssues()}
		for _, d := range r.GetDeploys() {
			it.Deploys = append(it.Deploys, d.GetEnvironment()+"@"+ts(d.GetDeployedAt()))
		}
		out.Releases = append(out.Releases, it)
	}
	return nil, out, nil
}

// ── Cron monitors ──────────────────────────────────────────────────────────

type listMonitorsIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Project      string `json:"project,omitempty" jsonschema:"project slug; empty for every project"`
	Query        string `json:"query,omitempty" jsonschema:"text to find in the monitor name or slug"`
	Failing      bool   `json:"failing,omitempty" jsonschema:"only monitors whose last run failed, was missed or timed out in some environment"`
}

// MonitorItem is a cron monitor with its state per environment.
type MonitorItem struct {
	Slug         string           `json:"slug" jsonschema:"monitor_slug the job checks in with"`
	Name         string           `json:"name"`
	Project      string           `json:"project"`
	Schedule     string           `json:"schedule" jsonschema:"crontab expression, or every N units counted from the previous run"`
	Timezone     string           `json:"timezone"`
	Paused       bool             `json:"paused,omitempty"`
	Environments []MonitorEnvItem `json:"environments"`
	RecentRuns   []string         `json:"recent_runs,omitempty" jsonschema:"statuses of the latest runs, newest first"`
}

// MonitorEnvItem is the state of a monitor in one environment.
type MonitorEnvItem struct {
	Environment    string `json:"environment"`
	Status         string `json:"status" jsonschema:"waiting, ok, error, missed or timeout"`
	LastCheckIn    string `json:"last_check_in,omitempty"`
	NextCheckIn    string `json:"next_check_in,omitempty"`
	FailuresInARow uint32 `json:"failures_in_a_row,omitempty"`
	Issue          string `json:"issue,omitempty" jsonschema:"short id of the open issue raised for this environment"`
}

type listMonitorsOut struct {
	Monitors []MonitorItem `json:"monitors"`
}

func (s *server) listMonitors(ctx context.Context, req *mcpsdk.CallToolRequest, in listMonitorsIn) (*mcpsdk.CallToolResult, listMonitorsOut, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, listMonitorsOut{}, err
	}
	res, err := s.api.monitors.ListMonitors(ctx, connect.NewRequest(&apiv1.ListMonitorsRequest{Project: in.Project, Query: in.Query}))
	if err != nil {
		return nil, listMonitorsOut{}, apiError(err)
	}
	out := listMonitorsOut{Monitors: []MonitorItem{}}
	for _, m := range res.Msg.GetMonitors() {
		it := MonitorItem{Slug: m.GetSlug(), Name: m.GetName(), Project: m.GetProject().GetSlug(), Timezone: m.GetTimezone(),
			Paused: m.GetPaused(), Environments: []MonitorEnvItem{}}
		if iv := m.GetSchedule().GetInterval(); iv != nil {
			unit := enumName(iv.GetUnit().String(), "MONITOR_INTERVAL_UNIT_")
			if iv.GetValue() != 1 {
				unit += "s"
			}
			it.Schedule = fmt.Sprintf("every %d %s", iv.GetValue(), unit)
		} else {
			it.Schedule = m.GetSchedule().GetCrontab()
		}
		failing := false
		for _, e := range m.GetEnvironments() {
			status := e.GetStatus()
			if status == apiv1.MonitorStatus_MONITOR_STATUS_ERROR || status == apiv1.MonitorStatus_MONITOR_STATUS_MISSED ||
				status == apiv1.MonitorStatus_MONITOR_STATUS_TIMEOUT {
				failing = true
			}
			it.Environments = append(it.Environments, MonitorEnvItem{Environment: e.GetEnvironment(),
				Status: enumName(status.String(), "MONITOR_STATUS_"), LastCheckIn: ts(e.GetLastCheckInAt()),
				NextCheckIn: ts(e.GetNextCheckInAt()), FailuresInARow: e.GetFailures(), Issue: e.GetIssue().GetShortId()})
		}
		if in.Failing && (!failing || m.GetPaused()) {
			continue
		}
		for _, c := range m.GetRecentCheckIns() {
			it.RecentRuns = append(it.RecentRuns, enumName(c.GetStatus().String(), "CHECK_IN_STATUS_"))
		}
		out.Monitors = append(out.Monitors, it)
	}
	return nil, out, nil
}

// ── AI ─────────────────────────────────────────────────────────────────────

type startAnalysisIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Issue        string `json:"issue" jsonschema:"short id (e.g. KALEM-1A) or UUID"`
	Mode         string `json:"mode,omitempty" jsonschema:"fix (propose a code change; default when a repository is linked) or analyze (explanation only)"`
	Instructions string `json:"instructions,omitempty" jsonschema:"extra context for the agent, e.g. what you already know about the bug"`
	Event        string `json:"event,omitempty" jsonschema:"event id to focus on; default is the issue's most informative event"`
}

func (s *server) startAnalysis(ctx context.Context, req *mcpsdk.CallToolRequest, in startAnalysisIn) (*mcpsdk.CallToolResult, AIRun, error) {
	var mode apiv1.AiRunMode
	switch strings.ToLower(in.Mode) {
	case "":
	case "fix":
		mode = apiv1.AiRunMode_AI_RUN_MODE_FIX
	case "analyze":
		mode = apiv1.AiRunMode_AI_RUN_MODE_ANALYZE
	default:
		return nil, AIRun{}, fmt.Errorf("mode must be fix or analyze (got %q)", in.Mode)
	}
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, AIRun{}, err
	}
	id, err := s.issueID(ctx, in.Issue)
	if err != nil {
		return nil, AIRun{}, err
	}
	res, err := s.api.ai.StartRun(ctx, connect.NewRequest(&apiv1.StartRunRequest{IssueId: id, EventId: in.Event, Mode: mode,
		Instructions: in.Instructions}))
	if err != nil {
		return nil, AIRun{}, apiError(err)
	}
	return nil, aiRun(res.Msg.GetRun(), false), nil
}

type getRunIn struct {
	Organization string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Run          string `json:"run" jsonschema:"AI run id (from start_ai_analysis)"`
	IncludeDiff  bool   `json:"include_diff,omitempty" jsonschema:"include the analysis report and the proposed diff"`
}

func (s *server) getRun(ctx context.Context, req *mcpsdk.CallToolRequest, in getRunIn) (*mcpsdk.CallToolResult, AIRun, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, AIRun{}, err
	}
	res, err := s.api.ai.GetRun(ctx, connect.NewRequest(&apiv1.GetRunRequest{Id: in.Run}))
	if err != nil {
		return nil, AIRun{}, apiError(err)
	}
	return nil, aiRun(res.Msg.GetRun(), in.IncludeDiff), nil
}

type createPRIn struct {
	Organization   string `json:"organization,omitempty" jsonschema:"organization slug; defaults to the Izci-Org header, the organization the token is bound to or the user's only organization"`
	Run            string `json:"run" jsonschema:"id of a succeeded AI run with a proposed change"`
	ReadyForReview bool   `json:"ready_for_review,omitempty" jsonschema:"open a regular pull request instead of a draft (default draft)"`
	Title          string `json:"title,omitempty" jsonschema:"default: fix: <issue title> (<short id>)"`
	BaseBranch     string `json:"base_branch,omitempty" jsonschema:"target branch; default: the branch the analysis ran on"`
}

func (s *server) createPullRequest(ctx context.Context, req *mcpsdk.CallToolRequest, in createPRIn) (*mcpsdk.CallToolResult, PullRequest, error) {
	ctx, err := s.scoped(ctx, req, in.Organization)
	if err != nil {
		return nil, PullRequest{}, err
	}
	res, err := s.api.ai.CreatePullRequest(ctx, connect.NewRequest(&apiv1.CreatePullRequestRequest{RunId: in.Run, Draft: !in.ReadyForReview,
		Title: in.Title, BaseBranch: in.BaseBranch}))
	if err != nil {
		return nil, PullRequest{}, apiError(err)
	}
	return nil, *pullRequest(res.Msg.GetPullRequest()), nil
}

// registerTools adds every tool to the MCP server.
func (s *server) registerTools(m *mcpsdk.Server) {
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "list_organizations", Annotations: readOnly,
		Description: "List the izci organizations (tenants) the access token can use, with the caller's role. Pass a slug as `organization` to other tools when there are several."},
		s.listOrganizations)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "list_projects", Annotations: readOnly,
		Description: "List the projects (monitored applications) of an organization with unresolved issue counts, events in the last 24 hours and the linked repository."},
		s.listProjects)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "get_project_setup", Annotations: readOnly,
		Description: "Explain how an application sends errors to a project: ingest endpoints (gRPC, Connect JSON) and the Sentry DSN template."},
		s.projectSetup)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "search_issues", Annotations: readOnly,
		Description: "Search issues (grouped errors) with izci's search language, e.g. `is:unresolved level:>=error customer:sivas-belediyesi release:1.4 \"timeout\"`. " +
			"Returns short ids, titles, levels, status, event/user/customer counts and links. Use get_issue for details."},
		s.searchIssues)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "get_issue", Annotations: readOnly,
		Description: "Get one issue by short id (e.g. KALEM-1A): status, releases, grouping, tag distributions (customers, releases, browsers…), pull requests, " +
			"owners by ownership rules and CODEOWNERS, the suspect commit (last change of the failing lines) when found, " +
			"and the most informative recent event with its exception chain, stack trace (most recent call first), breadcrumbs, request and runtime."},
		s.getIssue)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "list_issue_events", Annotations: readOnly,
		Description: "List the events (occurrences) of an issue, newest first, optionally filtered, e.g. `environment:production customer:x release:1.2`. Use get_event for one event's details."},
		s.listIssueEvents)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "get_event", Annotations: readOnly,
		Description: "Get one event: exception chain with stack frames, breadcrumbs (steps before the error), HTTP request, user, customer, runtime/OS/browser/device contexts, database statement and tags. raw=true returns the stored JSON."},
		s.getEvent)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "get_issue_stats", Annotations: readOnly,
		Description: "Count an issue's events over time (hourly or daily buckets) to see whether it is growing, stopped or started with a release."},
		s.issueStats)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "list_customers", Annotations: readOnly,
		Description: "List customers (tenants of the monitored applications, e.g. municipalities) with health, unresolved and new issues and event counts in a period."},
		s.listCustomers)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "get_customer_overview", Annotations: readOnly,
		Description: "Get one customer's health: per-project activity and the issues affecting it most. Customer keys come from list_customers."},
		s.customerOverview)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "list_releases", Annotations: readOnly,
		Description: "List a project's releases with commits, deploys and how many issues each release introduced."},
		s.listReleases)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "list_monitors", Annotations: readOnly,
		Description: "List cron monitors (scheduled jobs that check in, like Sentry Crons) with their schedule and, per environment, the last run's status " +
			"(ok, error, missed, timeout), next expected run and the open issue. Use failing=true to find jobs that are not running correctly."},
		s.listMonitors)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "get_ai_run", Annotations: readOnly,
		Description: "Get an AI analysis run: status, root cause, proposed fix summary, confidence, risk, changed files and (include_diff) the full report and diff."},
		s.getRun)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "update_issue", Annotations: changes,
		Description: "Triage issues: set status (resolved, ignored, unresolved), assignee (email, me, none) and priority. Requires a token with write scope and the member role."},
		s.updateIssue)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "add_comment", Annotations: appends,
		Description: "Add a comment to an issue's activity, e.g. findings of an investigation. Requires write scope."},
		s.addComment)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "start_ai_analysis", Annotations: appends,
		Description: "Start izci's AI analysis of an issue: the server checks out the linked repository at the failing release and lets an agent find the root cause " +
			"and propose a fix. Returns the queued run; poll get_ai_run until status is succeeded or failed. Requires the ai scope; counts against the monthly AI limit."},
		s.startAnalysis)
	mcpsdk.AddTool(m, &mcpsdk.Tool{Name: "create_fix_pull_request", Annotations: publishesPR,
		Description: "Push the change proposed by a succeeded AI run to a new branch and open a pull request on GitHub (a draft unless ready_for_review). " +
			"Calling it again returns the existing pull request. Requires the ai scope and a repository linked through a GitHub integration."},
		s.createPullRequest)
}

// jsonText renders v for text resources.
func jsonText(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
