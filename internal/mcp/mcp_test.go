package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/api"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/demo"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/testutil"
)

type fixture struct {
	mux      *http.ServeMux
	auth     *auth.Service
	full     string // token with read, write and ai scopes
	readOnly string
	log      *slog.Logger
	endpoint string
	// endpointAPI serves izci.api.v1.
	endpointAPI string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	database := testutil.NewDB(t)
	cfg := &config.Config{
		Env: config.EnvTest, PublicURL: "http://izci.test",
		Auth:      config.AuthConfig{Signup: "invite", SessionTTL: time.Hour, PasswordLogin: true, InvitationTTL: time.Hour},
		Retention: config.RetentionConfig{Days: 30, PremakeDays: 2},
	}
	box, _ := secret.NewBox(make([]byte, 32))
	log := slog.New(slog.DiscardHandler)
	f := &fixture{mux: http.NewServeMux(), auth: auth.NewService(database, cfg, box, log), log: log}
	if _, err := api.Mount(f.mux.Handle, api.Deps{DB: database, Auth: f.auth, Config: cfg, Log: log}); err != nil {
		t.Fatal(err)
	}
	web := httptest.NewServer(f.mux)
	t.Cleanup(web.Close)
	f.endpointAPI = web.URL

	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar}
	authc := apiv1connect.NewAuthServiceClient(hc, web.URL)
	if _, err := authc.CompleteSetup(ctx, connect.NewRequest(&apiv1.CompleteSetupRequest{Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem",
		ProjectPlatform: "dotnet"})); err != nil {
		t.Fatal(err)
	}
	users := apiv1connect.NewUserServiceClient(hc, web.URL)
	token := func(scopes ...apiv1.TokenScope) string {
		res, err := users.CreateAccessToken(ctx, connect.NewRequest(&apiv1.CreateAccessTokenRequest{Name: "mcp", Scopes: scopes}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg.GetSecret()
	}
	f.full = token(apiv1.TokenScope_TOKEN_SCOPE_READ, apiv1.TokenScope_TOKEN_SCOPE_WRITE, apiv1.TokenScope_TOKEN_SCOPE_AI)
	f.readOnly = token(apiv1.TokenScope_TOKEN_SCOPE_READ)
	if _, err := demo.Seed(ctx, database, log, demo.Options{OrgSlug: "kentos", Events: 400, Days: 3, Seed: 7}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := New(Options{API: NewAPI(InProcess(f.mux), "http://izci.internal"), Version: "test"})
	mcpHTTP := httptest.NewServer(srv.Handler(f.auth.VerifyAccessToken, log))
	t.Cleanup(mcpHTTP.Close)
	f.endpoint = mcpHTTP.URL
	return f
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func (f *fixture) session(t *testing.T, token string) *mcpsdk.ClientSession {
	t.Helper()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "izci-test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: f.endpoint,
		HTTPClient: &http.Client{Transport: bearerTransport{token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call runs a tool and decodes its structured result into out; it returns
// the text of tool errors.
func call(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			text += tc.Text
		}
	}
	if res.IsError {
		return text
	}
	if out != nil {
		raw, _ := json.Marshal(res.StructuredContent)
		if res.StructuredContent == nil {
			raw = []byte(text)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: decode %s: %v", name, raw, err)
		}
	}
	return ""
}

func TestHTTPRequiresToken(t *testing.T) {
	f := setup(t)
	for _, auth := range []string{"", "Bearer izp_not-a-real-token", "Basic b3duZXI6cGFzcw=="} {
		req, _ := http.NewRequest(http.MethodPost, f.endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized || !strings.Contains(res.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Fatalf("auth %q: status %d", auth, res.StatusCode)
		}
	}
}

func TestToolsOverHTTP(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cs := f.session(t, f.full)

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*mcpsdk.Tool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = tool
	}
	for _, want := range []string{"list_organizations", "list_projects", "get_project_setup", "search_issues", "get_issue", "list_issue_events",
		"get_event", "get_issue_stats", "list_customers", "get_customer_overview", "list_releases", "get_ai_run", "update_issue",
		"add_comment", "start_ai_analysis", "create_fix_pull_request", "list_monitors"} {
		if names[want] == nil {
			t.Errorf("tool %s is missing", want)
		}
	}
	if !names["search_issues"].Annotations.ReadOnlyHint || names["update_issue"].Annotations.ReadOnlyHint {
		t.Fatal("read-only hints are wrong")
	}

	var orgs listOrganizationsOut
	call(t, cs, "list_organizations", nil, &orgs)
	if len(orgs.Organizations) != 1 || orgs.Organizations[0].Slug != "kentos" || orgs.Organizations[0].Role != "owner" {
		t.Fatalf("organizations: %+v", orgs)
	}

	var found searchIssuesOut
	if msg := call(t, cs, "search_issues", map[string]any{"query": "is:unresolved level:>=error", "period": "7d", "sort": "events"}, &found); msg != "" {
		t.Fatal(msg)
	}
	if len(found.Issues) == 0 || found.Issues[0].ShortID == "" || !strings.HasPrefix(found.Issues[0].URL, "http://izci.test/o/kentos/issues/") {
		t.Fatalf("search: %+v", found)
	}
	short := found.Issues[0].ShortID

	var issue getIssueOut
	call(t, cs, "get_issue", map[string]any{"issue": short}, &issue)
	ev := issue.RecommendedEvent
	if issue.Issue.ShortID != short || ev == nil || len(ev.Exceptions) == 0 || len(ev.Exceptions[0].Frames) == 0 || len(issue.Tags) == 0 {
		t.Fatalf("issue: %+v", issue)
	}

	var events listIssueEventsOut
	call(t, cs, "list_issue_events", map[string]any{"issue": short, "limit": 3}, &events)
	if len(events.Events) == 0 || len(events.Events) > 3 {
		t.Fatalf("events: %+v", events)
	}
	var event Event
	call(t, cs, "get_event", map[string]any{"event": events.Events[0].ID}, &event)
	if event.ID != events.Events[0].ID || event.Issue != short {
		t.Fatalf("event: %+v", event)
	}
	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_event", Arguments: map[string]any{"event": event.ID, "raw": true}})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].(*mcpsdk.TextContent).Text, `"exceptions"`) {
		t.Fatalf("raw event: %v %v", res, err)
	}

	var stats issueStatsOut
	call(t, cs, "get_issue_stats", map[string]any{"issue": short, "period": "7d"}, &stats)
	if stats.Total == 0 || len(stats.Buckets) == 0 {
		t.Fatalf("stats: %+v", stats)
	}
	var customers listCustomersOut
	call(t, cs, "list_customers", map[string]any{"period": "7d", "limit": 5}, &customers)
	if len(customers.Customers) == 0 || customers.Customers[0].Health == "" {
		t.Fatalf("customers: %+v", customers)
	}
	var overview customerOverviewOut
	call(t, cs, "get_customer_overview", map[string]any{"customer": customers.Customers[0].Key, "period": "7d"}, &overview)
	if overview.Customer.Key != customers.Customers[0].Key || len(overview.TopIssues) == 0 {
		t.Fatalf("overview: %+v", overview)
	}
	var projects listProjectsOut
	call(t, cs, "list_projects", nil, &projects)
	var releases listReleasesOut
	call(t, cs, "list_releases", map[string]any{"project": projects.Projects[0].Slug}, &releases)
	var setupOut projectSetupOut
	call(t, cs, "get_project_setup", map[string]any{"project": "kalem"}, &setupOut)
	if len(projects.Projects) == 0 || len(releases.Releases) == 0 || !strings.Contains(setupOut.SentryDSNTemplate, "/") {
		t.Fatalf("projects %+v releases %+v setup %+v", projects, releases, setupOut)
	}

	// Cron monitors: one created through the API, listed and filtered.
	monitors := apiv1connect.NewMonitorServiceClient(&http.Client{Transport: bearerTransport{f.full}}, f.endpointAPI)
	create := connect.NewRequest(&apiv1.CreateMonitorRequest{Project: "kalem", Name: "Gece aktarımı",
		Schedule: &apiv1.MonitorSchedule{Kind: &apiv1.MonitorSchedule_Interval{Interval: &apiv1.MonitorInterval{Value: 2,
			Unit: apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_HOUR}}}, Timezone: "Europe/Istanbul"})
	create.Header().Set("Izci-Org", "kentos")
	if _, err := monitors.CreateMonitor(ctx, create); err != nil {
		t.Fatal(err)
	}
	var mons listMonitorsOut
	call(t, cs, "list_monitors", nil, &mons)
	if len(mons.Monitors) != 1 || mons.Monitors[0].Slug != "gece-aktarimi" || mons.Monitors[0].Schedule != "every 2 hours" ||
		mons.Monitors[0].Project != "kalem" {
		t.Fatalf("monitors: %+v", mons)
	}
	call(t, cs, "list_monitors", map[string]any{"failing": true}, &mons)
	if len(mons.Monitors) != 0 {
		t.Fatalf("failing monitors: %+v", mons)
	}

	// Triage through MCP, then read it back.
	var updated updateIssueOut
	if msg := call(t, cs, "update_issue", map[string]any{"issues": []string{short}, "status": "resolved", "assignee": "me",
		"priority": "high"}, &updated); msg != "" {
		t.Fatal(msg)
	}
	if len(updated.Issues) != 1 || updated.Issues[0].Status != "resolved" || updated.Issues[0].Assignee != "owner@example.com" ||
		updated.Issues[0].Priority != "high" {
		t.Fatalf("updated: %+v", updated)
	}
	var comment addCommentOut
	call(t, cs, "add_comment", map[string]any{"issue": short, "text": "Kök neden: çift gönderim."}, &comment)
	if comment.ID == "" {
		t.Fatal("comment id missing")
	}

	// Tool errors carry the API's explanation.
	if msg := call(t, cs, "start_ai_analysis", map[string]any{"issue": short}, nil); !strings.Contains(msg, "ai_disabled") {
		t.Fatalf("AI without AI: %q", msg)
	}
	if msg := call(t, cs, "get_issue", map[string]any{"issue": "KALEM-ZZZZ"}, nil); !strings.Contains(msg, "not found") {
		t.Fatalf("unknown issue: %q", msg)
	}
	if msg := call(t, cs, "search_issues", map[string]any{"period": "yesterday"}, nil); !strings.Contains(msg, "period must be") {
		t.Fatalf("bad period: %q", msg)
	}

	// Resources and prompts.
	read, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "izci://issues/" + short})
	if err != nil || len(read.Contents) != 1 || !strings.Contains(read.Contents[0].Text, short) {
		t.Fatalf("issue resource: %v %v", read, err)
	}
	prompt, err := cs.GetPrompt(ctx, &mcpsdk.GetPromptParams{Name: "triage_issue", Arguments: map[string]string{"issue": short}})
	if err != nil || len(prompt.Messages) != 1 || !strings.Contains(prompt.Messages[0].Content.(*mcpsdk.TextContent).Text, short) {
		t.Fatalf("prompt: %v %v", prompt, err)
	}
}

func TestTokenScopesApply(t *testing.T) {
	f := setup(t)
	cs := f.session(t, f.readOnly)
	var found searchIssuesOut
	if msg := call(t, cs, "search_issues", nil, &found); msg != "" || len(found.Issues) == 0 {
		t.Fatalf("read-only search: %q %+v", msg, found)
	}
	msg := call(t, cs, "update_issue", map[string]any{"issues": []string{found.Issues[0].ShortID}, "status": "ignored"}, nil)
	if !strings.Contains(msg, "scope") {
		t.Fatalf("a read-only token changed an issue: %q", msg)
	}
}

func TestStdioServer(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	srv := New(Options{API: NewAPI(InProcess(f.mux), "http://izci.internal"), Version: "test",
		Credentials: Credentials{Token: f.full, Org: "kentos"}})
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.sdk.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "stdio-test", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	var found searchIssuesOut
	if msg := call(t, cs, "search_issues", map[string]any{"query": "is:unresolved"}, &found); msg != "" || len(found.Issues) == 0 {
		t.Fatalf("stdio search: %q %+v", msg, found)
	}
	if msg := call(t, cs, "list_projects", map[string]any{"organization": "yok"}, nil); !strings.Contains(msg, "organization") {
		t.Fatalf("unknown organization: %q", msg)
	}
}
