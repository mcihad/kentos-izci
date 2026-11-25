package api_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ai"
	"github.com/mcihad/kentos-izci/internal/api"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/github"
)

// analyzeOnly is a stand-in for opencode that writes an analysis.
const analyzeOnly = `#!/bin/sh
case "$1" in --version) echo "1.18.31"; exit 0 ;; esac
while [ $# -gt 0 ]; do [ "$1" = "--dir" ] && dir="$2"; shift; done
cd "$dir" || exit 3
printf '%s\n' '{"type":"text","part":{"text":"Bağlamı okudum."}}'
mkdir -p .izci
printf '## Özet\n\nTest olayı: gerçek bir hata değil.\n' > .izci/analysis.md
printf '{"summary":"Test olayı","confidence":0.9,"risk":"low"}' > .izci/result.json
`

// setupOrg completes the setup and returns the owner's client and a test
// issue.
func setupOrg(t *testing.T, e *env) (*client, string) {
	t.Helper()
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	sent, err := owner.project.SendTestEvent(ctx, req(&apiv1.SendTestEventRequest{Project: "kalem"}))
	if err != nil {
		t.Fatal(err)
	}
	return owner, sent.Msg.GetIssueId()
}

func TestAiRunsDisabled(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, issue := setupOrg(t, e)
	if _, err := owner.ai.StartRun(ctx, req(&apiv1.StartRunRequest{IssueId: issue})); reason(err) != apiv1.ErrorReason_ERROR_REASON_AI_DISABLED {
		t.Fatalf("start without AI: %v", err)
	}
	st, err := owner.ai.GetAiStatus(ctx, req(&apiv1.GetAiStatusRequest{}))
	if err != nil || st.Msg.GetEnabled() {
		t.Fatalf("status: %v %v", st, err)
	}
}

func TestAiRunLifecycle(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(bin, []byte(analyzeOnly), 0o750); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, func(e *env, d *api.Deps) {
		runner := ai.New(e.db, e.log, ai.Options{Config: config.AIConfig{Enabled: true, OpencodeBin: bin, MaxConcurrent: 1,
			Timeout: time.Minute, OutputLanguage: "Turkish"}, DataDir: t.TempDir(), PublicURL: e.cfg.PublicURL, Worker: "api-test"})
		runCtx, stop := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = runner.Run(runCtx)
		}()
		t.Cleanup(func() {
			stop()
			<-done
		})
		d.AI = runner
	})
	ctx := context.Background()
	owner, issue := setupOrg(t, e)

	started, err := owner.ai.StartRun(ctx, req(&apiv1.StartRunRequest{IssueId: issue, Instructions: "Kısa tut."}))
	if err != nil {
		t.Fatal(err)
	}
	run := started.Msg.GetRun()
	if run.GetIssue().GetShortId() != "KALEM-1" || run.GetMode() != apiv1.AiRunMode_AI_RUN_MODE_ANALYZE ||
		run.GetRequestedBy().GetEmail() != "owner@example.com" || run.GetTrigger() != apiv1.AiRunTrigger_AI_RUN_TRIGGER_MANUAL {
		t.Fatalf("started run: %v", run)
	}

	// WatchRun replays and follows the run; its last message is the
	// finished run.
	watchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stream, err := owner.ai.WatchRun(watchCtx, req(&apiv1.WatchRunRequest{Id: run.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	var msgs []*apiv1.WatchRunResponse
	for stream.Receive() {
		msgs = append(msgs, stream.Msg())
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range msgs {
		if l := m.GetLog(); l != nil {
			texts = append(texts, l.GetMessage())
		}
	}
	last := msgs[len(msgs)-1].GetRun()
	if msgs[0].GetRun() == nil || last == nil || last.GetStatus() != apiv1.AiRunStatus_AI_RUN_STATUS_SUCCEEDED {
		t.Fatalf("stream: first %v, last %v", msgs[0], msgs[len(msgs)-1])
	}
	if joined := strings.Join(texts, "\n"); !strings.Contains(joined, "Bağlamı okudum.") || !strings.Contains(joined, "Analiz tamamlandı") {
		t.Fatalf("streamed log:\n%s", joined)
	}
	if last.GetResult().GetRisk() != apiv1.Risk_RISK_LOW || !strings.Contains(last.GetAnalysisMarkdown(), "Test olayı") {
		t.Fatalf("finished run: %v", last)
	}

	got, err := owner.ai.GetRun(ctx, req(&apiv1.GetRunRequest{Id: run.GetId(), IncludeLog: true}))
	if err != nil || len(got.Msg.GetLog()) != len(texts) {
		t.Fatalf("GetRun: %d log entries (streamed %d), %v", len(got.Msg.GetLog()), len(texts), err)
	}
	list, err := owner.ai.ListRuns(ctx, req(&apiv1.ListRunsRequest{IssueId: "KALEM-1"}))
	if err != nil || len(list.Msg.GetRuns()) != 1 || list.Msg.GetRuns()[0].GetAnalysisMarkdown() != "" {
		t.Fatalf("ListRuns: %v %v", list, err)
	}
	if _, err := owner.ai.CreateBranch(ctx, req(&apiv1.CreateBranchRequest{RunId: run.GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_AI_RUN_NOT_READY {
		t.Fatalf("branch of an analysis: %v", err)
	}
	issueResp, err := owner.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: issue}))
	if err != nil || issueResp.Msg.GetIssue().GetSummary().GetAiState() != apiv1.AiState_AI_STATE_ANALYZED {
		t.Fatalf("issue AI state: %v %v", issueResp.Msg.GetIssue().GetSummary().GetAiState(), err)
	}
	st, err := owner.ai.GetAiStatus(ctx, req(&apiv1.GetAiStatusRequest{}))
	if err != nil || !st.Msg.GetEnabled() || st.Msg.GetOpencodeVersion() != "1.18.31" || st.Msg.GetMonthlyRuns() != 1 {
		t.Fatalf("status: %v %v", st, err)
	}
}

// fakeGitHub serves the GitHub REST API and OAuth endpoints izci uses.
type fakeGitHub struct {
	srv *httptest.Server
	mu  sync.Mutex
	// installations visible to the user behind an OAuth code.
	installations map[string][]int64
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{installations: map[string][]int64{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch {
		case r.URL.Path == "/user" && auth == "token ghp_valid_token_for_tests":
			write(map[string]any{"login": "kentos-bot", "avatar_url": "https://avatars.test/u/1"})
		case r.URL.Path == "/user/repos" && auth == "token ghp_valid_token_for_tests":
			write([]map[string]any{{"name": "kalem", "full_name": "kentos/kalem", "html_url": "https://github.com/kentos/kalem",
				"default_branch": "develop", "private": true, "owner": map[string]any{"login": "kentos"}}})
		case r.URL.Path == "/repos/kentos/kalem" && auth == "token ghp_valid_token_for_tests":
			write(map[string]any{"name": "kalem", "full_name": "kentos/kalem", "default_branch": "develop", "owner": map[string]any{"login": "kentos"}})
		case strings.HasPrefix(r.URL.Path, "/repos/") && auth == "token ghp_valid_token_for_tests":
			w.WriteHeader(http.StatusNotFound)
			write(map[string]any{"message": "Not Found"})
		case r.URL.Path == "/login/oauth/access_token":
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "Iv1.test" || r.Form.Get("client_secret") != "oauth-secret" {
				write(map[string]any{"error": "incorrect_client_credentials"})
				return
			}
			write(map[string]any{"access_token": "user-" + r.Form.Get("code")})
		case r.URL.Path == "/user/installations" && strings.HasPrefix(auth, "token user-"):
			f.mu.Lock()
			ids := f.installations[strings.TrimPrefix(auth, "token user-")]
			f.mu.Unlock()
			var list []map[string]any
			for _, id := range ids {
				list = append(list, map[string]any{"id": id, "account": map[string]any{"login": "kentos", "avatar_url": "https://avatars.test/o/2"}})
			}
			write(map[string]any{"total_count": len(list), "installations": list})
		default:
			w.WriteHeader(http.StatusUnauthorized)
			write(map[string]any{"message": "Bad credentials"})
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func testAppKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func TestGitHubIntegrations(t *testing.T) {
	gh := newFakeGitHub(t)
	app, err := github.NewApp(github.AppConfig{ID: 42, Slug: "izci-test", PrivateKey: testAppKey(t), ClientID: "Iv1.test",
		ClientSecret: "oauth-secret", APIURL: gh.srv.URL, WebURL: gh.srv.URL}, gh.srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, func(e *env, d *api.Deps) {
		e.cfg.GitHub = config.GitHubConfig{APIURL: gh.srv.URL, WebURL: "https://github.com"}
		d.GitHub = github.NewAccess(gh.srv.URL, "https://github.com", app, e.box, gh.srv.Client())
	})
	ctx := context.Background()
	owner, _ := setupOrg(t, e)

	if _, err := owner.integ.ConnectGitHubToken(ctx, req(&apiv1.ConnectGitHubTokenRequest{Name: "Kişisel",
		Token: "ghp_wrong_token_for_the_tests"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_GITHUB_TOKEN_INVALID {
		t.Fatalf("invalid token: %v", err)
	}
	connected, err := owner.integ.ConnectGitHubToken(ctx, req(&apiv1.ConnectGitHubTokenRequest{Name: "KentOS bot",
		Token: "ghp_valid_token_for_tests"}))
	if err != nil {
		t.Fatal(err)
	}
	integ := connected.Msg.GetIntegration()
	if integ.GetAccountLogin() != "kentos-bot" || integ.GetAuth() != apiv1.IntegrationAuth_INTEGRATION_AUTH_TOKEN || !integ.GetHealthy() {
		t.Fatalf("integration: %v", integ)
	}
	var stored []byte
	if err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT secret FROM integrations WHERE id = $1`, integ.GetId()).Scan(&stored)
	}); err != nil || strings.Contains(string(stored), "ghp_valid") {
		t.Fatalf("the token is not stored sealed: %v", err)
	}
	repos, err := owner.integ.ListRepositories(ctx, req(&apiv1.ListRepositoriesRequest{IntegrationId: integ.GetId(), Query: "KAL"}))
	if err != nil || len(repos.Msg.GetRepositories()) != 1 || repos.Msg.GetRepositories()[0].GetFullName() != "kentos/kalem" {
		t.Fatalf("repositories: %v %v", repos, err)
	}
	linked, err := owner.project.LinkRepository(ctx, req(&apiv1.LinkRepositoryRequest{Project: "kalem", Repository: "kentos/kalem",
		IntegrationId: integ.GetId()}))
	if err != nil || linked.Msg.GetRepository().GetDefaultBranch() != "develop" {
		t.Fatalf("link with the integration's default branch: %v %v", linked, err)
	}
	if _, err := owner.project.LinkRepository(ctx, req(&apiv1.LinkRepositoryRequest{Project: "kalem", Repository: "kentos/other",
		IntegrationId: integ.GetId()})); field(err) != "repository" {
		t.Fatalf("inaccessible repository: %v", err)
	}

	// GitHub App: the callback links the installation only for the user who
	// started it and only if that GitHub user can access the installation.
	start, err := owner.integ.StartGitHubAppInstallation(ctx, req(&apiv1.StartGitHubAppInstallationRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	installURL, _ := url.Parse(start.Msg.GetInstallUrl())
	state := installURL.Query().Get("state")
	if installURL.Path != "/apps/izci-test/installations/new" || state == "" {
		t.Fatalf("install URL %s", installURL)
	}
	callback := func(c *client, q url.Values) string {
		t.Helper()
		hc := *c.hc
		hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		res, err := hc.Get(e.srv.URL + "/integrations/github/callback?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		loc, _ := url.Parse(res.Header.Get("Location"))
		return loc.Query().Get("github")
	}
	gh.mu.Lock()
	gh.installations["alice"] = []int64{77}
	gh.mu.Unlock()
	if got := callback(owner, url.Values{"state": {state}, "installation_id": {"88"}, "code": {"alice"}}); got != "denied" {
		t.Fatalf("someone else's installation: %s", got)
	}
	if got := callback(owner, url.Values{"state": {state + "x"}, "installation_id": {"77"}, "code": {"alice"}}); got != "invalid" {
		t.Fatalf("forged state: %s", got)
	}
	stranger := e.client(nil)
	if got := callback(stranger, url.Values{"state": {state}, "installation_id": {"77"}, "code": {"alice"}}); got != "session" {
		t.Fatalf("callback without the starting session: %s", got)
	}
	if got := callback(owner, url.Values{"state": {state}, "installation_id": {"77"}, "code": {"alice"}}); got != "connected" {
		t.Fatalf("installation: %s", got)
	}
	list, err := owner.integ.ListIntegrations(ctx, req(&apiv1.ListIntegrationsRequest{}))
	if err != nil || len(list.Msg.GetIntegrations()) != 2 {
		t.Fatalf("integrations: %v %v", list, err)
	}
	var appInteg *apiv1.Integration
	for _, i := range list.Msg.GetIntegrations() {
		if i.GetAuth() == apiv1.IntegrationAuth_INTEGRATION_AUTH_APP {
			appInteg = i
		}
	}
	if appInteg == nil || appInteg.GetInstallationId() != 77 || appInteg.GetAccountLogin() != "kentos" {
		t.Fatalf("app integration: %v", appInteg)
	}

	// Another organization cannot take over the installation.
	other := e.client(map[string]string{"Izci-Org": "diger"})
	if _, err := other.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "owner@example.com", Password: "uzun ve güçlü parola"})); err != nil {
		t.Fatal(err)
	}
	if _, err := other.org.CreateOrganization(ctx, req(&apiv1.CreateOrganizationRequest{Name: "Diğer", Slug: "diger"})); err != nil {
		t.Fatal(err)
	}
	start, err = other.integ.StartGitHubAppInstallation(ctx, req(&apiv1.StartGitHubAppInstallationRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	installURL, _ = url.Parse(start.Msg.GetInstallUrl())
	if got := callback(other, url.Values{"state": {installURL.Query().Get("state")}, "installation_id": {"77"}, "code": {"alice"}}); got != "taken" {
		t.Fatalf("installation of another organization: %s", got)
	}

	if _, err := owner.integ.DeleteIntegration(ctx, req(&apiv1.DeleteIntegrationRequest{Id: integ.GetId()})); err != nil {
		t.Fatal(err)
	}
	proj, err := owner.project.GetProject(ctx, req(&apiv1.GetProjectRequest{Slug: "kalem"}))
	if err != nil || proj.Msg.GetProject().GetRepository().GetIntegrationId() != "" {
		t.Fatalf("repository after deleting its integration: %v %v", proj.Msg.GetProject().GetRepository(), err)
	}
	status, err := owner.integ.GetGitHubStatus(ctx, req(&apiv1.GetGitHubStatusRequest{}))
	if err != nil || !status.Msg.GetAppConfigured() || status.Msg.GetAppSlug() != "izci-test" {
		t.Fatalf("GitHub status: %v %v", status, err)
	}
	var cerr *connect.Error
	if _, err := owner.integ.ListRepositories(ctx, req(&apiv1.ListRepositoriesRequest{IntegrationId: integ.GetId()})); !errors.As(err, &cerr) ||
		cerr.Code() != connect.CodeNotFound {
		t.Fatalf("repositories of a deleted integration: %v", err)
	}
}
