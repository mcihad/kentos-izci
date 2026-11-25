// Package app assembles izci: database, ingest pipeline, services,
// background jobs and the HTTP server. The CLI's serve command and end-to-end
// tests both build the application through here.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"connectrpc.com/grpcreflect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mcihad/kentos-izci/docs"
	"github.com/mcihad/kentos-izci/internal/ai"
	"github.com/mcihad/kentos-izci/internal/alert"
	"github.com/mcihad/kentos-izci/internal/api"
	"github.com/mcihad/kentos-izci/internal/auditforward"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/digest"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/inbox"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/ingest/sentry"
	"github.com/mcihad/kentos-izci/internal/jobs"
	"github.com/mcihad/kentos-izci/internal/mcp"
	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/monitor"
	"github.com/mcihad/kentos-izci/internal/notify"
	"github.com/mcihad/kentos-izci/internal/ownership"
	"github.com/mcihad/kentos-izci/internal/realtime"
	"github.com/mcihad/kentos-izci/internal/secret"
	"github.com/mcihad/kentos-izci/internal/sentryapi"
	"github.com/mcihad/kentos-izci/internal/server"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/symbols"
	"github.com/mcihad/kentos-izci/internal/telegram"
	"github.com/mcihad/kentos-izci/internal/version"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// App is a fully wired izci instance.
type App struct {
	Config   *config.Config
	Log      *slog.Logger
	DB       *db.DB
	Server   *server.Server
	Pipeline *ingest.Pipeline
	Ingest   *ingest.Service

	Auth *auth.Service
	API  *api.Servers
	// Hub fans live updates out to WatchLive streams.
	Hub *realtime.Hub
	// Alerts evaluates alert rules for every stored batch.
	Alerts *alert.Engine
	// Bot is the Telegram bot; nil when TELEGRAM_BOT_TOKEN is empty.
	Bot *telegram.Bot
	// Notify delivers queued notifications to every kind of channel.
	Notify *notify.Dispatcher
	// Inbox delivers members' personal notifications by e-mail and
	// Telegram.
	Inbox *inbox.Service
	// GitHub gives access to repositories through integrations.
	GitHub *github.Access
	// AI runs AI analyses; it idles unless AI_ENABLED is set.
	AI *ai.Runner
	// Monitors processes cron check-ins and sweeps overdue runs.
	Monitors *monitor.Service
	// Symbols resolves stack traces with uploaded source maps and debug
	// files and assembles those uploads.
	Symbols *symbols.Service
	// Ownership assigns new issues to their owners and finds suspect
	// commits.
	Ownership *ownership.Service

	jobs  *jobs.Runner
	usage *ingest.UsageRecorder
	keys  *ingest.KeyResolver
}

// Options customizes New.
type Options struct {
	UI fs.FS
}

// New wires the application on an open database. Migrations and
// partitions must already be in place (see Prepare).
func New(cfg *config.Config, log *slog.Logger, database *db.DB, opts Options) (*App, error) {
	a := &App{Config: cfg, Log: log, DB: database, Hub: realtime.NewHub(), Alerts: alert.NewEngine(30 * time.Second)}

	box, err := secret.NewBox(cfg.SecretKey, cfg.SecretKeyPrevious.Bytes()...)
	if err != nil {
		return nil, err
	}
	// Notification channels: Telegram (when a bot is configured), e-mail
	// (when SMTP is), Slack, Teams, webhooks and Jira.
	a.Notify = notify.NewDispatcher(database, log, cfg.PublicURL)
	notifyHTTP := notify.NewHTTPClient(cfg.Notify.AllowPrivateNetworks)
	hooks := notify.NewWebhooks(box, notifyHTTP)
	a.Notify.Register(store.ChannelSlack, hooks.Slack())
	a.Notify.Register(store.ChannelTeams, hooks.Teams())
	a.Notify.Register(store.ChannelWebhook, hooks.Generic())
	jira := notify.NewJira(box, notifyHTTP)
	a.Notify.Register(store.ChannelJira, jira)
	var email *notify.Email
	if cfg.SMTP.Enabled() {
		if email, err = notify.NewEmail(cfg.SMTP); err != nil {
			return nil, err
		}
		a.Notify.Register(store.ChannelEmail, email)
	}
	if cfg.Telegram.Enabled() {
		a.Bot = telegram.New(cfg.Telegram, cfg.PublicURL, database, box, log, nil)
		a.Bot.OnRulesChanged = a.Alerts.Invalidate
		a.Notify.Register(store.ChannelTelegram, a.Bot)
	}
	// Personal notifications go by e-mail and Telegram when the server
	// has them (interfaces stay nil otherwise).
	var mailer inbox.Mailer
	var digestMailer digest.Mailer
	if email != nil {
		mailer, digestMailer = email, email
	}
	digests := digest.New(database, digestMailer, cfg.PublicURL, log)
	var direct inbox.Telegram
	if a.Bot != nil {
		direct = a.Bot
	}
	a.Inbox = inbox.New(database, log, mailer, direct, cfg.PublicURL)
	if a.GitHub, err = NewGitHubAccess(cfg, box); err != nil {
		return nil, err
	}
	a.AI = ai.New(database, log, ai.Options{Config: cfg.AI, DataDir: cfg.DataDir, PublicURL: cfg.PublicURL, GitHub: a.GitHub})
	a.Ownership = ownership.NewService(database, log, a.GitHub)

	a.Pipeline = ingest.NewPipeline(database, log, ingest.PipelineConfig{
		Shards:    cfg.Ingest.Shards,
		QueueSize: cfg.Ingest.QueueSize,
		BatchSize: cfg.Ingest.BatchSize,
		Linger:    cfg.Ingest.Linger,
		PublicURL: cfg.PublicURL,
	}, ingest.Hooks{
		InTx: func(ctx context.Context, tx pgx.Tx, b *ingest.Batch) error {
			// Automatic assignment of new issues comes first so that
			// notifications name the assignee; like alerting, it runs in a
			// savepoint.
			if ownership.HasNewIssues(b) {
				own, err := tx.Begin(ctx)
				if err != nil {
					return err
				}
				if err := a.Ownership.AssignNew(ctx, own, b); err != nil {
					_ = own.Rollback(ctx)
					log.Error("automatic assignment failed; batch stored without it", slog.Any("err", err))
				} else if err := own.Commit(ctx); err != nil {
					return err
				}
			}
			// Followers of issues that came back hear about it; like
			// alerting, in a savepoint.
			if personal, err := tx.Begin(ctx); err != nil {
				return err
			} else if n, err := inbox.Regressions(ctx, personal, b); err != nil {
				_ = personal.Rollback(ctx)
				log.Error("personal notifications failed; batch stored without them", slog.Any("err", err))
			} else if err := personal.Commit(ctx); err != nil {
				return err
			} else if n > 0 {
				b.PersonalNotifications += n
			}
			// Alerting runs in a savepoint: if it fails, the events are
			// still stored, only without notifications.
			sp, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			n, err := a.Alerts.Evaluate(ctx, sp, b)
			if err != nil {
				_ = sp.Rollback(ctx)
				log.Error("alert evaluation failed; batch stored without notifications", slog.Any("err", err))
				return nil
			}
			b.Notifications += n
			return sp.Commit(ctx)
		},
		AfterCommit: func(b *ingest.Batch) {
			if a.Hub.Subscribers() > 0 {
				a.Hub.Publish(realtime.FromBatch(b, cfg.PublicURL)...)
			}
			if b.Notifications > 0 {
				a.Notify.Wake()
			}
			if b.PersonalNotifications > 0 {
				a.Inbox.Wake()
			}
			a.AI.NewIssues(b)
		},
	})

	a.keys = ingest.NewKeyResolver(database, 30*time.Second)
	a.usage = ingest.NewUsageRecorder()
	a.Ingest = ingest.NewService(ingest.ServiceConfig{
		Durable:            cfg.Ingest.Ack == "durable",
		AckTimeout:         cfg.Ingest.AckTimeout,
		Retention:          time.Duration(cfg.Retention.Days) * 24 * time.Hour,
		TrustedProxies:     cfg.HTTP.TrustedProxies,
		MaxAttachmentBytes: cfg.Ingest.MaxAttachmentBytes,
		SessionKey:         cfg.SecretKey,
	}, database, log, a.keys, ingest.NewRateLimiter(cfg.Ingest.RateLimitPerMinute),
		ingest.NewQuotaTracker(database), a.usage, ingest.NewDedupe(cfg.Ingest.DedupeSize, time.Hour), a.Pipeline)
	// Cron monitoring: failing monitors become events of an issue per
	// monitor and environment, so alert rules cover them.
	a.Monitors = monitor.NewService(database, log,
		ingest.NewIncidentEmitter(database, a.Pipeline, time.Duration(cfg.Retention.Days)*24*time.Hour))
	a.Ingest.SetMonitors(a.Monitors)
	// Minified scripts and native Flutter builds: stack traces are resolved
	// with uploaded source maps and debug files before grouping.
	a.Symbols = symbols.NewService(database, log, symbols.Options{CacheBytes: cfg.Ingest.SymbolCacheMB << 20,
		MaxFileBytes: int64(cfg.Ingest.UploadMaxMB) << 20, TempDir: filepath.Join(cfg.DataDir, "tmp")})
	a.Ingest.SetSymbolicator(a.Symbols)

	a.Server = server.New(server.Options{Config: cfg, Logger: log, DB: database, UI: opts.UI})
	path, handler := ingestv1connect.NewIngestServiceHandler(a.Ingest,
		connect.WithReadMaxBytes(8<<20),
		connect.WithCompressMinBytes(1024),
	)
	a.Server.Handle(path, ingest.CORS(handler))
	sentry.NewHandler(a.Ingest, log).Register(a.Server.Handle)
	// Server reflection lets grpcurl and buf curl call izci without proto
	// files; the standard health service serves load balancer checks.
	reflector := grpcreflect.NewStaticReflector(append([]string{ingestv1connect.IngestServiceName}, api.ServiceNames...)...)
	a.Server.Handle(grpcreflect.NewHandlerV1(reflector))
	a.Server.Handle(grpcreflect.NewHandlerV1Alpha(reflector))
	a.Server.Handle(grpchealth.NewHandler(grpchealth.NewStaticChecker(ingestv1connect.IngestServiceName)))
	if cfg.Metrics.Enabled {
		m := metrics.NewInstance()
		m.Gauge("izci_ingest_queue_depth", func() float64 { return float64(a.Pipeline.QueueDepth()) })
		m.Gauge("izci_ingest_queue_capacity", func() float64 { return float64(a.Pipeline.QueueCapacity()) })
		m.Gauge("izci_live_subscribers", func() float64 { return float64(a.Hub.Subscribers()) })
		m.Gauge("izci_ai_runs_running", func() float64 { return float64(a.AI.Running()) })
		m.Gauge("izci_symbol_cache_bytes", func() float64 { return float64(a.Symbols.CacheBytes()) })
		m.Pool(database.Pool)
		a.Server.Handle("GET /metrics", m.Handler(cfg.Metrics.Token.Value()))
	}

	docs.Register(a.Server.Handle, cfg.PublicURL)
	a.Auth = auth.NewService(database, cfg, box, log)
	a.Auth.RegisterRoutes(a.Server.Handle)
	// sentry-cli and the Sentry bundler plugins upload source maps, debug
	// files and releases through a subset of Sentry's web API.
	sentryapi.New(database, a.keys, a.Auth, a.Symbols, log).Register(a.Server.Handle)
	deps := api.Deps{
		DB: database, Auth: a.Auth, Config: cfg, Log: log,
		Hub: a.Hub, Pipeline: a.Pipeline, Keys: a.keys, StreamsDone: a.Server.StreamsDone(), Alerts: a.Alerts,
		AI: a.AI, GitHub: a.GitHub, Symbols: a.Symbols, Delivery: a.Notify, Jira: jira, Email: email, Ownership: a.Ownership, Inbox: a.Inbox,
	}
	if a.Bot != nil {
		deps.Notifier = a.Bot
	}
	if email != nil {
		// Not assigned unconditionally: a nil *notify.Email would make a
		// non-nil Mailer.
		deps.Mailer = email
	}
	// The API is also mounted on a private mux that MCP calls in-process.
	apiMux := http.NewServeMux()
	mount := func(pattern string, h http.Handler) {
		a.Server.Handle(pattern, h)
		apiMux.Handle(pattern, h)
	}
	if a.API, err = api.Mount(mount, deps); err != nil {
		return nil, err
	}
	if cfg.MCP.Enabled {
		m := mcp.New(mcp.Options{API: mcp.NewAPI(mcp.InProcess(apiMux), "http://izci.internal"), Version: version.Version})
		a.Server.Handle("/mcp", m.Handler(a.Auth.VerifyAccessToken, log))
	}
	a.AI.OnIssueChange = a.API.Issue.PublishIssueUpdates
	if a.Bot != nil {
		a.Bot.OnTriage = a.API.Issue.PublishIssueUpdates
		if a.AI.Enabled() {
			a.Bot.Analyze = a.analyzeFromTelegram
		}
		if cfg.Telegram.Mode == "webhook" {
			a.Server.Handle("POST /telegram/webhook", a.Bot.WebhookHandler())
		}
	}

	a.jobs = jobs.NewRunner(log,
		jobs.Task{Name: "flush-usage", Interval: 15 * time.Second, Run: func(ctx context.Context) error {
			a.usage.Flush(ctx, database, log)
			a.keys.FlushUsage(ctx, log)
			a.Ingest.SessionRecorder().Flush(ctx, database, log)
			return nil
		}},
		jobs.Task{Name: "monitor-sweep", Interval: 30 * time.Second, Run: a.Monitors.Sweep},
		jobs.Task{Name: "link-feedback", Interval: time.Minute, Run: func(ctx context.Context) error {
			// Feedback can arrive before its event is stored.
			return database.InSystem(ctx, func(tx pgx.Tx) error {
				_, err := store.LinkFeedback(ctx, tx, time.Now().Add(-24*time.Hour))
				return err
			})
		}},
		jobs.Task{Name: "event-partitions", Interval: time.Hour, Run: func(ctx context.Context) error {
			now := time.Now()
			created, err := database.EnsureEventPartitions(ctx, now.AddDate(0, 0, -1), now.AddDate(0, 0, cfg.Retention.PremakeDays))
			if len(created) > 0 {
				log.Info("event partitions created", slog.Int("count", len(created)))
			}
			return err
		}},
		jobs.Task{Name: "project-purge", Interval: 5 * time.Minute, Run: func(ctx context.Context) error {
			purged, err := database.PurgeDeletedProjects(ctx, 5)
			if len(purged) > 0 {
				log.Info("deleted projects purged", slog.Any("projects", purged))
			}
			if err != nil {
				return err
			}
			orgs, err := database.PurgeDeletedOrganizations(ctx, 5)
			if len(orgs) > 0 {
				log.Info("deleted organizations purged", slog.Any("organizations", orgs))
			}
			return err
		}},
		jobs.Task{Name: "audit-forward", Interval: 15 * time.Second, Run: auditforward.New(database, cfg.Audit, log).Run},
		jobs.Task{Name: "digest", Interval: 10 * time.Minute, Run: digests.Run},
		jobs.Task{Name: "issue-purge", Interval: time.Minute, Run: func(ctx context.Context) error {
			// Events of merged and unmerged issues move first, so that a
			// deletion right after a merge finds them.
			if _, err := ingest.ProcessIssueMoves(ctx, database, 20); err != nil {
				return err
			}
			_, err := database.PurgeDeletedIssues(ctx, 20)
			return err
		}},
		jobs.Task{Name: "retention", Interval: 6 * time.Hour, RunAtStart: true, Run: func(ctx context.Context) error {
			cutoff := time.Now().AddDate(0, 0, -cfg.Retention.Days)
			res, err := database.ApplyRetention(ctx, cutoff)
			if len(res.Partitions) > 0 {
				log.Info("retention dropped event partitions", slog.Any("partitions", res.Partitions))
			}
			if err != nil {
				return err
			}
			// Organizations and projects may keep their data for less time.
			short, err := database.ShortRetentionProjects(ctx, cfg.Retention.Days)
			if err != nil {
				return err
			}
			for _, p := range short {
				n, err := database.ApplyProjectRetention(ctx, p.ProjectID, time.Now().AddDate(0, 0, -p.Days))
				if err != nil {
					return err
				}
				if n > 0 {
					log.Info("project retention deleted events", slog.String("project", p.ProjectID.String()), slog.Int64("events", n))
				}
			}
			return nil
		}},
	)
	return a, nil
}

// APIHandler mounts izci.api.v1 on a new handler for in-process clients such
// as `izci mcp`; background work (ingest, AI execution, Telegram) is not
// started.
func APIHandler(cfg *config.Config, log *slog.Logger, database *db.DB) (http.Handler, error) {
	box, err := secret.NewBox(cfg.SecretKey, cfg.SecretKeyPrevious.Bytes()...)
	if err != nil {
		return nil, err
	}
	access, err := NewGitHubAccess(cfg, box)
	if err != nil {
		return nil, err
	}
	runner := ai.New(database, log, ai.Options{Config: cfg.AI, DataDir: cfg.DataDir, PublicURL: cfg.PublicURL, GitHub: access})
	mux := http.NewServeMux()
	_, err = api.Mount(mux.Handle, api.Deps{DB: database, Auth: auth.NewService(database, cfg, box, log), Config: cfg, Log: log,
		AI: runner, GitHub: access})
	return mux, err
}

// NewGitHubAccess builds repository access from the GitHub settings: tokens
// of integrations and, when configured, the GitHub App.
func NewGitHubAccess(cfg *config.Config, box *secret.Box) (*github.Access, error) {
	var app *github.App
	if cfg.GitHub.AppConfigured() {
		key := cfg.GitHub.AppPrivateKey.Value()
		if key == "" {
			raw, err := os.ReadFile(cfg.GitHub.AppPrivateKeyFile)
			if err != nil {
				return nil, fmt.Errorf("read GITHUB_APP_PRIVATE_KEY_FILE: %w", err)
			}
			key = string(raw)
		}
		var err error
		if app, err = github.NewApp(github.AppConfig{ID: cfg.GitHub.AppID, Slug: cfg.GitHub.AppSlug, PrivateKey: key,
			ClientID: cfg.GitHub.AppClientID, ClientSecret: cfg.GitHub.AppClientSecret.Value(), APIURL: cfg.GitHub.APIURL,
			WebURL: cfg.GitHub.WebURL}, nil); err != nil {
			return nil, err
		}
	}
	return github.NewAccess(cfg.GitHub.APIURL, cfg.GitHub.WebURL, app, box, nil), nil
}

// analyzeFromTelegram starts an analysis for a Telegram button press and
// returns the (Turkish) reply shown to the member.
func (a *App) analyzeFromTelegram(ctx context.Context, orgID, issueID, userID uuid.UUID) string {
	_, err := a.AI.Start(ctx, orgID, ai.StartRequest{IssueID: issueID, Trigger: store.AiTriggerTelegram, RequestedBy: &userID})
	switch {
	case err == nil:
		return "Yapay zekâ analizi başladı; sonuç İzci'de hatanın Yapay zekâ sekmesinde görünecek."
	case errors.Is(err, store.ErrRunActive):
		return "Bu hata için bir analiz zaten sürüyor."
	case errors.Is(err, ai.ErrBudgetExceeded):
		return "Organizasyonun bu ayki yapay zekâ analiz hakkı doldu."
	case errors.Is(err, ai.ErrDisabled):
		return "Yapay zekâ analizi bu sunucuda kapalı."
	case errors.Is(err, ai.ErrNotAllowed):
		return "Yapay zekâ kullanımı bu organizasyonda kapalı. Bir yönetici Organizasyon ayarları'ndan açabilir."
	case errors.Is(err, store.ErrNotFound):
		return "Bu hata artık yok."
	}
	a.Log.Error("start AI analysis from Telegram", slog.Any("err", err))
	return "Analiz şu anda başlatılamadı. Biraz sonra yeniden deneyin."
}

// Prepare checks the server, applies migrations (when enabled) and creates
// event partitions ahead of time.
func Prepare(ctx context.Context, cfg *config.Config, log *slog.Logger, database *db.DB) error {
	info, err := database.CheckServer(ctx, cfg.IsProduction(), cfg.DB.AllowPrivilegedRole)
	if err != nil {
		return err
	}
	log.Info("database connected", slog.String("server", info.Version), slog.String("database", info.Database), slog.String("role", info.Role))
	if cfg.DB.AutoMigrate {
		if err := database.MigrateUp(ctx); err != nil {
			return err
		}
	}
	now := time.Now()
	created, err := database.EnsureEventPartitions(ctx, now.AddDate(0, 0, -1), now.AddDate(0, 0, cfg.Retention.PremakeDays))
	if err != nil {
		return err
	}
	if len(created) > 0 {
		log.Info("event partitions created", slog.Int("count", len(created)))
	}
	return nil
}

// Run starts background work and serves until ctx is cancelled, then shuts
// down in order: stop accepting requests, drain the ingest queue, stop jobs.
func (a *App) Run(ctx context.Context) error {
	if err := protectProcess(); err != nil {
		a.Log.Warn("cannot hide the process environment from other processes", slog.Any("err", err))
	}
	if required, err := a.Auth.SetupRequired(ctx); err == nil && required {
		a.Log.Warn("izci is not set up yet: open the setup page and enter the setup code",
			slog.String("setup_url", strings.TrimRight(a.Config.PublicURL, "/")+"/setup"), slog.String("setup_code", a.Auth.SetupToken()))
	}
	a.Pipeline.Start()
	jobCtx, stopJobs := context.WithCancel(context.Background())
	a.jobs.Start(jobCtx)
	var bot sync.WaitGroup
	if a.Bot != nil {
		bot.Go(func() { a.Bot.Run(jobCtx) })
	}
	// Every instance delivers; outbox rows are claimed with SKIP LOCKED.
	bot.Go(func() { a.Notify.Run(jobCtx) })
	bot.Go(func() { _ = a.Inbox.Run(jobCtx) })
	bot.Go(func() { a.Ownership.Run(jobCtx) })
	bot.Go(func() {
		if err := a.AI.Run(jobCtx); err != nil {
			a.Log.Error("AI runner stopped", slog.Any("err", err))
		}
	})

	serveErr := a.Server.Run(ctx)

	drainCtx, cancel := context.WithTimeout(context.Background(), a.Config.HTTP.ShutdownTimeout)
	defer cancel()
	if err := a.Pipeline.Close(drainCtx); err != nil {
		a.Log.Error("ingest queue not fully drained", slog.Any("err", err), slog.Int("left", a.Pipeline.QueueDepth()))
	}
	stopJobs()
	a.jobs.Wait()
	bot.Wait()
	// Final flush of counters gathered since the last tick.
	a.usage.Flush(drainCtx, a.DB, a.Log)
	a.keys.FlushUsage(drainCtx, a.Log)
	a.Ingest.SessionRecorder().Flush(drainCtx, a.DB, a.Log)
	if serveErr != nil && !errors.Is(serveErr, context.Canceled) {
		return serveErr
	}
	return nil
}
