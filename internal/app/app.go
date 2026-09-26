// Package app assembles izci: database, ingest pipeline, services,
// background jobs and the HTTP server. The CLI's serve command and end-to-end
// tests both build the application through here.
package app

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"time"

	"connectrpc.com/connect"

	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/ingest/sentry"
	"github.com/mcihad/kentos-izci/internal/jobs"
	"github.com/mcihad/kentos-izci/internal/server"
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
	a := &App{Config: cfg, Log: log, DB: database}

	a.Pipeline = ingest.NewPipeline(database, log, ingest.PipelineConfig{
		Shards:    cfg.Ingest.Shards,
		QueueSize: cfg.Ingest.QueueSize,
		BatchSize: cfg.Ingest.BatchSize,
		Linger:    cfg.Ingest.Linger,
		PublicURL: cfg.PublicURL,
	}, ingest.Hooks{})

	a.keys = ingest.NewKeyResolver(database, 30*time.Second)
	a.usage = ingest.NewUsageRecorder()
	a.Ingest = ingest.NewService(ingest.ServiceConfig{
		Durable:        cfg.Ingest.Ack == "durable",
		AckTimeout:     cfg.Ingest.AckTimeout,
		Retention:      time.Duration(cfg.Retention.Days) * 24 * time.Hour,
		TrustedProxies: cfg.HTTP.TrustedProxies,
	}, database, log, a.keys, ingest.NewRateLimiter(cfg.Ingest.RateLimitPerMinute),
		ingest.NewQuotaTracker(database), a.usage, ingest.NewDedupe(cfg.Ingest.DedupeSize, time.Hour), a.Pipeline)

	a.Server = server.New(server.Options{Config: cfg, Logger: log, DB: database, UI: opts.UI})
	path, handler := ingestv1connect.NewIngestServiceHandler(a.Ingest,
		connect.WithReadMaxBytes(8<<20),
		connect.WithCompressMinBytes(1024),
	)
	a.Server.Handle(path, ingest.CORS(handler))
	sentry.NewHandler(a.Ingest, log).Register(a.Server.Handle)

	a.jobs = jobs.NewRunner(log,
		jobs.Task{Name: "flush-usage", Interval: 15 * time.Second, Run: func(ctx context.Context) error {
			a.usage.Flush(ctx, database, log)
			a.keys.FlushUsage(ctx, log)
			return nil
		}},
		jobs.Task{Name: "event-partitions", Interval: time.Hour, Run: func(ctx context.Context) error {
			now := time.Now()
			created, err := database.EnsureEventPartitions(ctx, now.AddDate(0, 0, -1), now.AddDate(0, 0, cfg.Retention.PremakeDays))
			if len(created) > 0 {
				log.Info("event partitions created", slog.Int("count", len(created)))
			}
			return err
		}},
		jobs.Task{Name: "retention", Interval: 6 * time.Hour, RunAtStart: true, Run: func(ctx context.Context) error {
			cutoff := time.Now().AddDate(0, 0, -cfg.Retention.Days)
			res, err := database.ApplyRetention(ctx, cutoff)
			if len(res.Partitions) > 0 {
				log.Info("retention dropped event partitions", slog.Any("partitions", res.Partitions))
			}
			return err
		}},
	)
	return a, nil
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
	a.Pipeline.Start()
	jobCtx, stopJobs := context.WithCancel(context.Background())
	a.jobs.Start(jobCtx)

	serveErr := a.Server.Run(ctx)

	drainCtx, cancel := context.WithTimeout(context.Background(), a.Config.HTTP.ShutdownTimeout)
	defer cancel()
	if err := a.Pipeline.Close(drainCtx); err != nil {
		a.Log.Error("ingest queue not fully drained", slog.Any("err", err), slog.Int("left", a.Pipeline.QueueDepth()))
	}
	stopJobs()
	a.jobs.Wait()
	// Final flush of counters gathered since the last tick.
	a.usage.Flush(drainCtx, a.DB, a.Log)
	a.keys.FlushUsage(drainCtx, a.Log)
	if serveErr != nil && !errors.Is(serveErr, context.Canceled) {
		return serveErr
	}
	return nil
}
