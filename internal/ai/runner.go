// Package ai runs AI analyses of issues with opencode: it checks out the
// linked repository at the failing release, lets the agent investigate
// with restricted permissions, records the analysis and the proposed
// change, and publishes changes as branches and pull requests.
package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Errors of Start and the publishing calls.
var (
	ErrDisabled       = errors.New("AI analysis is disabled on this instance")
	ErrBudgetExceeded = errors.New("the monthly AI run limit of the organization is reached")
	ErrEventMismatch  = errors.New("the event does not belong to the issue")
	ErrNotReady       = errors.New("the run has no proposed change to publish")
	ErrNoRepository   = errors.New("the project has no repository linked through a GitHub integration")
)

var (
	errCancelled = errors.New("cancelled")
	errTimeout   = errors.New("timed out")
)

// maxLogEntries bounds the log of one run.
const maxLogEntries = 5000

// Options configure a Runner.
type Options struct {
	Config    config.AIConfig
	DataDir   string
	PublicURL string
	// GitHub gives access to repositories through integrations; nil
	// disables cloning private repositories and publishing.
	GitHub *github.Access
	// Worker names this instance in ai_runs.worker; defaults to the host
	// name, which stays the same across restarts.
	Worker string
	// AllowInsecureRemotes accepts file:// and http:// repositories. Only
	// for tests: it would let members read local repositories.
	AllowInsecureRemotes bool
}

// Runner executes queued runs and publishes their results.
type Runner struct {
	db    *db.DB
	log   *slog.Logger
	opts  Options
	ws    workspace
	agent agent

	// OnIssueChange runs after the AI state of an issue changed.
	OnIssueChange func(ctx context.Context, orgID uuid.UUID, issueIDs []uuid.UUID)

	wake    chan struct{}
	pending chan candidate
	wg      sync.WaitGroup

	mu       sync.Mutex
	active   map[uuid.UUID]context.CancelCauseFunc
	mirrors  map[uuid.UUID]*sync.Mutex
	watchers map[uuid.UUID]map[chan struct{}]struct{}
	version  struct {
		value string
		at    time.Time
	}
}

// New creates a runner.
func New(database *db.DB, log *slog.Logger, opts Options) *Runner {
	// Paths are absolute: opencode resolves --dir against its own working
	// directory, which is the checkout.
	root := filepath.Join(opts.DataDir, "ai")
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if opts.Worker == "" {
		// Stable across restarts, distinct for installations sharing a host.
		host, _ := os.Hostname()
		sum := sha256.Sum256([]byte(root))
		opts.Worker = host + ":" + hex.EncodeToString(sum[:4])
	}
	return &Runner{
		db:       database,
		log:      log.With(slog.String("component", "ai")),
		opts:     opts,
		ws:       workspace{root: root},
		agent:    agent{bin: opts.Config.OpencodeBin, home: filepath.Join(root, "opencode"), config: opts.Config.OpencodeConfig},
		wake:     make(chan struct{}, 1),
		pending:  make(chan candidate, 256),
		active:   map[uuid.UUID]context.CancelCauseFunc{},
		mirrors:  map[uuid.UUID]*sync.Mutex{},
		watchers: map[uuid.UUID]map[chan struct{}]struct{}{},
	}
}

// Enabled reports whether AI analysis is turned on.
func (r *Runner) Enabled() bool { return r.opts.Config.Enabled }

// Config returns the AI settings of the instance.
func (r *Runner) Config() config.AIConfig { return r.opts.Config }

// Wake makes the runner look for queued runs now.
func (r *Runner) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run executes queued runs until ctx ends, then waits for the runs in
// progress to stop.
func (r *Runner) Run(ctx context.Context) error {
	if !r.Enabled() {
		return nil
	}
	if err := ensureDir(r.agent.home); err != nil {
		return err
	}
	r.recover(ctx)
	r.janitor(ctx)
	poll := time.NewTicker(3 * time.Second)
	defer poll.Stop()
	clean := time.NewTicker(time.Hour)
	defer clean.Stop()
	for {
		r.claim(ctx)
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return nil
		case <-r.wake:
		case <-poll.C:
		case c := <-r.pending:
			r.consider(ctx, c)
		case <-clean.C:
			r.janitor(ctx)
		}
	}
}

// recover fails runs this worker left behind when it stopped.
func (r *Runner) recover(ctx context.Context) {
	err := r.db.InSystem(ctx, func(tx pgx.Tx) error {
		ids, err := store.FailStaleAiRuns(ctx, tx, r.opts.Worker)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := store.RefreshIssueAiState(ctx, tx, id); err != nil {
				return err
			}
		}
		if len(ids) > 0 {
			r.log.Warn("marked interrupted AI runs as failed", slog.Int("runs", len(ids)))
		}
		return nil
	})
	if err != nil {
		r.log.Error("recover AI runs", slog.Any("err", err))
	}
}

// Running returns the number of runs executing on this instance.
func (r *Runner) Running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}

// claim starts queued runs while slots are free.
func (r *Runner) claim(ctx context.Context) {
	free := r.opts.Config.MaxConcurrent - r.Running()
	if free <= 0 || ctx.Err() != nil {
		return
	}
	var runs []*store.AiRun
	err := r.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		runs, err = store.ClaimAiRuns(ctx, tx, free, r.opts.Worker)
		return err
	})
	if err != nil {
		if ctx.Err() == nil {
			r.log.Error("claim AI runs", slog.Any("err", err))
		}
		return
	}
	for _, run := range runs {
		runCtx, cancel := context.WithCancelCause(ctx)
		r.mu.Lock()
		r.active[run.ID] = cancel
		r.mu.Unlock()
		r.wg.Go(func() { r.execute(runCtx, cancel, run) })
	}
}

// StartRequest describes a run to queue.
type StartRequest struct {
	IssueID uuid.UUID
	// EventID focuses the analysis; nil picks the most informative event.
	EventID *uuid.UUID
	// Mode is store.AiModeAnalyze or store.AiModeFix; 0 means a fix when a
	// repository is linked.
	Mode         int16
	Model        string
	Instructions string
	Trigger      int16
	RequestedBy  *uuid.UUID
}

// Start queues a run. It fails with ErrDisabled, ErrBudgetExceeded,
// ErrEventMismatch, store.ErrRunActive or store.ErrNotFound.
func (r *Runner) Start(ctx context.Context, orgID uuid.UUID, req StartRequest) (*store.AiRun, error) {
	if !r.Enabled() {
		return nil, ErrDisabled
	}
	var run *store.AiRun
	err := r.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var projectID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id = $1`, req.IssueID).Scan(&projectID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return store.ErrNotFound
			}
			return err
		}
		if req.EventID != nil {
			at := ingest.ReceivedAt(*req.EventID)
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM events WHERE id = $1 AND issue_id = $2 AND received_at BETWEEN $3 AND $4)`,
				*req.EventID, req.IssueID, at.Add(-time.Second), at.Add(time.Second)).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return ErrEventMismatch
			}
		}
		org, err := store.GetOrganization(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if limit := org.Limits.GetMonthlyAiRuns(); limit > 0 {
			n, err := store.CountAiRuns(ctx, tx, orgID, nil, false, monthStart(time.Now()))
			if err != nil {
				return err
			}
			if n >= int(limit) {
				return ErrBudgetExceeded
			}
		}
		mode := req.Mode
		if mode == 0 {
			mode = store.AiModeAnalyze
			if _, err := store.GetRepository(ctx, tx, projectID); err == nil {
				mode = store.AiModeFix
			}
		}
		run, err = store.CreateAiRun(ctx, tx, &store.AiRun{OrgID: orgID, ProjectID: projectID, IssueID: req.IssueID,
			EventID: req.EventID, Trigger: req.Trigger, Mode: mode, Model: strings.TrimSpace(req.Model),
			Instructions: strings.TrimSpace(req.Instructions), RequestedBy: req.RequestedBy})
		if err != nil {
			return err
		}
		if err := store.AddAiRunUsage(ctx, tx, orgID, time.Now()); err != nil {
			return err
		}
		return store.RefreshIssueAiState(ctx, tx, req.IssueID)
	})
	if err != nil {
		return nil, err
	}
	r.Wake()
	r.issueChanged(ctx, orgID, run.IssueID)
	return run, nil
}

// Cancel stops a queued or running run. Finished runs are returned as they
// are.
func (r *Runner) Cancel(ctx context.Context, orgID, runID uuid.UUID, reason string) (*store.AiRun, error) {
	var run *store.AiRun
	err := r.db.InOrg(ctx, orgID, func(tx pgx.Tx) error {
		var err error
		run, err = store.CancelAiRun(ctx, tx, runID, reason)
		if errors.Is(err, store.ErrNotFound) {
			run, err = store.GetAiRun(ctx, tx, runID)
			return err
		}
		if err != nil {
			return err
		}
		return store.RefreshIssueAiState(ctx, tx, run.IssueID)
	})
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	if cancel := r.active[runID]; cancel != nil {
		cancel(errCancelled)
	}
	r.mu.Unlock()
	r.ping(runID)
	r.issueChanged(ctx, orgID, run.IssueID)
	return run, nil
}

// Subscribe returns a channel that receives a signal whenever the run
// logged something or changed state on this instance. Watchers should
// also poll, since the run may execute on another instance.
func (r *Runner) Subscribe(runID uuid.UUID) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	r.mu.Lock()
	if r.watchers[runID] == nil {
		r.watchers[runID] = map[chan struct{}]struct{}{}
	}
	r.watchers[runID][ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		delete(r.watchers[runID], ch)
		if len(r.watchers[runID]) == 0 {
			delete(r.watchers, runID)
		}
		r.mu.Unlock()
	}
}

func (r *Runner) ping(runID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.watchers[runID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (r *Runner) issueChanged(ctx context.Context, orgID, issueID uuid.UUID) {
	if r.OnIssueChange != nil {
		r.OnIssueChange(ctx, orgID, []uuid.UUID{issueID})
	}
}

// lockMirror serializes git operations on a project's mirror.
func (r *Runner) lockMirror(projectID uuid.UUID) func() {
	r.mu.Lock()
	m := r.mirrors[projectID]
	if m == nil {
		m = &sync.Mutex{}
		r.mirrors[projectID] = m
	}
	r.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// tryLockMirror is lockMirror without waiting.
func (r *Runner) tryLockMirror(projectID uuid.UUID) (func(), bool) {
	r.mu.Lock()
	m := r.mirrors[projectID]
	if m == nil {
		m = &sync.Mutex{}
		r.mirrors[projectID] = m
	}
	r.mu.Unlock()
	if !m.TryLock() {
		return nil, false
	}
	return m.Unlock, true
}

// execute runs one claimed run to completion.
func (r *Runner) execute(ctx context.Context, cancel context.CancelCauseFunc, run *store.AiRun) {
	started := time.Now()
	defer func() {
		r.mu.Lock()
		delete(r.active, run.ID)
		r.mu.Unlock()
		cancel(nil)
	}()
	ctx, stop := context.WithTimeoutCause(ctx, r.opts.Config.Timeout, errTimeout)
	defer stop()
	log := r.log.With(slog.String("run", run.ID.String()))
	lg := &runLog{r: r, run: run}
	done := make(chan struct{})
	watching := make(chan struct{})
	go func() {
		defer close(watching)
		lg.watch(ctx, cancel, done)
	}()

	co, err := r.work(ctx, run, lg)
	close(done)
	<-watching

	save, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelSave()
	cause := context.Cause(ctx)
	shutdown := false
	switch {
	case errors.Is(err, errCancelled) || ctx.Err() != nil && errors.Is(cause, errCancelled):
		run.Status = store.AiCancelled
		lg.add(LogSystem, "", "Analiz iptal edildi")
	case ctx.Err() != nil && errors.Is(cause, errTimeout):
		run.Status, run.Error = store.AiFailed, fmt.Sprintf("timed out after %s", r.opts.Config.Timeout)
		lg.add(LogError, "", "Süre doldu; analiz durduruldu")
	case ctx.Err() != nil:
		run.Status, shutdown = store.AiCancelled, true
		lg.add(LogSystem, "", "Sunucu kapandığı için analiz durduruldu")
	case err != nil:
		run.Status, run.Error = store.AiFailed, err.Error()
		lg.add(LogError, "", "Analiz başarısız: "+clip(err.Error(), 1000))
	default:
		run.Status = store.AiSucceeded
		lg.add(LogSystem, "", "Analiz tamamlandı")
	}
	lg.flush(save)

	err = r.db.InOrg(save, run.OrgID, func(tx pgx.Tx) error {
		switch {
		case shutdown:
			if _, err := store.CancelAiRun(save, tx, run.ID, "stopped by server shutdown"); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		case run.Status != store.AiCancelled:
			ok, err := store.FinishAiRun(save, tx, run)
			if err != nil {
				return err
			}
			if ok {
				data := map[string]string{"run_id": run.ID.String(), "status": "failed"}
				if run.Status == store.AiSucceeded {
					data["status"] = "succeeded"
				}
				if run.Result.Summary != "" {
					data["summary"] = clip(run.Result.Summary, 300)
				}
				if len(run.Files) > 0 {
					data["files"] = fmt.Sprint(len(run.Files))
				}
				if err := store.AddIssueActivity(save, tx, run.OrgID, []uuid.UUID{run.IssueID}, store.ActivityAiAnalysis, run.RequestedBy, data); err != nil {
					return err
				}
			} else {
				run.Status = store.AiCancelled
			}
		}
		return store.RefreshIssueAiState(save, tx, run.IssueID)
	})
	if err != nil {
		log.Error("save AI run", slog.Any("err", err))
	}
	if co != nil {
		unlock := r.lockMirror(run.ProjectID)
		r.ws.removeCheckout(save, co.mirror, run.ID)
		unlock()
	} else {
		r.ws.removeCheckout(save, "", run.ID)
	}
	r.ping(run.ID)
	r.issueChanged(save, run.OrgID, run.IssueID)
	outcome := "failed"
	switch run.Status {
	case store.AiSucceeded:
		outcome = "succeeded"
	case store.AiCancelled:
		outcome = "cancelled"
	}
	metrics.AIRunFinished(outcome, time.Since(started), run.Usage.InputTokens, run.Usage.OutputTokens, run.Usage.CostUSD)
	log.Info("AI run finished", slog.Int("status", int(run.Status)), slog.Int("files", len(run.Files)),
		slog.Int64("input_tokens", run.Usage.InputTokens), slog.Int64("output_tokens", run.Usage.OutputTokens))

	if run.Status == store.AiSucceeded && run.Trigger == store.AiTriggerAutomatic && run.Diff != "" && !shutdown {
		r.autoPublish(save, run)
	}
}

// work prepares the checkout, runs the agent and collects the results
// into run. It returns the checkout for cleanup.
func (r *Runner) work(ctx context.Context, run *store.AiRun, lg *runLog) (*checkout, error) {
	var (
		ic            *issueContext
		repo          *store.Repository
		integration   *store.Integration
		releaseCommit string
	)
	err := r.db.InOrg(ctx, run.OrgID, func(tx pgx.Tx) error {
		var err error
		if ic, err = loadContext(ctx, tx, run, r.opts.PublicURL); err != nil {
			return err
		}
		repo, err = store.GetRepository(ctx, tx, run.ProjectID)
		if errors.Is(err, store.ErrNotFound) {
			repo, err = nil, nil
		}
		if err != nil {
			return err
		}
		if repo != nil && repo.IntegrationID != nil {
			if integration, err = store.GetIntegration(ctx, tx, *repo.IntegrationID); err != nil {
				return fmt.Errorf("load integration: %w", err)
			}
		}
		if rel := ic.Event.GetRelease(); rel != "" {
			err := tx.QueryRow(ctx, `SELECT commit_sha FROM releases WHERE project_id = $1 AND version = $2`, run.ProjectID, rel).Scan(&releaseCommit)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	model := firstNonEmpty(run.Model, ic.Settings.GetAi().GetModel(), r.opts.Config.Model)
	allowed := cleanCommands(ic.Settings.GetAi().GetAllowedCommands())

	var (
		co       *checkout
		dir      string
		branch   string
		rewrites []pathRewrite
	)
	if repo != nil {
		_ = json.Unmarshal(repo.PathMappings, &rewrites)
		remote := remoteURL(repo.URL)
		if err := r.checkRemote(remote); err != nil {
			return nil, err
		}
		auth, err := r.gitAuth(ctx, integration)
		if err != nil {
			return nil, err
		}
		lg.add(LogSystem, "", "Depo eşitleniyor: "+repo.Owner+"/"+repo.Name)
		unlock := r.lockMirror(run.ProjectID)
		mirror, err := r.ws.sync(ctx, run.ProjectID, remote, auth)
		var commit string
		if err == nil {
			commit, branch, err = resolve(ctx, mirror, releaseCommit, repo.DefaultBranch)
		}
		if err == nil {
			co, err = r.ws.addWorktree(ctx, mirror, run.ID, commit)
		}
		unlock()
		if err != nil {
			return co, fmt.Errorf("prepare repository: %w", err)
		}
		dir = co.dir
		run.BaseCommit, run.BaseBranch = commit, branch
		where := branch + " dalı"
		if releaseCommit != "" && strings.HasPrefix(commit, releaseCommit) {
			where = ic.Event.GetRelease() + " sürümü"
		}
		lg.add(LogSystem, "", fmt.Sprintf("Kod hazır: %s (%s)", commit[:min(12, len(commit))], where))
	} else {
		if dir, err = r.ws.scratch(run.ID); err != nil {
			return nil, err
		}
		lg.add(LogSystem, "", "Depo bağlı değil; analiz yalnız hata kaydına dayanacak")
	}
	history := ""
	if co != nil {
		history = recentCommits(ctx, co.mirror, co.base, appFiles(ic, dir, rewrites))
	}
	if err := writeContext(dir, ic, co, branch, rewrites, history); err != nil {
		return co, fmt.Errorf("write context: %w", err)
	}

	var started bool
	if err := r.db.InOrg(ctx, run.OrgID, func(tx pgx.Tx) error {
		var err error
		started, err = store.MarkAiRunRunning(ctx, tx, run.ID, model, run.BaseCommit, run.BaseBranch)
		return err
	}); err != nil {
		return co, err
	}
	if !started {
		return co, errCancelled
	}
	run.Model = model
	r.ping(run.ID)
	label := model
	if label == "" {
		label = "opencode varsayılan modeli"
	}
	lg.add(LogSystem, "", "Ajan başladı · "+label)
	a := r.agent
	a.model = model
	out, agentErr := a.run(ctx, dir, prompt(ic, run, co != nil, allowed, r.opts.Config.OutputLanguage), "izci "+ic.ShortID, allowed, lg.add)
	run.Usage = out.usage
	if ctx.Err() != nil {
		return co, context.Cause(ctx)
	}
	lg.add(LogSystem, "", "Sonuçlar toplanıyor")
	analysis, result, found, err := readResults(dir)
	if err != nil {
		return co, err
	}
	if !found {
		switch {
		case agentErr != nil:
			return co, agentErr
		case len(out.errors) > 0:
			return co, errors.New(out.errors[len(out.errors)-1])
		}
		return co, errors.New("the agent finished without writing .izci/analysis.md or .izci/result.json")
	}
	if agentErr != nil {
		lg.add(LogError, "", "opencode hatayla kapandı, ancak sonuç dosyaları yazılmıştı: "+clip(agentErr.Error(), 500))
	}
	run.Analysis, run.Result = analysis, result
	if co != nil && run.Mode == store.AiModeFix {
		patch, files, err := co.changes(ctx)
		if err != nil {
			return co, err
		}
		run.Diff, run.Files = string(patch), files
		if len(files) == 0 {
			lg.add(LogSystem, "", "Kod değişikliği önerilmedi")
		} else {
			add, del := 0, 0
			for _, f := range files {
				add, del = add+f.Additions, del+f.Deletions
			}
			lg.add(LogSystem, "", fmt.Sprintf("%d dosyada değişiklik önerildi (+%d −%d)", len(files), add, del))
		}
	}
	return co, nil
}

func (r *Runner) checkRemote(remote string) error {
	u, err := url.Parse(remote)
	if err == nil && u.Scheme == "https" && u.Host != "" {
		return nil
	}
	if r.opts.AllowInsecureRemotes {
		return nil
	}
	return fmt.Errorf("repository URL %q is not an https URL", remote)
}

// gitAuth returns the token of an integration for git over HTTPS.
func (r *Runner) gitAuth(ctx context.Context, integration *store.Integration) (gitAuth, error) {
	if integration == nil {
		return gitAuth{}, nil
	}
	if r.opts.GitHub == nil {
		return gitAuth{}, github.ErrAppNotConfigured
	}
	client, err := r.opts.GitHub.Client(integration)
	if err != nil {
		return gitAuth{}, err
	}
	token, err := client.Token(ctx)
	if err != nil {
		return gitAuth{}, fmt.Errorf("GitHub token: %w", err)
	}
	return gitAuth{token: token}, nil
}

// readResults reads the files the agent wrote into .izci/. Paths are
// resolved inside the checkout only, so a symlink cannot make izci read
// files elsewhere on the server.
func readResults(dir string) (analysis string, result store.AiResult, found bool, err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", result, false, err
	}
	defer root.Close()
	read := func(name string, limit int64) ([]byte, bool) {
		f, err := root.Open(filepath.Join(".izci", name))
		if err != nil {
			return nil, false
		}
		defer f.Close()
		if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
			return nil, false
		}
		b, err := io.ReadAll(io.LimitReader(f, limit))
		return b, err == nil
	}
	md, okMD := read("analysis.md", 256<<10)
	js, okJS := read("result.json", 64<<10)
	if okJS {
		var raw struct {
			Summary         string   `json:"summary"`
			RootCause       string   `json:"root_cause"`
			FixSummary      string   `json:"fix_summary"`
			Confidence      float64  `json:"confidence"`
			Risk            string   `json:"risk"`
			TestSuggestions []string `json:"test_suggestions"`
		}
		if json.Unmarshal(js, &raw) == nil {
			result = store.AiResult{
				Summary:    clip(strings.TrimSpace(raw.Summary), 2000),
				RootCause:  clip(strings.TrimSpace(raw.RootCause), 8000),
				FixSummary: clip(strings.TrimSpace(raw.FixSummary), 8000),
				Confidence: confidence(raw.Confidence),
				Risk:       risk(raw.Risk),
			}
			for _, s := range raw.TestSuggestions {
				if s = strings.TrimSpace(s); s != "" && len(result.TestSuggestions) < 10 {
					result.TestSuggestions = append(result.TestSuggestions, clip(s, 500))
				}
			}
		} else {
			okJS = false
		}
	}
	analysis = strings.TrimSpace(string(md))
	switch {
	case !okMD && !okJS:
		return "", result, false, nil
	case analysis == "":
		analysis = synthesize(result)
	case result.Summary == "":
		result.Summary = firstParagraph(analysis)
	}
	return analysis, result, true, nil
}

func confidence(v float64) float64 {
	if v > 1 && v <= 100 {
		v /= 100
	}
	return max(0, min(1, v))
}

func risk(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low", "düşük":
		return "low"
	case "medium", "orta":
		return "medium"
	case "high", "yüksek":
		return "high"
	}
	return ""
}

// synthesize writes a report from result.json when analysis.md is missing.
func synthesize(r store.AiResult) string {
	var b strings.Builder
	section := func(title, body string) {
		if body != "" {
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", title, body)
		}
	}
	section("Özet", r.Summary)
	section("Kök neden", r.RootCause)
	section("Önerilen düzeltme", r.FixSummary)
	if len(r.TestSuggestions) > 0 {
		b.WriteString("## Doğrulama\n\n")
		for _, s := range r.TestSuggestions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	return strings.TrimSpace(b.String())
}

// firstParagraph returns the first paragraph of Markdown that is not a
// heading.
func firstParagraph(md string) string {
	for _, p := range strings.Split(md, "\n\n") {
		p = strings.TrimSpace(p)
		if p != "" && !strings.HasPrefix(p, "#") {
			return clip(p, 600)
		}
	}
	return ""
}

func cleanCommands(in []string) []string {
	var out []string
	for _, c := range in {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// runLog buffers log entries of a run and stores them every second.
type runLog struct {
	r       *Runner
	run     *store.AiRun
	mu      sync.Mutex
	seq     int
	pending []store.AiLog
}

func (l *runLog) add(kind int16, tool, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seq >= maxLogEntries {
		return
	}
	l.seq++
	if l.seq == maxLogEntries {
		kind, tool, msg = LogSystem, "", "Kayıt sınırına ulaşıldı; sonraki adımlar kaydedilmiyor"
	}
	l.pending = append(l.pending, store.AiLog{Seq: l.seq, Time: time.Now(), Kind: kind, Tool: tool, Message: msg})
}

func (l *runLog) flush(ctx context.Context) {
	l.mu.Lock()
	batch := l.pending
	l.pending = nil
	l.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	err := l.r.db.InOrg(ctx, l.run.OrgID, func(tx pgx.Tx) error {
		return store.AppendAiLogs(ctx, tx, l.run.OrgID, l.run.ID, batch)
	})
	if err != nil && ctx.Err() == nil {
		l.r.log.Warn("store AI run log", slog.Any("err", err))
	}
	l.r.ping(l.run.ID)
}

// watch flushes the log every second and stops the run when a user
// cancelled it (possibly through another instance).
func (l *runLog) watch(ctx context.Context, cancel context.CancelCauseFunc, done <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-t.C:
		}
		l.flush(ctx)
		var status int16
		_ = l.r.db.InOrg(ctx, l.run.OrgID, func(tx pgx.Tx) error {
			var err error
			status, err = store.AiRunStatus(ctx, tx, l.run.ID)
			return err
		})
		if status == store.AiCancelled {
			cancel(errCancelled)
			return
		}
	}
}

// candidate is a new issue that automation may analyze.
type candidate struct {
	orgID, projectID, issueID uuid.UUID
	level                     ingestv1.Level
}

// NewIssues offers the issues created by a batch to automation. It never
// blocks the ingest pipeline: when the queue is full, issues are skipped.
func (r *Runner) NewIssues(b *ingest.Batch) {
	if !r.Enabled() {
		return
	}
	for _, o := range b.Issues {
		if !o.Created {
			continue
		}
		level := ingestv1.Level_LEVEL_UNSPECIFIED
		for _, ev := range o.Events {
			level = max(level, ev.Level)
		}
		select {
		case r.pending <- candidate{orgID: o.OrgID, projectID: o.ProjectID, issueID: o.ID, level: level}:
		default:
		}
	}
}

var errSkip = errors.New("skip")

// consider starts an automatic analysis when the project asks for one and
// its daily budget allows it.
func (r *Runner) consider(ctx context.Context, c candidate) {
	err := r.db.InOrg(ctx, c.orgID, func(tx pgx.Tx) error {
		p, err := store.GetProject(ctx, tx, c.projectID)
		if err != nil {
			return err
		}
		s := p.Settings.GetAi()
		if s.GetAutoAnalyze() != apiv1.AutoAnalyze_AUTO_ANALYZE_NEW_ISSUES || s.GetDailyBudget() == 0 {
			return errSkip
		}
		minLevel := s.GetMinLevel()
		if minLevel == ingestv1.Level_LEVEL_UNSPECIFIED {
			minLevel = ingestv1.Level_LEVEL_ERROR
		}
		if c.level < minLevel {
			return errSkip
		}
		now := time.Now().UTC()
		n, err := store.CountAiRuns(ctx, tx, c.orgID, &c.projectID, true, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC))
		if err != nil {
			return err
		}
		if n >= int(s.GetDailyBudget()) {
			return errSkip
		}
		return nil
	})
	if errors.Is(err, errSkip) {
		return
	}
	if err == nil {
		_, err = r.Start(ctx, c.orgID, StartRequest{IssueID: c.issueID, Trigger: store.AiTriggerAutomatic})
	}
	if err != nil && !errors.Is(err, ErrBudgetExceeded) && !errors.Is(err, store.ErrRunActive) && ctx.Err() == nil {
		r.log.Warn("automatic AI analysis", slog.String("issue", c.issueID.String()), slog.Any("err", err))
	}
}

// autoPublish takes the project's automatic action after an automatic run.
func (r *Runner) autoPublish(ctx context.Context, run *store.AiRun) {
	var action apiv1.AutoAction
	_ = r.db.InOrg(ctx, run.OrgID, func(tx pgx.Tx) error {
		p, err := store.GetProject(ctx, tx, run.ProjectID)
		if err == nil {
			action = p.Settings.GetAi().GetAutoAction()
		}
		return err
	})
	var err error
	switch action {
	// Automatic changes go to "izci-auto/" branches, pull requests are
	// always drafts (AUTO_ACTION_PULL_REQUEST included) and changes to
	// files CI or build tools run are never published.
	case apiv1.AutoAction_AUTO_ACTION_BRANCH:
		_, err = r.CreateBranch(ctx, run.OrgID, run.ID, BranchOptions{automatic: true}, nil)
	case apiv1.AutoAction_AUTO_ACTION_DRAFT_PULL_REQUEST, apiv1.AutoAction_AUTO_ACTION_PULL_REQUEST:
		_, err = r.CreatePullRequest(ctx, run.OrgID, run.ID, PullRequestOptions{automatic: true}, nil)
	default:
		return
	}
	lg := &runLog{r: r, run: run, seq: r.lastSeq(ctx, run)}
	var sensitive *SensitiveChangesError
	switch {
	case errors.As(err, &sensitive):
		lg.add(LogSystem, "", "Öneri otomatik olarak yayınlanmadı: CI, derleme araçları ya da git'in çalıştırdığı dosyalara dokunuyor ("+
			clip(strings.Join(sensitive.Files, ", "), 400)+"). İnceleyip el ile yayınlayabilirsiniz.")
	case err != nil:
		lg.add(LogError, "", "Otomatik yayınlama başarısız: "+clip(err.Error(), 500))
		r.log.Warn("automatic AI publishing", slog.String("run", run.ID.String()), slog.Any("err", err))
	default:
		lg.add(LogSystem, "", "Öneri otomatik olarak yayınlandı")
	}
	lg.flush(ctx)
}

func (r *Runner) lastSeq(ctx context.Context, run *store.AiRun) int {
	var seq int
	_ = r.db.InOrg(ctx, run.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT coalesce(max(seq), 0) FROM ai_run_logs WHERE run_id = $1`, run.ID).Scan(&seq)
	})
	return seq
}

// janitor fails runs whose worker disappeared and removes leftovers:
// checkouts of finished runs and mirrors of unlinked repositories.
func (r *Runner) janitor(ctx context.Context) {
	err := r.db.InSystem(ctx, func(tx pgx.Tx) error {
		ids, err := store.FailExpiredAiRuns(ctx, tx, time.Now().Add(-r.opts.Config.Timeout-10*time.Minute))
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := store.RefreshIssueAiState(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		r.log.Warn("expire AI runs", slog.Any("err", err))
	}

	work := filepath.Join(r.ws.root, "work")
	entries, _ := os.ReadDir(work)
	for _, e := range entries {
		id, err := uuid.Parse(e.Name())
		info, ierr := e.Info()
		if err != nil || ierr != nil || time.Since(info.ModTime()) < r.opts.Config.Timeout+time.Hour {
			continue
		}
		r.mu.Lock()
		_, running := r.active[id]
		r.mu.Unlock()
		if !running {
			_ = os.RemoveAll(filepath.Join(work, e.Name()))
		}
	}

	repos := filepath.Join(r.ws.root, "repos")
	entries, _ = os.ReadDir(repos)
	var projects []uuid.UUID
	for _, e := range entries {
		if id, err := uuid.Parse(strings.TrimSuffix(e.Name(), ".git")); err == nil && e.IsDir() {
			projects = append(projects, id)
		}
	}
	if len(projects) == 0 {
		return
	}
	linked := map[uuid.UUID]bool{}
	err = r.db.InSystem(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT project_id FROM project_repositories WHERE project_id = ANY($1)`, projects)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		for _, id := range ids {
			linked[id] = true
		}
		return err
	})
	if err != nil {
		return
	}
	for _, id := range projects {
		unlock, ok := r.tryLockMirror(id)
		if !ok {
			continue
		}
		dir := r.ws.mirrorDir(id)
		if linked[id] {
			g := bare(dir, gitAuth{})
			_, _ = g.run(ctx, "worktree", "prune")
			_, _ = g.run(ctx, "-c", "gc.auto=6700", "gc", "--auto", "--quiet")
		} else {
			_ = os.RemoveAll(dir)
		}
		unlock()
	}
}
