package api_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/demo"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// seeded creates the "kentos" organization through setup and fills it with
// deterministic demo data.
func seeded(t *testing.T) (*env, *client) {
	t.Helper()
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem",
		ProjectPlatform: "dotnet"})); err != nil {
		t.Fatal(err)
	}
	if _, err := demo.Seed(ctx, e.db, e.log, demo.Options{OrgSlug: "kentos", Events: 800, Days: 3, Seed: 7}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return e, owner
}

func listIssues(t *testing.T, c *client, query string) []*apiv1.IssueSummary {
	t.Helper()
	res, err := c.issue.ListIssues(context.Background(), req(&apiv1.ListIssuesRequest{Query: query, StatsPeriod: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS}))
	if err != nil {
		t.Fatalf("list issues %q: %v", query, err)
	}
	return res.Msg.GetIssues()
}

func TestIssuesAndEvents(t *testing.T) {
	_, owner := seeded(t)
	ctx := context.Background()

	all := listIssues(t, owner, "")
	// Six demo issues, one of which the seed resolves.
	if len(all) != 5 {
		t.Fatalf("expected the unresolved demo issues, got %d", len(all))
	}
	for _, is := range all {
		if is.GetStatus() != apiv1.IssueStatus_ISSUE_STATUS_UNRESOLVED || is.GetSeries() == nil || len(is.GetSeries().GetCounts()) != 28 {
			t.Fatalf("summary: %v", is)
		}
		if !strings.HasPrefix(is.GetPermalink(), "http://izci.test/o/kentos/issues/") || is.GetProject().GetName() == "" {
			t.Fatalf("permalink/project: %v", is)
		}
	}

	t.Run("search", func(t *testing.T) {
		for _, tc := range []struct {
			query string
			check func(*apiv1.IssueSummary) bool
		}{
			{"level:fatal", func(i *apiv1.IssueSummary) bool { return i.GetLevel() == ingestv1.Level_LEVEL_FATAL }},
			{"!level:error level:>=warning", func(i *apiv1.IssueSummary) bool { return i.GetLevel() != ingestv1.Level_LEVEL_ERROR }},
			{"project:kalem-web", func(i *apiv1.IssueSummary) bool { return i.GetProject().GetSlug() == "kalem-web" }},
			{"customer:sivas-belediyesi", func(i *apiv1.IssueSummary) bool { return i.GetCustomersCount() > 0 }},
			{"type:*Exception", func(i *apiv1.IssueSummary) bool { return strings.HasSuffix(i.GetType(), "Exception") }},
			{"times_seen:>10", func(i *apiv1.IssueSummary) bool { return i.GetTimesSeen() > 10 }},
			{`"reference not set"`, func(i *apiv1.IssueSummary) bool { return strings.Contains(i.GetTitle(), "reference not set") }},
			{"is:unhandled", func(i *apiv1.IssueSummary) bool { return i.GetUnhandled() }},
			// Wildcards on tag keys (release, browser, …) and on other keys.
			{"release:*.* !release:0.*", func(*apiv1.IssueSummary) bool { return true }},
			{`environment:"prod*"`, func(*apiv1.IssueSummary) bool { return true }},
		} {
			got := listIssues(t, owner, tc.query)
			if len(got) == 0 {
				t.Errorf("%q matched nothing", tc.query)
			}
			for _, i := range got {
				if !tc.check(i) {
					t.Errorf("%q returned %s (%s)", tc.query, i.GetShortId(), i.GetTitle())
				}
			}
		}
		if got := listIssues(t, owner, "project:nope"); len(got) != 0 {
			t.Errorf("unknown project matched %d issues", len(got))
		}
		_, err := owner.issue.ListIssues(ctx, req(&apiv1.ListIssuesRequest{Query: "level:loud"}))
		if reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_QUERY {
			t.Errorf("invalid query: %v", err)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		first, err := owner.issue.ListIssues(ctx, req(&apiv1.ListIssuesRequest{PageSize: 2, StatsPeriod: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS}))
		if err != nil || len(first.Msg.GetIssues()) != 2 || first.Msg.GetNextPageToken() == "" || first.Msg.GetTotalEstimate() != 5 {
			t.Fatalf("first page: %v %v", first, err)
		}
		second, err := owner.issue.ListIssues(ctx, req(&apiv1.ListIssuesRequest{PageSize: 2, PageToken: first.Msg.GetNextPageToken(),
			StatsPeriod: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS}))
		if err != nil || len(second.Msg.GetIssues()) != 2 || second.Msg.GetIssues()[0].GetId() == first.Msg.GetIssues()[0].GetId() {
			t.Fatalf("second page: %v %v", second, err)
		}
		sorted, err := owner.issue.ListIssues(ctx, req(&apiv1.ListIssuesRequest{Sort: apiv1.IssueSort_ISSUE_SORT_EVENTS,
			StatsPeriod: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS}))
		if err != nil {
			t.Fatal(err)
		}
		list := sorted.Msg.GetIssues()
		for i := 1; i < len(list); i++ {
			if list[i].GetPeriodCount() > list[i-1].GetPeriodCount() {
				t.Fatalf("events sort out of order at %d", i)
			}
		}
	})

	target := listIssues(t, owner, "type:System.NullReferenceException")[0]

	t.Run("detail", func(t *testing.T) {
		byShort, err := owner.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: strings.ToLower(target.GetShortId())}))
		if err != nil {
			t.Fatalf("get by short id: %v", err)
		}
		is := byShort.Msg.GetIssue()
		if is.GetSummary().GetId() != target.GetId() || is.GetLatestEventId() == "" || len(is.GetGrouping().GetComponents()) == 0 ||
			len(is.GetGrouping().GetFingerprint()) != 32 || !strings.HasSuffix(is.GetLocation().GetFilename(), "OrderService.cs") {
			t.Fatalf("issue detail: %v", is)
		}
		if _, err := owner.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: "KALEM-ZZZZ"})); connect.CodeOf(err) != connect.CodeNotFound {
			t.Fatalf("unknown short id: %v", err)
		}

		stats, err := owner.issue.GetIssueStats(ctx, req(&apiv1.GetIssueStatsRequest{IssueId: target.GetId(), Period: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS}))
		if err != nil || len(stats.Msg.GetSeries().GetCounts()) != 28 || stats.Msg.GetTotal() == 0 {
			t.Fatalf("stats: %v %v", stats, err)
		}
		prod, err := owner.issue.GetIssueStats(ctx, req(&apiv1.GetIssueStatsRequest{IssueId: target.GetId(), Environment: "production",
			Period: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS}))
		if err != nil || prod.Msg.GetTotal() == 0 || prod.Msg.GetTotal() > stats.Msg.GetTotal() {
			t.Fatalf("environment stats: %v %v", prod, err)
		}

		facets, err := owner.issue.GetIssueTagFacets(ctx, req(&apiv1.GetIssueTagFacetsRequest{IssueId: target.GetId()}))
		if err != nil {
			t.Fatal(err)
		}
		var customer *apiv1.TagFacet
		for _, f := range facets.Msg.GetFacets() {
			if f.GetKey() == "customer" {
				customer = f
			}
		}
		if customer == nil || customer.GetUniqueValues() == 0 || customer.GetValues()[0].GetLabel() == "" ||
			!strings.HasSuffix(customer.GetValues()[0].GetValue(), "belediyesi") {
			t.Fatalf("customer facet: %v", customer)
		}
	})

	t.Run("events", func(t *testing.T) {
		page, err := owner.issue.ListIssueEvents(ctx, req(&apiv1.ListIssueEventsRequest{IssueId: target.GetId(), PageSize: 5}))
		if err != nil || len(page.Msg.GetEvents()) != 5 || page.Msg.GetNextPageToken() == "" {
			t.Fatalf("events page: %v %v", page, err)
		}
		next, err := owner.issue.ListIssueEvents(ctx, req(&apiv1.ListIssueEventsRequest{IssueId: target.GetId(), PageSize: 5,
			PageToken: page.Msg.GetNextPageToken()}))
		if err != nil || len(next.Msg.GetEvents()) == 0 || next.Msg.GetEvents()[0].GetId() == page.Msg.GetEvents()[4].GetId() {
			t.Fatalf("events next page: %v %v", next, err)
		}
		staging, err := owner.issue.ListIssueEvents(ctx, req(&apiv1.ListIssueEventsRequest{IssueId: target.GetId(), Query: "environment:staging customer:sivas-belediyesi"}))
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range staging.Msg.GetEvents() {
			if ev.GetEnvironment() != "staging" || ev.GetCustomer().GetKey() != "sivas-belediyesi" || ev.GetCustomer().GetName() != "Sivas Belediyesi" {
				t.Fatalf("filtered event: %v", ev)
			}
		}
		if _, err := owner.issue.ListIssueEvents(ctx, req(&apiv1.ListIssueEventsRequest{IssueId: target.GetId(), Query: "is:resolved"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_QUERY {
			t.Fatalf("issue key in event search: %v", err)
		}

		latest, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{IssueId: target.GetId()}))
		if err != nil {
			t.Fatal(err)
		}
		d := latest.Msg.GetEvent()
		if d.GetIssue().GetShortId() != target.GetShortId() || d.GetEvent().GetExceptions()[0].GetType() != "System.NullReferenceException" ||
			latest.Msg.GetNextId() != "" || latest.Msg.GetPreviousId() == "" || d.GetUserAgent().GetBrowser() == "" {
			t.Fatalf("latest event: %v", latest.Msg)
		}
		for _, h := range d.GetEvent().GetRequest().GetHeaders() {
			if h.GetName() == "Authorization" && h.GetValue() != "[Filtered]" {
				t.Fatalf("authorization header not scrubbed: %q", h.GetValue())
			}
		}
		prev, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{Id: latest.Msg.GetPreviousId()}))
		if err != nil || prev.Msg.GetNextId() != d.GetId() {
			t.Fatalf("previous event: %v %v", prev, err)
		}
		byClient, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{ClientEventId: d.GetEventId()}))
		if err != nil || byClient.Msg.GetEvent().GetId() != d.GetId() {
			t.Fatalf("by client id: %v %v", byClient, err)
		}
		oldest, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{IssueId: target.GetId(), Pick: apiv1.EventPick_EVENT_PICK_OLDEST}))
		if err != nil || oldest.Msg.GetPreviousId() != "" {
			t.Fatalf("oldest event: %v %v", oldest, err)
		}
		if _, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{IssueId: target.GetId(), Pick: apiv1.EventPick_EVENT_PICK_RECOMMENDED})); err != nil {
			t.Fatalf("recommended event: %v", err)
		}
		raw, err := owner.event.GetRawEvent(ctx, req(&apiv1.GetRawEventRequest{Id: d.GetId()}))
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw.Msg.GetJson()), &decoded); err != nil || decoded["exceptions"] == nil || !strings.Contains(raw.Msg.GetJson(), "\n  ") {
			t.Fatalf("raw event: %v %s", err, raw.Msg.GetJson()[:min(200, len(raw.Msg.GetJson()))])
		}
		if _, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
			t.Fatalf("empty selector: %v", err)
		}
	})

	t.Run("triage", func(t *testing.T) {
		sess, err := owner.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		me := sess.Msg.GetSession().GetUser().GetId()
		high := apiv1.Priority_PRIORITY_HIGH
		resolved := apiv1.IssueStatus_ISSUE_STATUS_RESOLVED
		upd, err := owner.issue.UpdateIssues(ctx, req(&apiv1.UpdateIssuesRequest{IssueIds: []string{target.GetId()}, Status: &resolved,
			Resolve: &apiv1.ResolveOptions{InRelease: "1.5.0"}, AssigneeId: &me, Priority: &high}))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		got := upd.Msg.GetIssues()[0]
		if got.GetStatus() != resolved || got.GetAssignee().GetId() != me || got.GetPriority() != high {
			t.Fatalf("updated issue: %v", got)
		}
		if slices.ContainsFunc(listIssues(t, owner, ""), func(i *apiv1.IssueSummary) bool { return i.GetId() == target.GetId() }) {
			t.Fatal("resolved issue still listed as unresolved")
		}
		if got := listIssues(t, owner, "is:resolved assigned:me"); len(got) != 1 || got[0].GetId() != target.GetId() {
			t.Fatalf("is:resolved assigned:me: %v", got)
		}
		detail, _ := owner.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: target.GetId()}))
		if detail.Msg.GetIssue().GetResolvedInRelease() != "1.5.0" || detail.Msg.GetIssue().GetResolvedBy().GetId() != me {
			t.Fatalf("resolution details: %v", detail.Msg.GetIssue())
		}

		ignored := apiv1.IssueStatus_ISSUE_STATUS_IGNORED
		if _, err := owner.issue.UpdateIssues(ctx, req(&apiv1.UpdateIssuesRequest{IssueIds: []string{target.GetId()}, Status: &ignored,
			Ignore: &apiv1.IgnoreOptions{Count: 100, Duration: durationpb.New(time.Hour)}})); err != nil {
			t.Fatalf("ignore: %v", err)
		}
		detail, _ = owner.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: target.GetId()}))
		if sn := detail.Msg.GetIssue().GetSnooze(); sn.GetUntilCount() != 100 || sn.GetUntil() == nil || sn.GetCountSince() != 0 {
			t.Fatalf("snooze: %v", sn)
		}
		unresolved := apiv1.IssueStatus_ISSUE_STATUS_UNRESOLVED
		none := ""
		if _, err := owner.issue.UpdateIssues(ctx, req(&apiv1.UpdateIssuesRequest{IssueIds: []string{target.GetId()}, Status: &unresolved, AssigneeId: &none})); err != nil {
			t.Fatalf("unresolve: %v", err)
		}
		if _, err := owner.issue.AddIssueComment(ctx, req(&apiv1.AddIssueCommentRequest{IssueId: target.GetId(), Text: "  Sivas'ta yeniden üretildi.  "})); err != nil {
			t.Fatalf("comment: %v", err)
		}
		act, err := owner.issue.ListIssueActivity(ctx, req(&apiv1.ListIssueActivityRequest{IssueId: target.GetId(), PageSize: 3}))
		if err != nil || len(act.Msg.GetActivities()) != 3 || act.Msg.GetNextPageToken() == "" {
			t.Fatalf("activity: %v %v", act, err)
		}
		if a := act.Msg.GetActivities()[0]; a.GetKind() != apiv1.ActivityKind_ACTIVITY_KIND_COMMENT || a.GetText() != "Sivas'ta yeniden üretildi." || a.GetActor().GetId() != me {
			t.Fatalf("latest activity: %v", a)
		}
		rest, err := owner.issue.ListIssueActivity(ctx, req(&apiv1.ListIssueActivityRequest{IssueId: target.GetId(), PageToken: act.Msg.GetNextPageToken()}))
		if err != nil || len(rest.Msg.GetActivities()) < 4 || rest.Msg.GetActivities()[len(rest.Msg.GetActivities())-1].GetKind() != apiv1.ActivityKind_ACTIVITY_KIND_CREATED {
			t.Fatalf("remaining activity: %v %v", rest, err)
		}
		if _, err := owner.issue.UpdateIssues(ctx, req(&apiv1.UpdateIssuesRequest{IssueIds: []string{target.GetId()}, AssigneeId: ptr("0190c0de-0000-7000-8000-000000000000")})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
			t.Fatalf("assigning a non-member: %v", err)
		}
	})

	t.Run("suggest", func(t *testing.T) {
		keys, err := owner.issue.SuggestSearch(ctx, req(&apiv1.SuggestSearchRequest{Query: "is:unresolved lev", Cursor: 17}))
		if err != nil || len(keys.Msg.GetSuggestions()) != 1 || keys.Msg.GetSuggestions()[0].GetText() != "level:" ||
			keys.Msg.GetReplaceFrom() != 14 || keys.Msg.GetReplaceTo() != 17 {
			t.Fatalf("key suggestions: %v %v", keys, err)
		}
		customers, err := owner.issue.SuggestSearch(ctx, req(&apiv1.SuggestSearchRequest{Query: "customer:siv", Cursor: 12}))
		if err != nil || len(customers.Msg.GetSuggestions()) != 1 || customers.Msg.GetSuggestions()[0].GetText() != "customer:sivas-belediyesi" ||
			customers.Msg.GetSuggestions()[0].GetLabel() != "Sivas Belediyesi" {
			t.Fatalf("customer suggestions: %v %v", customers, err)
		}
		projects, err := owner.issue.SuggestSearch(ctx, req(&apiv1.SuggestSearchRequest{Query: "!project:ka", Cursor: 11}))
		if err != nil || len(projects.Msg.GetSuggestions()) != 2 || projects.Msg.GetSuggestions()[0].GetText() != "!project:kalem" {
			t.Fatalf("project suggestions: %v %v", projects, err)
		}
		browsers, err := owner.issue.SuggestSearch(ctx, req(&apiv1.SuggestSearchRequest{Query: "browser:", Cursor: 8}))
		if err != nil || len(browsers.Msg.GetSuggestions()) == 0 || browsers.Msg.GetSuggestions()[0].GetCount() == 0 {
			t.Fatalf("browser suggestions: %v %v", browsers, err)
		}
	})
}

func ptr[T any](v T) *T { return &v }

func TestProjectsAndKeys(t *testing.T) {
	e, owner := seeded(t)
	ctx := context.Background()

	list, err := owner.project.ListProjects(ctx, req(&apiv1.ListProjectsRequest{}))
	if err != nil || len(list.Msg.GetProjects()) != 4 {
		t.Fatalf("projects: %v %v", list, err)
	}
	for _, p := range list.Msg.GetProjects() {
		if p.GetStats().GetUnresolvedIssues() == 0 || len(p.GetStats().GetSeries_24H().GetCounts()) != 24 || p.GetPublicId() == 0 {
			t.Fatalf("project stats: %v", p)
		}
	}

	created, err := owner.project.CreateProject(ctx, req(&apiv1.CreateProjectRequest{Name: "Şikâyet Yönetimi", Platform: "Go"}))
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	p := created.Msg.GetProject()
	if p.GetSlug() != "sikayet-yonetimi" || p.GetPlatform() != "go" || !strings.HasPrefix(created.Msg.GetApiKeySecret(), "izk_") ||
		len(created.Msg.GetApiKey().GetScopes()) != 2 {
		t.Fatalf("created project: %v", created.Msg)
	}
	if _, err := owner.project.CreateProject(ctx, req(&apiv1.CreateProjectRequest{Name: "X", Slug: "sikayet-yonetimi"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_SLUG_TAKEN {
		t.Fatalf("duplicate slug: %v", err)
	}

	key, err := owner.project.CreateApiKey(ctx, req(&apiv1.CreateApiKeyRequest{Project: p.GetSlug(), Name: "tarayıcı",
		AllowedOrigins: []string{"https://Kalem.Example.com/"}, RateLimitPerMinute: 600}))
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	k := key.Msg.GetKey()
	if k.GetAllowedOrigins()[0] != "https://kalem.example.com" || k.GetScopes()[0] != apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST ||
		!strings.HasPrefix(key.Msg.GetSentryDsn(), "http://izk_") || k.GetCreatedBy().GetEmail() != "owner@example.com" {
		t.Fatalf("key: %v", key.Msg)
	}
	if _, err := owner.project.CreateApiKey(ctx, req(&apiv1.CreateApiKeyRequest{Project: p.GetSlug(), Name: "bad",
		AllowedOrigins: []string{"https://example.com/path"}})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("invalid origin: %v", err)
	}
	upd, err := owner.project.UpdateApiKey(ctx, req(&apiv1.UpdateApiKeyRequest{Project: p.GetSlug(), KeyId: k.GetId(), Enabled: false}))
	if err != nil || upd.Msg.GetKey().GetEnabled() || upd.Msg.GetKey().GetName() != "tarayıcı" || len(upd.Msg.GetKey().GetAllowedOrigins()) != 0 {
		t.Fatalf("update key: %v %v", upd, err)
	}
	if _, err := owner.project.RevokeApiKey(ctx, req(&apiv1.RevokeApiKeyRequest{Project: p.GetSlug(), KeyId: k.GetId()})); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	keys, _ := owner.project.ListApiKeys(ctx, req(&apiv1.ListApiKeysRequest{Project: p.GetSlug()}))
	all, _ := owner.project.ListApiKeys(ctx, req(&apiv1.ListApiKeysRequest{Project: p.GetSlug(), IncludeRevoked: true}))
	if len(keys.Msg.GetKeys()) != 1 || len(all.Msg.GetKeys()) != 2 {
		t.Fatalf("key lists: %d active, %d total", len(keys.Msg.GetKeys()), len(all.Msg.GetKeys()))
	}

	ep, err := owner.project.GetIngestEndpoints(ctx, req(&apiv1.GetIngestEndpointsRequest{Project: p.GetSlug()}))
	if err != nil || ep.Msg.GetGrpcTarget() != "izci.test:80" || ep.Msg.GetGrpcTls() ||
		ep.Msg.GetSentryDsnTemplate() != "http://<key>@izci.test/"+itoa(p.GetPublicId()) {
		t.Fatalf("endpoints: %v %v", ep, err)
	}

	settings := p.GetSettings()
	settings.IgnoredEnvironments = []string{" Staging "}
	settings.Privacy.IpStorage = apiv1.IpStorage_IP_STORAGE_NONE
	up, err := owner.project.UpdateProject(ctx, req(&apiv1.UpdateProjectRequest{Slug: p.GetSlug(), Name: "Şikâyet", Settings: settings}))
	if err != nil || up.Msg.GetProject().GetName() != "Şikâyet" || up.Msg.GetProject().GetSettings().GetIgnoredEnvironments()[0] != "staging" {
		t.Fatalf("update project: %v %v", up, err)
	}

	// Test events go through the pipeline and group into one issue.
	sent, err := owner.project.SendTestEvent(ctx, req(&apiv1.SendTestEventRequest{Project: p.GetSlug()}))
	if err != nil || !strings.HasPrefix(sent.Msg.GetIssueShortId(), "SIKAYET-YONETIMI-") {
		t.Fatalf("test event: %v %v", sent, err)
	}
	again, err := owner.project.SendTestEvent(ctx, req(&apiv1.SendTestEventRequest{Project: p.GetSlug()}))
	if err != nil || again.Msg.GetIssueId() != sent.Msg.GetIssueId() {
		t.Fatalf("second test event: %v %v", again, err)
	}
	ev, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{Id: sent.Msg.GetEventId()}))
	if err != nil || ev.Msg.GetEvent().GetClientIp() != "" || ev.Msg.GetEvent().GetEvent().GetTags()["izci_test"] != "true" {
		t.Fatalf("test event detail (IP storage NONE): %v %v", ev, err)
	}

	envs, err := owner.project.ListEnvironments(ctx, req(&apiv1.ListEnvironmentsRequest{}))
	if err != nil || len(envs.Msg.GetEnvironments()) != 2 || envs.Msg.GetEnvironments()[0].GetName() != "production" {
		t.Fatalf("environments: %v %v", envs, err)
	}
	rel, err := owner.project.ListReleases(ctx, req(&apiv1.ListReleasesRequest{Project: "kalem", PageSize: 1}))
	if err != nil || len(rel.Msg.GetReleases()) != 1 || rel.Msg.GetNextPageToken() == "" {
		t.Fatalf("releases: %v %v", rel, err)
	}
	rel2, err := owner.project.ListReleases(ctx, req(&apiv1.ListReleasesRequest{Project: "kalem", PageToken: rel.Msg.GetNextPageToken()}))
	if err != nil || len(rel2.Msg.GetReleases()) == 0 || rel2.Msg.GetReleases()[0].GetVersion() == rel.Msg.GetReleases()[0].GetVersion() {
		t.Fatalf("releases page 2: %v %v", rel2, err)
	}

	link, err := owner.project.LinkRepository(ctx, req(&apiv1.LinkRepositoryRequest{Project: "kalem", Repository: "https://github.com/kentos/kalem.git",
		PathMappings: []*apiv1.PathMapping{{FromPrefix: "/app/", ToPrefix: "src/"}}}))
	if err != nil || link.Msg.GetRepository().GetOwner() != "kentos" || link.Msg.GetRepository().GetName() != "kalem" ||
		link.Msg.GetRepository().GetDefaultBranch() != "main" || len(link.Msg.GetRepository().GetPathMappings()) != 1 {
		t.Fatalf("link repository: %v %v", link, err)
	}
	kalemIssue := listIssues(t, owner, "project:kalem")[0]
	kev, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{IssueId: kalemIssue.GetId()}))
	if err != nil || kev.Msg.GetEvent().GetRepositoryUrl() != "https://github.com/kentos/kalem" || kev.Msg.GetEvent().GetSourceRef() != "main" ||
		kev.Msg.GetEvent().GetPathRewrites()[0].GetToPrefix() != "src/" {
		t.Fatalf("source links: %v %v", kev, err)
	}
	if _, err := owner.project.LinkRepository(ctx, req(&apiv1.LinkRepositoryRequest{Project: "kalem", Repository: "not a repo"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("invalid repository: %v", err)
	}
	if _, err := owner.project.UnlinkRepository(ctx, req(&apiv1.UnlinkRepositoryRequest{Project: "kalem"})); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.project.UnlinkRepository(ctx, req(&apiv1.UnlinkRepositoryRequest{Project: "kalem"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_REPOSITORY_NOT_LINKED {
		t.Fatalf("second unlink: %v", err)
	}

	if _, err := owner.project.DeleteProject(ctx, req(&apiv1.DeleteProjectRequest{Slug: p.GetSlug(), ConfirmSlug: "yanlis"})); field(err) != "confirm_slug" {
		t.Fatalf("delete without confirmation: %v", err)
	}
	if _, err := owner.project.DeleteProject(ctx, req(&apiv1.DeleteProjectRequest{Slug: p.GetSlug(), ConfirmSlug: p.GetSlug()})); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := owner.project.GetProject(ctx, req(&apiv1.GetProjectRequest{Slug: p.GetSlug()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("deleted project still visible: %v", err)
	}
	purged, err := e.db.PurgeDeletedProjects(ctx, 10)
	if err != nil || len(purged) != 1 || purged[0] != "sikayet-yonetimi" {
		t.Fatalf("purge: %v %v", purged, err)
	}
	if _, err := owner.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: sent.Msg.GetIssueId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("purged issue still readable: %v", err)
	}
}

func itoa(n uint64) string {
	b := []byte{}
	for {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
		if n == 0 {
			return string(b)
		}
	}
}

func TestCustomersAndDashboard(t *testing.T) {
	_, owner := seeded(t)
	ctx := context.Background()

	list, err := owner.customer.ListCustomers(ctx, req(&apiv1.ListCustomersRequest{Period: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS}))
	if err != nil || len(list.Msg.GetCustomers()) != 6 {
		t.Fatalf("customers: %v %v", list, err)
	}
	cs := list.Msg.GetCustomers()
	for i, c := range cs {
		if c.GetPeriodEvents() == 0 || c.GetUnresolvedIssues() == 0 || c.GetHealth() != apiv1.Health_HEALTH_CRITICAL || len(c.GetSeries().GetCounts()) != 28 {
			t.Fatalf("customer summary: %v", c)
		}
		if i > 0 && c.GetPeriodEvents() > cs[i-1].GetPeriodEvents() {
			t.Fatal("customers not sorted by events")
		}
	}
	byName, err := owner.customer.ListCustomers(ctx, req(&apiv1.ListCustomersRequest{Sort: apiv1.CustomerSort_CUSTOMER_SORT_NAME, Query: "belediye", PageSize: 2}))
	if err != nil || byName.Msg.GetCustomers()[0].GetCustomer().GetKey() != "amasya-belediyesi" || byName.Msg.GetNextPageToken() != "2" {
		t.Fatalf("customers by name: %v %v", byName, err)
	}

	one, err := owner.customer.GetCustomer(ctx, req(&apiv1.GetCustomerRequest{Key: "Sivas-Belediyesi"}))
	if err != nil {
		t.Fatal(err)
	}
	if one.Msg.GetSummary().GetCustomer().GetName() != "Sivas Belediyesi" || len(one.Msg.GetProjects()) != 4 || len(one.Msg.GetTopIssues()) == 0 {
		t.Fatalf("customer detail: %v", one.Msg)
	}
	var total uint64
	for i, p := range one.Msg.GetProjects() {
		total += p.GetPeriodEvents()
		if i > 0 && p.GetPeriodEvents() > one.Msg.GetProjects()[i-1].GetPeriodEvents() {
			t.Fatal("project breakdown not sorted")
		}
	}
	if total != one.Msg.GetSummary().GetPeriodEvents() {
		t.Fatalf("breakdown total %d != customer total %d", total, one.Msg.GetSummary().GetPeriodEvents())
	}
	upd, err := owner.customer.UpdateCustomer(ctx, req(&apiv1.UpdateCustomerRequest{Key: "sivas-belediyesi", Name: ptr(" Sivas Bld. "), Notes: ptr("Destek: 0346")}))
	if err != nil || upd.Msg.GetCustomer().GetName() != "Sivas Bld." || upd.Msg.GetCustomer().GetNotes() != "Destek: 0346" {
		t.Fatalf("update customer: %v %v", upd, err)
	}
	if _, err := owner.customer.GetCustomer(ctx, req(&apiv1.GetCustomerRequest{Key: "yok"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown customer: %v", err)
	}

	ov, err := owner.dashboard.GetOverview(ctx, req(&apiv1.GetOverviewRequest{Period: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS}))
	if err != nil {
		t.Fatal(err)
	}
	tot := ov.Msg.GetTotals()
	if tot.GetEvents() != 800 || tot.GetUnresolvedIssues() < 5 || tot.GetNewIssues() == 0 || tot.GetAffectedCustomers() != 6 || tot.GetAffectedUsers() == 0 {
		t.Fatalf("totals: %v", tot)
	}
	var levels uint64
	for _, ls := range ov.Msg.GetEventsByLevel() {
		for _, c := range ls.GetSeries().GetCounts() {
			levels += c
		}
	}
	if levels != 800 || ov.Msg.GetEventsByLevel()[0].GetLevel() != ingestv1.Level_LEVEL_FATAL {
		t.Fatalf("level series: %d events, first level %v", levels, ov.Msg.GetEventsByLevel()[0].GetLevel())
	}
	if len(ov.Msg.GetProjects()) != 4 || len(ov.Msg.GetNewIssues()) == 0 || len(ov.Msg.GetCustomers()) != 6 {
		t.Fatalf("overview lists: %d projects, %d new, %d customers", len(ov.Msg.GetProjects()), len(ov.Msg.GetNewIssues()), len(ov.Msg.GetCustomers()))
	}

	filtered, err := owner.dashboard.GetOverview(ctx, req(&apiv1.GetOverviewRequest{Period: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS,
		Projects: []string{"kalem"}, Environments: []string{"staging"}}))
	if err != nil {
		t.Fatal(err)
	}
	ft := filtered.Msg.GetTotals()
	if ft.GetEvents() == 0 || ft.GetEvents() >= tot.GetEvents() || filtered.Msg.GetProjects()[0].GetProject().GetSlug() != "kalem" || len(filtered.Msg.GetProjects()) != 1 {
		t.Fatalf("filtered overview: %v", filtered.Msg.GetTotals())
	}
	hour, err := owner.dashboard.GetOverview(ctx, req(&apiv1.GetOverviewRequest{Period: apiv1.StatsPeriod_STATS_PERIOD_LAST_HOUR}))
	if err != nil || len(hour.Msg.GetProjects()[0].GetSeries().GetCounts()) != 60 {
		t.Fatalf("last hour overview: %v", err)
	}
}

func TestWatchLive(t *testing.T) {
	_, owner := seeded(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := owner.dashboard.WatchLive(ctx, req(&apiv1.WatchLiveRequest{Projects: []string{"kalem"}}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if !stream.Receive() || stream.Msg().GetKind() != apiv1.LiveEventKind_LIVE_EVENT_KIND_HEARTBEAT {
		t.Fatalf("first message must be a heartbeat: %v %v", stream.Msg(), stream.Err())
	}
	// Events of other projects are filtered out.
	if _, err := owner.project.SendTestEvent(ctx, req(&apiv1.SendTestEventRequest{Project: "platform"})); err != nil {
		t.Fatal(err)
	}
	sent, err := owner.project.SendTestEvent(ctx, req(&apiv1.SendTestEventRequest{Project: "kalem"}))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() {
		t.Fatalf("no live message: %v", stream.Err())
	}
	m := stream.Msg()
	if m.GetKind() != apiv1.LiveEventKind_LIVE_EVENT_KIND_ISSUE_CREATED || m.GetIssue().GetShortId() != sent.Msg.GetIssueShortId() ||
		m.GetEvent().GetId() != sent.Msg.GetEventId() || m.GetIssue().GetProject().GetSlug() != "kalem" {
		t.Fatalf("live message: %v", m)
	}
	// Triage is broadcast too.
	resolved := apiv1.IssueStatus_ISSUE_STATUS_RESOLVED
	if _, err := owner.issue.UpdateIssues(ctx, req(&apiv1.UpdateIssuesRequest{IssueIds: []string{sent.Msg.GetIssueId()}, Status: &resolved})); err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() || stream.Msg().GetKind() != apiv1.LiveEventKind_LIVE_EVENT_KIND_ISSUE_UPDATED ||
		stream.Msg().GetIssue().GetStatus() != resolved {
		t.Fatalf("update message: %v %v", stream.Msg(), stream.Err())
	}
}

func TestRolesTenancyAndAdmin(t *testing.T) {
	e, owner := seeded(t)
	ctx := context.Background()
	target := listIssues(t, owner, "")[0]

	inv, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "viewer@example.com", Role: apiv1.Role_ROLE_VIEWER}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	viewer := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := viewer.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: url[strings.LastIndex(url, "/")+1:],
		Name: "Viewer", Password: "izleyicinin parolası"})); err != nil {
		t.Fatal(err)
	}
	if got := listIssues(t, viewer, ""); len(got) == 0 {
		t.Fatal("viewer must read issues")
	}
	resolved := apiv1.IssueStatus_ISSUE_STATUS_RESOLVED
	if _, err := viewer.issue.UpdateIssues(ctx, req(&apiv1.UpdateIssuesRequest{IssueIds: []string{target.GetId()}, Status: &resolved})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("viewer triage: %v", err)
	}
	if _, err := viewer.project.CreateApiKey(ctx, req(&apiv1.CreateApiKeyRequest{Project: "kalem", Name: "x"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("viewer key creation: %v", err)
	}
	if _, err := viewer.admin.GetInstanceInfo(ctx, req(&apiv1.GetInstanceInfoRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("viewer admin access: %v", err)
	}

	// Another organization of the same owner cannot see kentos data.
	if _, err := owner.org.CreateOrganization(ctx, req(&apiv1.CreateOrganizationRequest{Name: "Başka", Slug: "baska"})); err != nil {
		t.Fatal(err)
	}
	other := e.client(map[string]string{"Izci-Org": "baska"})
	if _, err := other.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "owner@example.com", Password: "uzun ve güçlü parola"})); err != nil {
		t.Fatal(err)
	}
	if _, err := other.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: target.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("cross-tenant issue: %v", err)
	}
	if _, err := other.event.GetEvent(ctx, req(&apiv1.GetEventRequest{Id: target.GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("cross-tenant event: %v", err)
	}
	if got := listIssues(t, other, ""); len(got) != 0 {
		t.Fatalf("other organization lists %d issues", len(got))
	}
	cust, err := other.customer.ListCustomers(ctx, req(&apiv1.ListCustomersRequest{}))
	if err != nil || len(cust.Msg.GetCustomers()) != 0 {
		t.Fatalf("cross-tenant customers: %v %v", cust, err)
	}

	info, err := owner.admin.GetInstanceInfo(ctx, req(&apiv1.GetInstanceInfoRequest{}))
	if err != nil || info.Msg.GetOrganizations() != 2 || info.Msg.GetUsers() != 2 || info.Msg.GetEvents_24H() == 0 || !strings.HasPrefix(info.Msg.GetPostgresVersion(), "18") {
		t.Fatalf("instance info: %v %v", info, err)
	}
	orgs, err := owner.admin.ListOrganizations(ctx, req(&apiv1.ListOrganizationsRequest{Query: "kent"}))
	if err != nil || len(orgs.Msg.GetOrganizations()) != 1 || orgs.Msg.GetOrganizations()[0].GetOrganization().GetProjectCount() != 4 ||
		orgs.Msg.GetOrganizations()[0].GetLastEventAt() == nil {
		t.Fatalf("admin organizations: %v %v", orgs, err)
	}
	kentosID := orgs.Msg.GetOrganizations()[0].GetOrganization().GetId()
	plan, err := owner.admin.UpdateOrganizationPlan(ctx, req(&apiv1.UpdateOrganizationPlanRequest{OrganizationId: kentosID, Plan: "team",
		Limits: &apiv1.OrganizationLimits{MaxProjects: 4, MonthlyEvents: 1_000_000}}))
	if err != nil || plan.Msg.GetOrganization().GetPlan() != "team" {
		t.Fatalf("update plan: %v %v", plan, err)
	}
	if _, err := owner.project.CreateProject(ctx, req(&apiv1.CreateProjectRequest{Name: "Beşinci"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_LIMIT_REACHED {
		t.Fatalf("project limit: %v", err)
	}

	users, err := owner.admin.ListUsers(ctx, req(&apiv1.ListUsersRequest{}))
	if err != nil || len(users.Msg.GetUsers()) != 2 {
		t.Fatalf("admin users: %v %v", users, err)
	}
	var ownerID, viewerID string
	for _, u := range users.Msg.GetUsers() {
		if u.GetUser().GetSuperadmin() {
			ownerID = u.GetUser().GetId()
		} else {
			viewerID = u.GetUser().GetId()
		}
	}
	if _, err := owner.admin.UpdateUser(ctx, req(&apiv1.UpdateUserRequest{UserId: ownerID, Superadmin: ptr(false)})); reason(err) != apiv1.ErrorReason_ERROR_REASON_CONFLICT {
		t.Fatalf("self demotion: %v", err)
	}
	dis, err := owner.admin.UpdateUser(ctx, req(&apiv1.UpdateUserRequest{UserId: viewerID, Disabled: ptr(true)}))
	if err != nil || !dis.Msg.GetUser().GetDisabled() {
		t.Fatalf("disable user: %v %v", dis, err)
	}
	if _, err := viewer.issue.ListIssues(ctx, req(&apiv1.ListIssuesRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("disabled user session: %v", err)
	}
}
