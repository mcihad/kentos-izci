// Package ingesttest builds a complete ingest environment for tests: a
// migrated private database schema, an organization with a project and an
// API key, the pipeline, the ingest service and an HTTP server speaking
// HTTP/1.1 and cleartext HTTP/2.
package ingesttest

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/monitor"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/symbols"
	"github.com/mcihad/kentos-izci/internal/testutil"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// Options customize the environment.
type Options struct {
	// KeyRateLimit sets the API key's events-per-minute limit (0 = none).
	KeyRateLimit int32
	// AllowedOrigins of the API key.
	AllowedOrigins []string
	// Mount registers extra handlers (e.g. the Sentry endpoints) on the mux.
	Mount func(mux *http.ServeMux, svc *ingest.Service)
	// Async answers once events are queued (INGEST_ACK=async).
	Async bool
	// QueueSize of each writer queue (default: the pipeline's).
	QueueSize int
	// Stopped leaves the writers stopped, so queued events stay queued.
	Stopped bool
	// InTx runs inside every batch transaction (see ingest.Hooks).
	InTx func(ctx context.Context, tx pgx.Tx, b *ingest.Batch) error
}

// Env is a running ingest environment.
type Env struct {
	DB       *db.DB
	Org      *store.Organization
	Project  *store.Project
	Key      string
	KeyID    uuid.UUID
	Service  *ingest.Service
	Pipeline *ingest.Pipeline
	// Monitors processes cron check-ins; its failures become issues.
	Monitors *monitor.Service
	// Symbols resolves stack traces with uploaded source maps and debug
	// files.
	Symbols *symbols.Service
	// Keys resolves API keys.
	Keys *ingest.KeyResolver
	// Mux serves Server; tests may mount more handlers on it.
	Mux    *http.ServeMux
	Server *httptest.Server
	// Batches receives committed batches (buffered, best effort).
	Batches chan *ingest.Batch
}

// New starts an environment that is torn down with the test.
func New(t testing.TB, opts Options) *Env {
	t.Helper()
	ctx := context.Background()
	database := testutil.NewDB(t)
	if _, err := database.EnsureEventPartitions(ctx, time.Now().AddDate(0, 0, -2), time.Now().AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	e := &Env{DB: database, Batches: make(chan *ingest.Batch, 256)}
	err := database.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if e.Org, err = store.CreateOrganization(ctx, tx, "kentos", "KentOS"); err != nil {
			return err
		}
		if e.Project, err = store.CreateProject(ctx, tx, e.Org.ID, "kalem", "KentOS Kalem", "dotnet"); err != nil {
			return err
		}
		k, secret, err := store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{
			OrgID: e.Org.ID, ProjectID: e.Project.ID, Name: "test",
			Scopes:             []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST, apiv1.ApiKeyScope_API_KEY_SCOPE_RELEASES},
			AllowedOrigins:     opts.AllowedOrigins,
			RateLimitPerMinute: opts.KeyRateLimit,
		})
		e.Key, e.KeyID = secret, k.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.DiscardHandler)
	e.Pipeline = ingest.NewPipeline(database, log, ingest.PipelineConfig{Shards: 2, QueueSize: opts.QueueSize, BatchSize: 200, PublicURL: "https://izci.test"},
		ingest.Hooks{InTx: opts.InTx, AfterCommit: func(b *ingest.Batch) {
			select {
			case e.Batches <- b:
			default:
			}
		}})
	if !opts.Stopped {
		e.Pipeline.Start()
		t.Cleanup(func() { _ = e.Pipeline.Close(context.Background()) })
	}

	e.Keys = ingest.NewKeyResolver(database, time.Minute)
	e.Service = ingest.NewService(ingest.ServiceConfig{Durable: !opts.Async, Retention: 90 * 24 * time.Hour}, database, log,
		e.Keys, ingest.NewRateLimiter(1_000_000), ingest.NewQuotaTracker(database),
		ingest.NewUsageRecorder(), ingest.NewDedupe(10000, time.Hour), e.Pipeline)
	e.Symbols = symbols.NewService(database, log, symbols.Options{CacheBytes: 64 << 20})
	e.Service.SetSymbolicator(e.Symbols)
	e.Monitors = monitor.NewService(database, log, ingest.NewIncidentEmitter(database, e.Pipeline, 90*24*time.Hour))
	e.Service.SetMonitors(e.Monitors)
	path, handler := ingestv1connect.NewIngestServiceHandler(e.Service)
	mux := http.NewServeMux()
	e.Mux = mux
	mux.Handle(path, ingest.CORS(handler))
	if opts.Mount != nil {
		opts.Mount(mux, e.Service)
	}

	srv := httptest.NewUnstartedServer(mux)
	srv.Config.Protocols = new(http.Protocols)
	srv.Config.Protocols.SetHTTP1(true)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Start()
	t.Cleanup(srv.Close)
	e.Server = srv
	return e
}

// Scalar runs a query in the organization's scope and scans one value.
func (e *Env) Scalar(t testing.TB, dest any, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := e.DB.InOrg(ctx, e.Org.ID, func(tx pgx.Tx) error { return tx.QueryRow(ctx, sql, args...).Scan(dest) }); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
}
