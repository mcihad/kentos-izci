package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ai"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/testutil"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// fakeOpencode imitates `opencode run --format json`: it reports what it
// can see of its environment, then writes an analysis and fixes calc.go.
// Keywords in the prompt select other behaviors.
const fakeOpencode = `#!/bin/sh
case "$1" in
  --version) echo "1.18.31"; exit 0 ;;
  models) echo "fake/model"; exit 0 ;;
esac
dir=""; prompt=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dir) dir="$2"; shift 2 ;;
    --title|--model|--format) shift 2 ;;
    run|--pure|--auto) shift ;;
    *) prompt="$1"; shift ;;
  esac
done
cd "$dir" || exit 3
ev() { printf '%s\n' "$1"; }
ev '{"type":"step_start","timestamp":1,"sessionID":"s","part":{"type":"step-start"}}'
ev "{\"type\":\"tool_use\",\"timestamp\":2,\"sessionID\":\"s\",\"part\":{\"tool\":\"read\",\"state\":{\"status\":\"completed\",\"input\":{\"filePath\":\"$dir/.izci/context.md\"}}}}"
leak=no; [ -n "$DATABASE_URL$IZCI_SECRET_KEY$TELEGRAM_BOT_TOKEN$GITHUB_APP_PRIVATE_KEY" ] && leak=yes
case "$OPENCODE_CONFIG_CONTENT" in *'"bash":"deny"'*) perm=locked ;; *) perm=open ;; esac
ctx=missing; grep -q 'repository file .calc.go.' .izci/context.md 2>/dev/null && ctx=mapped
pii=hidden; grep -q 'ayse@example.com' .izci/event.json 2>/dev/null && pii=shared
ev "{\"type\":\"text\",\"timestamp\":3,\"sessionID\":\"s\",\"part\":{\"text\":\"leak=$leak perm=$perm project=$OPENCODE_DISABLE_PROJECT_CONFIG context=$ctx pii=$pii\"}}"
case "$prompt" in *SLEEP*) sleep 30 ;; esac
case "$prompt" in *FAILNOW*)
  ev '{"type":"error","timestamp":4,"sessionID":"s","error":{"name":"ProviderAuthError","data":{"message":"no credentials for provider fake"}}}'
  exit 1 ;;
esac
if [ -f calc.go ] && [ -n "${prompt##*Do not change any file*}" ]; then
  printf 'package calc\n\nimport "errors"\n\n// Divide returns a / b.\nfunc Divide(a, b int) (int, error) {\n\tif b == 0 {\n\t\treturn 0, errors.New("division by zero")\n\t}\n\treturn a / b, nil\n}\n' > calc.go
fi
case "$prompt" in *WORKFLOW*)
  mkdir -p .github/workflows
  printf 'name: ci\non: push\njobs:\n  leak:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo "$SECRET"\n' > .github/workflows/ci.yml ;;
esac
case "$prompt" in *TAMPER*)
  git init -q --bare .fakegit
  printf '[core]\n\trepositoryformatversion = 0\n\tbare = false\n\tfsmonitor = "touch ../tamper-marker; false"\n' > .fakegit/config
  printf 'gitdir: .fakegit\n' > .git ;;
esac
mkdir -p .izci
printf '## Özet\n\nBölme işleminde sıfır kontrolü yok.\n\n## Kök neden\n\ncalc.go:7 b sıfır olduğunda bölüyor.\n' > .izci/analysis.md
printf '{"summary":"Sıfıra bölme","root_cause":"calc.go:7","fix_summary":"Sıfır kontrolü eklendi","confidence":0.8,"risk":"low","test_suggestions":["Divide(1, 0) hata döner"]}' > .izci/result.json
ev '{"type":"step_finish","timestamp":5,"sessionID":"s","part":{"type":"step-finish","cost":0.0125,"tokens":{"input":1000,"output":200,"reasoning":50,"cache":{"read":300,"write":0}}}}'
`

type fixture struct {
	db        *db.DB
	org       uuid.UUID
	kalem     *store.Project
	bildirim  *store.Project
	remote    string
	release   string
	head      string
	runner    *ai.Runner
	pipeline  *ingest.Pipeline
	dataDir   string
	mu        sync.Mutex
	prRequest map[string]any
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Dev",
		"GIT_AUTHOR_EMAIL=dev@kentos.test", "GIT_COMMITTER_NAME=Dev", "GIT_COMMITTER_EMAIL=dev@kentos.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setup(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	f := &fixture{db: testutil.NewDB(t), dataDir: t.TempDir()}

	// A repository whose first commit (release 1.0.0) has the bug.
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	gitRun(t, src, "init", "-q", "-b", "main")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write("calc.go", "package calc\n\n// Divide returns a / b.\nfunc Divide(a, b int) int {\n\treturn a / b\n}\n")
	write("go.mod", "module example.com/calc\n\ngo 1.26\n")
	gitRun(t, src, "add", ".")
	gitRun(t, src, "commit", "-q", "-m", "initial")
	f.release = gitRun(t, src, "rev-parse", "HEAD")
	write("README.md", "# calc\n")
	gitRun(t, src, "add", ".")
	gitRun(t, src, "commit", "-q", "-m", "docs")
	f.head = gitRun(t, src, "rev-parse", "HEAD")
	f.remote = filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, filepath.Dir(f.remote), "clone", "-q", "--bare", src, f.remote)

	key := make([]byte, 32)
	box, err := secret.NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/kentos/kalem/pulls" || r.Header.Get("Authorization") != "token ghp_test" {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.prRequest = body
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 12, "html_url": "https://github.com/kentos/kalem/pull/12",
			"state": "open", "draft": body["draft"], "title": body["title"]})
	}))
	t.Cleanup(gh.Close)
	access := github.NewAccess(gh.URL, "https://github.com", nil, box, nil)

	if _, err := f.db.EnsureEventPartitions(ctx, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	err = f.db.InSystem(ctx, func(tx pgx.Tx) error {
		org, err := store.CreateOrganization(ctx, tx, "kentos", "KentOS")
		if err != nil {
			return err
		}
		f.org = org.ID
		// The organization allows sending its data to the provider.
		if _, err := tx.Exec(ctx, `UPDATE organizations SET settings = settings || '{"ai_enabled": true}' WHERE id = $1`, org.ID); err != nil {
			return err
		}
		if f.kalem, err = store.CreateProject(ctx, tx, org.ID, "kalem", "Kalem", "go"); err != nil {
			return err
		}
		if f.bildirim, err = store.CreateProject(ctx, tx, org.ID, "bildirim", "Bildirim", "go"); err != nil {
			return err
		}
		id := uuid.New()
		if _, err := store.CreateIntegration(ctx, tx, &store.Integration{ID: id, OrgID: org.ID, Auth: store.IntegrationToken,
			Name: "kentos", AccountLogin: "kentos", Secret: access.SealToken(id, "ghp_test")}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_repositories (project_id, org_id, integration_id, provider, owner, name, url, default_branch, path_mappings)
			VALUES ($1, $2, $3, 'github', 'kentos', 'kalem', $4, 'main', '[{"from_prefix":"/app/","to_prefix":""}]')`,
			f.kalem.ID, org.ID, id, "file://"+f.remote)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(bin, []byte(fakeOpencode), 0o750); err != nil {
		t.Fatal(err)
	}
	// Secrets of izci in the server environment must not reach the agent.
	t.Setenv("DATABASE_URL", "postgres://secret")
	t.Setenv("IZCI_SECRET_KEY", "secret")
	f.runner = ai.New(f.db, slog.New(slog.DiscardHandler), ai.Options{
		Config: config.AIConfig{Enabled: true, OpencodeBin: bin, MaxConcurrent: 2, Timeout: time.Minute, OutputLanguage: "Turkish",
			GitAuthorName: "izci-bot", GitAuthorEmail: "bot@izci.test"},
		DataDir: f.dataDir, PublicURL: "https://izci.test", GitHub: access, Worker: "test", AllowInsecureRemotes: true,
	})
	runCtx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = f.runner.Run(runCtx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})

	f.pipeline = ingest.NewPipeline(f.db, slog.New(slog.DiscardHandler), ingest.PipelineConfig{Shards: 1, BatchSize: 50}, ingest.Hooks{})
	f.pipeline.Start()
	t.Cleanup(func() { _ = f.pipeline.Close(context.Background()) })
	return f
}

// issue ingests a division error and returns its issue.
func (f *fixture) issue(t *testing.T, p *store.Project, message string) uuid.UUID {
	t.Helper()
	ev := &ingestv1.Event{Level: ingestv1.Level_LEVEL_ERROR, Environment: "production", Release: "1.0.0", Platform: "go",
		User: &ingestv1.User{Id: "7", Email: "ayse@example.com"},
		Exceptions: []*ingestv1.Exception{{Type: "runtime.Error", Value: message, Stacktrace: &ingestv1.Stacktrace{Frames: []*ingestv1.Frame{
			{Function: "main.main", Filename: "/app/main.go", Lineno: 10, InApp: proto.Bool(true)},
			{Function: "calc.Divide", Filename: "/app/calc.go", Lineno: 5, InApp: proto.Bool(true), ContextLine: "\treturn a / b"},
		}}}}}
	processed, err := ingest.Normalize(ev, ingest.NormalizeOptions{Project: ingest.ProjectContext{
		OrgID: f.org, OrgSlug: "kentos", ProjectID: p.ID, ProjectSlug: p.Slug, Settings: p.Settings}, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	chans, err := f.pipeline.Submit(processed)
	if err != nil {
		t.Fatal(err)
	}
	r := <-chans[0]
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	ctx := context.Background()
	if err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO releases (org_id, project_id, version, commit_sha) VALUES ($1, $2, '1.0.0', $3)
			ON CONFLICT (project_id, version) DO UPDATE SET commit_sha = excluded.commit_sha`, f.org, p.ID, f.release)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return r.IssueID
}

func (f *fixture) wait(t *testing.T, id uuid.UUID, done func(*store.AiRun) bool) *store.AiRun {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var run *store.AiRun
		if err := f.db.InOrg(ctx, f.org, func(tx pgx.Tx) error {
			var err error
			run, err = store.GetAiRun(ctx, tx, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if done(run) {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run stayed in status %d", run.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (f *fixture) finished(t *testing.T, id uuid.UUID) *store.AiRun {
	return f.wait(t, id, func(r *store.AiRun) bool { return !r.Active() })
}

func (f *fixture) aiState(t *testing.T, issue uuid.UUID) int16 {
	t.Helper()
	ctx := context.Background()
	var state int16
	if err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT ai_state FROM issues WHERE id = $1`, issue).Scan(&state)
	}); err != nil {
		t.Fatal(err)
	}
	return state
}

func (f *fixture) logs(t *testing.T, run uuid.UUID) string {
	t.Helper()
	ctx := context.Background()
	var lines []string
	if err := f.db.InOrg(ctx, f.org, func(tx pgx.Tx) error {
		logs, err := store.ListAiLogs(ctx, tx, run, 0)
		for _, l := range logs {
			lines = append(lines, l.Tool+" "+l.Message)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func TestFixRunBranchAndPullRequest(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	issue := f.issue(t, f.kalem, "integer divide by zero")
	var user uuid.UUID
	if err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
		u, err := store.CreateUser(ctx, tx, store.NewUserInput{Email: "dev@kentos.test", Name: "Dev"})
		if err == nil {
			user = u.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	run, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue, Trigger: store.AiTriggerManual, RequestedBy: &user})
	if err != nil {
		t.Fatal(err)
	}
	if run.Mode != store.AiModeFix || run.Status != store.AiQueued {
		t.Fatalf("queued run: mode %d status %d", run.Mode, run.Status)
	}
	if _, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue}); !errors.Is(err, store.ErrRunActive) {
		t.Fatalf("second run for the same issue: %v", err)
	}

	run = f.finished(t, run.ID)
	log := f.logs(t, run.ID)
	if run.Status != store.AiSucceeded {
		t.Fatalf("status %d error %q\n%s", run.Status, run.Error, log)
	}
	if !strings.Contains(log, "leak=no perm=locked project=1 context=mapped pii=hidden") {
		t.Fatalf("agent environment:\n%s", log)
	}
	if !strings.Contains(log, "read .izci/context.md") || !strings.Contains(log, "1 dosyada değişiklik önerildi (+7 −2)") {
		t.Fatalf("log:\n%s", log)
	}
	if run.BaseCommit != f.release || run.BaseBranch != "main" {
		t.Fatalf("analyzed %s (%s), want the release commit %s", run.BaseCommit, run.BaseBranch, f.release)
	}
	if run.Result.Summary != "Sıfıra bölme" || run.Result.Risk != "low" || !strings.Contains(run.Analysis, "## Kök neden") {
		t.Fatalf("result %+v\n%s", run.Result, run.Analysis)
	}
	if len(run.Files) != 1 || run.Files[0].Path != "calc.go" || run.Files[0].Status != "modified" ||
		!strings.Contains(run.Diff, "+\tif b == 0 {") || strings.Contains(run.Diff, ".izci") {
		t.Fatalf("files %+v\n%s", run.Files, run.Diff)
	}
	if run.Usage.InputTokens != 1300 || run.Usage.OutputTokens != 250 {
		t.Fatalf("usage %+v", run.Usage)
	}
	if got := f.aiState(t, issue); got != store.AiStateFixProposed {
		t.Fatalf("issue AI state %d", got)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "ai", "work", run.ID.String())); !os.IsNotExist(err) {
		t.Fatalf("checkout was not removed: %v", err)
	}

	// The branch holds exactly the reviewed diff on top of the release.
	info, err := f.runner.CreateBranch(ctx, f.org, run.ID, ai.BranchOptions{}, &user)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Branch, "izci/fix/kalem-1-") || info.State != "branch" {
		t.Fatalf("branch %+v", info)
	}
	tip := gitRun(t, f.remote, "rev-parse", "refs/heads/"+info.Branch)
	if parent := gitRun(t, f.remote, "rev-parse", tip+"^"); parent != f.release {
		t.Fatalf("branch parent %s, want %s", parent, f.release)
	}
	if author := gitRun(t, f.remote, "log", "-1", "--format=%an <%ae>", tip); author != "izci-bot <bot@izci.test>" {
		t.Fatalf("author %q", author)
	}
	if body := gitRun(t, f.remote, "show", tip+":calc.go"); !strings.Contains(body, "division by zero") {
		t.Fatalf("calc.go on the branch:\n%s", body)
	}
	if again, err := f.runner.CreateBranch(ctx, f.org, run.ID, ai.BranchOptions{Name: "other"}, &user); err != nil || again.Branch != info.Branch {
		t.Fatalf("second CreateBranch: %+v %v", again, err)
	}

	pr, err := f.runner.CreatePullRequest(ctx, f.org, run.ID, ai.PullRequestOptions{Draft: true}, &user)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 12 || pr.Branch != info.Branch || !pr.Draft {
		t.Fatalf("pull request %+v", pr)
	}
	f.mu.Lock()
	req := f.prRequest
	f.mu.Unlock()
	if req["head"] != info.Branch || req["base"] != "main" || req["draft"] != true ||
		!strings.Contains(req["title"].(string), "(KALEM-1)") || !strings.Contains(req["body"].(string), "https://izci.test/o/kentos/issues/KALEM-1") {
		t.Fatalf("pull request request %v", req)
	}
	if got := f.aiState(t, issue); got != store.AiStatePullRequest {
		t.Fatalf("issue AI state after the PR %d", got)
	}
	var kinds []int16
	if err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT kind FROM issue_activity WHERE issue_id = $1 AND kind >= 7 ORDER BY created_at, id`, issue)
		if err != nil {
			return err
		}
		kinds, err = pgx.CollectRows(rows, pgx.RowTo[int16])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 3 || kinds[0] != store.ActivityAiAnalysis || kinds[1] != store.ActivityBranchCreated || kinds[2] != store.ActivityPullRequest {
		t.Fatalf("activity kinds %v", kinds)
	}
}

func TestTamperedCheckoutCannotRunCode(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	issue := f.issue(t, f.kalem, "integer divide by zero")
	run, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue, Instructions: "TAMPER"})
	if err != nil {
		t.Fatal(err)
	}
	run = f.finished(t, run.ID)
	if run.Status != store.AiSucceeded {
		t.Fatalf("status %d: %s\n%s", run.Status, run.Error, f.logs(t, run.ID))
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "ai", "work", "tamper-marker")); err == nil {
		t.Fatal("git ran a command from the agent's repository config")
	}
	for _, file := range run.Files {
		if file.Path == ".git" {
			t.Fatalf("the worktree link became part of the change: %+v", run.Files)
		}
	}
	if !strings.Contains(run.Diff, "division by zero") {
		t.Fatalf("the fix is missing:\n%s", run.Diff)
	}
}

// TestOrganizationConsent checks that an organization's data goes to the
// AI provider only with its consent, and that a run records what it
// handed over, without the personal data of the event.
func TestOrganizationConsent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	issue := f.issue(t, f.bildirim, "integer divide by zero")
	allow := func(on bool) {
		t.Helper()
		if err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE organizations SET settings = jsonb_set(settings, '{ai_enabled}', to_jsonb($2::boolean)) WHERE id = $1`, f.org, on)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	allow(false)
	if _, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue}); !errors.Is(err, ai.ErrNotAllowed) {
		t.Fatalf("start without consent: %v", err)
	}
	allow(true)
	run, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue})
	if err != nil {
		t.Fatal(err)
	}
	run = f.finished(t, run.ID)
	if run.Status != store.AiSucceeded {
		t.Fatalf("run: status %d error %q", run.Status, run.Error)
	}
	for _, want := range []string{"## Prompt", "## .izci/context.md", "## .izci/event.json", "integer divide by zero"} {
		if !strings.Contains(run.SharedContext, want) {
			t.Errorf("shared context lacks %q", want)
		}
	}
	if strings.Contains(run.SharedContext, "ayse@example.com") {
		t.Error("the shared context carries the user's e-mail address")
	}
}

func TestAnalyzeCancelAndFailure(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	// Without a repository the run only analyzes.
	issue := f.issue(t, f.bildirim, "queue is full")
	run, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue})
	if err != nil {
		t.Fatal(err)
	}
	run = f.finished(t, run.ID)
	if run.Status != store.AiSucceeded || run.Mode != store.AiModeAnalyze || run.Diff != "" || run.BaseCommit != "" {
		t.Fatalf("analysis run: status %d mode %d diff %q error %q", run.Status, run.Mode, run.Diff, run.Error)
	}
	if got := f.aiState(t, issue); got != store.AiStateAnalyzed {
		t.Fatalf("issue AI state %d", got)
	}
	if _, err := f.runner.CreateBranch(ctx, f.org, run.ID, ai.BranchOptions{}, nil); !errors.Is(err, ai.ErrNotReady) {
		t.Fatalf("branch without changes: %v", err)
	}

	// A running agent is stopped when the run is cancelled.
	run, err = f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue, Instructions: "SLEEP"})
	if err != nil {
		t.Fatal(err)
	}
	f.wait(t, run.ID, func(r *store.AiRun) bool { return r.Status == store.AiRunning })
	time.Sleep(300 * time.Millisecond)
	started := time.Now()
	if _, err := f.runner.Cancel(ctx, f.org, run.ID, "cancelled by test"); err != nil {
		t.Fatal(err)
	}
	run = f.finished(t, run.ID)
	if run.Status != store.AiCancelled || run.Error != "cancelled by test" {
		t.Fatalf("cancelled run: status %d error %q", run.Status, run.Error)
	}
	f.wait(t, run.ID, func(*store.AiRun) bool {
		_, err := os.Stat(filepath.Join(f.dataDir, "ai", "work", run.ID.String()))
		return os.IsNotExist(err)
	})
	if time.Since(started) > 10*time.Second {
		t.Fatalf("cancellation took %s", time.Since(started))
	}
	if got := f.aiState(t, issue); got != store.AiStateAnalyzed {
		t.Fatalf("issue AI state after cancelling %d, want the previous analysis", got)
	}

	// Provider errors fail the run with opencode's message.
	run, err = f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue, Instructions: "FAILNOW"})
	if err != nil {
		t.Fatal(err)
	}
	run = f.finished(t, run.ID)
	if run.Status != store.AiFailed || !strings.Contains(run.Error, "no credentials for provider fake") {
		t.Fatalf("failed run: status %d error %q", run.Status, run.Error)
	}
	if got := f.aiState(t, issue); got != store.AiStateFailed {
		t.Fatalf("issue AI state after a failure %d", got)
	}

	// The organization's monthly limit is enforced.
	if err := f.db.InSystem(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE organizations SET limits = '{"monthlyAiRuns": 3}' WHERE id = $1`, f.org)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue}); !errors.Is(err, ai.ErrBudgetExceeded) {
		t.Fatalf("over the monthly limit: %v", err)
	}
}

// TestSensitiveChangesNeedConfirmation checks that a change to files CI
// runs (a workflow here) is never published automatically and needs an
// explicit confirmation otherwise; automatic branches use their own prefix.
func TestSensitiveChangesNeedConfirmation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	err := f.db.InOrg(ctx, f.org, func(tx pgx.Tx) error {
		settings := f.kalem.Settings
		settings.Ai = &apiv1.AiSettings{AutoAction: apiv1.AutoAction_AUTO_ACTION_PULL_REQUEST}
		_, err := store.UpdateProject(ctx, tx, f.kalem.ID, f.kalem.Name, f.kalem.Platform, settings)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// Manual: refused until confirmed.
	issue := f.issue(t, f.kalem, "integer divide by zero")
	run, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue, Instructions: "WORKFLOW"})
	if err != nil {
		t.Fatal(err)
	}
	run = f.finished(t, run.ID)
	if run.Status != store.AiSucceeded || len(run.Files) != 2 {
		t.Fatalf("run %d files %+v", run.Status, run.Files)
	}
	var sensitive *ai.SensitiveChangesError
	if _, err := f.runner.CreateBranch(ctx, f.org, run.ID, ai.BranchOptions{}, nil); !errors.As(err, &sensitive) ||
		len(sensitive.Files) != 1 || sensitive.Files[0] != ".github/workflows/ci.yml" {
		t.Fatalf("unconfirmed sensitive change: %v", err)
	}
	if _, err := f.runner.CreatePullRequest(ctx, f.org, run.ID, ai.PullRequestOptions{Draft: true}, nil); !errors.As(err, &sensitive) {
		t.Fatalf("unconfirmed sensitive pull request: %v", err)
	}
	info, err := f.runner.CreateBranch(ctx, f.org, run.ID, ai.BranchOptions{AllowSensitive: true}, nil)
	if err != nil || !strings.HasPrefix(info.Branch, "izci/fix/") {
		t.Fatalf("confirmed: %+v %v", info, err)
	}

	// Automatic: never published, whatever the confirmation.
	auto := f.issue(t, f.kalem, "nil map write")
	run, err = f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: auto, Trigger: store.AiTriggerAutomatic, Instructions: "WORKFLOW"})
	if err != nil {
		t.Fatal(err)
	}
	f.finished(t, run.ID)
	waitLog := func(id uuid.UUID, text string) string {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			log := f.logs(t, id)
			if strings.Contains(log, text) {
				return log
			}
			if time.Now().After(deadline) {
				t.Fatalf("log lacks %q:\n%s", text, log)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitLog(run.ID, "otomatik olarak yayınlanmadı")
	if r := f.finished(t, run.ID); r.PullRequest != nil {
		t.Fatalf("automatic run published a sensitive change: %+v", r.PullRequest)
	}

	// An automatic change without sensitive files goes to an "izci-auto/"
	// branch as a draft pull request, even with AUTO_ACTION_PULL_REQUEST.
	clean := f.issue(t, f.kalem, "index out of range")
	run, err = f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: clean, Trigger: store.AiTriggerAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	f.finished(t, run.ID)
	waitLog(run.ID, "Öneri otomatik olarak yayınlandı")
	r := f.finished(t, run.ID)
	if r.PullRequest == nil || !strings.HasPrefix(r.PullRequest.Branch, ai.AutoBranchPrefix+"fix/") || !r.PullRequest.Draft {
		t.Fatalf("automatic publication: %+v", r.PullRequest)
	}
	f.mu.Lock()
	body, _ := f.prRequest["body"].(string)
	f.mu.Unlock()
	if !strings.Contains(body, "Otomatik açıldı") {
		t.Fatalf("automatic pull request body:\n%s", body)
	}
}

// TestServerDecidesCommands checks that a project cannot let the agent run
// commands the server does not allow (AI_ALLOWED_COMMANDS is empty here).
func TestServerDecidesCommands(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	err := f.db.InOrg(ctx, f.org, func(tx pgx.Tx) error {
		settings := f.kalem.Settings
		settings.Ai = &apiv1.AiSettings{AllowedCommands: []string{"go test ./...", "sh"}}
		_, err := store.UpdateProject(ctx, tx, f.kalem.ID, f.kalem.Name, f.kalem.Platform, settings)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	issue := f.issue(t, f.kalem, "integer divide by zero")
	run, err := f.runner.Start(ctx, f.org, ai.StartRequest{IssueID: issue})
	if err != nil {
		t.Fatal(err)
	}
	f.finished(t, run.ID)
	log := f.logs(t, run.ID)
	if !strings.Contains(log, "Sunucu izin vermediği için şu komutlar çalıştırılamaz: go test ./..., sh") || !strings.Contains(log, "perm=locked") {
		t.Fatalf("log:\n%s", log)
	}
}
