package ingest

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mcihad/kentos-izci/internal/db"
)

// PipelineConfig tunes batching.
type PipelineConfig struct {
	// Shards is the number of writer goroutines; events of one project always
	// go to the same shard, so a project's issues are written by one writer.
	Shards int
	// QueueSize is the capacity of each shard queue.
	QueueSize int
	// BatchSize caps the events written per transaction.
	BatchSize int
	// Linger optionally waits for more events after the first one of a
	// batch. Zero relies on natural batching: events that arrive while a
	// batch is being written form the next batch.
	Linger time.Duration
	// PublicURL builds issue links in results.
	PublicURL string
}

// Result is the outcome of one submitted event.
type Result struct {
	IssueID        uuid.UUID
	IssueShortID   string
	IssueURL       string
	IssueNew       bool
	IssueRegressed bool
	Err            error
}

// Hooks let other subsystems take part in batch processing.
type Hooks struct {
	// InTx runs inside the batch transaction after issues and events are
	// written; alert evaluation writes its outbox rows here.
	InTx func(ctx context.Context, tx pgx.Tx, b *Batch) error
	// AfterCommit runs after a successful commit (live updates, automation).
	AfterCommit func(b *Batch)
}

// ErrOverloaded means the shard queue is full; callers should answer with a
// retryable error.
var ErrOverloaded = errors.New("ingest queue is full")

// ErrClosed means the pipeline is shutting down.
var ErrClosed = errors.New("ingest pipeline is closed")

type job struct {
	p    *Processed
	done chan Result
}

// Pipeline batches processed events into group-committed transactions.
type Pipeline struct {
	db     *db.DB
	log    *slog.Logger
	cfg    PipelineConfig
	hooks  Hooks
	shards []chan *job
	wg     sync.WaitGroup
	mu     sync.RWMutex
	closed bool

	stats PipelineStats
}

// PipelineStats are cumulative counters (exported as metrics).
type PipelineStats struct {
	Batches     atomic.Int64
	Events      atomic.Int64
	Failed      atomic.Int64
	Overloaded  atomic.Int64
	LastBatchMs atomic.Int64
}

// NewPipeline creates a pipeline; call Start to run it.
func NewPipeline(database *db.DB, log *slog.Logger, cfg PipelineConfig, hooks Hooks) *Pipeline {
	if cfg.Shards <= 0 {
		cfg.Shards = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 5000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	pl := &Pipeline{db: database, log: log, cfg: cfg, hooks: hooks}
	for range cfg.Shards {
		pl.shards = append(pl.shards, make(chan *job, cfg.QueueSize))
	}
	return pl
}

// Stats exposes counters.
func (pl *Pipeline) Stats() *PipelineStats { return &pl.stats }

// QueueDepth returns the number of queued events across shards.
func (pl *Pipeline) QueueDepth() int {
	n := 0
	for _, ch := range pl.shards {
		n += len(ch)
	}
	return n
}

// Start launches the shard writers.
func (pl *Pipeline) Start() {
	for i, ch := range pl.shards {
		pl.wg.Add(1)
		go pl.runShard(i, ch)
	}
}

// Close stops accepting events, writes everything already queued and waits
// for the writers (or ctx).
func (pl *Pipeline) Close(ctx context.Context) error {
	pl.mu.Lock()
	if !pl.closed {
		pl.closed = true
		for _, ch := range pl.shards {
			close(ch)
		}
	}
	pl.mu.Unlock()
	done := make(chan struct{})
	go func() { pl.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Submit enqueues processed events. The returned channels receive exactly
// one Result each once the batch containing the event committed (or
// failed). Submission never blocks: a full queue yields ErrOverloaded.
func (pl *Pipeline) Submit(ps ...*Processed) ([]<-chan Result, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	if pl.closed {
		return nil, ErrClosed
	}
	out := make([]<-chan Result, len(ps))
	for i, p := range ps {
		j := &job{p: p, done: make(chan Result, 1)}
		select {
		case pl.shards[pl.shardOf(p.ProjectID)] <- j:
			out[i] = j.done
		default:
			pl.stats.Overloaded.Add(1)
			// Resolve the rest immediately so callers never wait forever.
			for k := i; k < len(ps); k++ {
				ch := make(chan Result, 1)
				ch <- Result{Err: ErrOverloaded}
				out[k] = ch
			}
			return out, ErrOverloaded
		}
	}
	return out, nil
}

func (pl *Pipeline) shardOf(project uuid.UUID) int {
	h := fnv.New32a()
	h.Write(project[:])
	return int(h.Sum32() % uint32(len(pl.shards)))
}

func (pl *Pipeline) runShard(idx int, ch chan *job) {
	defer pl.wg.Done()
	batch := make([]*job, 0, pl.cfg.BatchSize)
	for {
		j, ok := <-ch
		if !ok {
			return
		}
		batch = append(batch[:0], j)
		// Natural batching: take whatever is already queued.
		deadline := time.Now().Add(pl.cfg.Linger)
	collect:
		for len(batch) < pl.cfg.BatchSize {
			select {
			case next, ok := <-ch:
				if !ok {
					break collect
				}
				batch = append(batch, next)
			default:
				if pl.cfg.Linger <= 0 || time.Now().After(deadline) {
					break collect
				}
				time.Sleep(200 * time.Microsecond)
			}
		}
		pl.flush(idx, batch)
	}
}

// flush writes a batch with retries for transient errors. When a batch
// fails permanently it is retried event by event so that one bad event
// cannot drop its neighbours.
func (pl *Pipeline) flush(shard int, jobs []*job) {
	start := time.Now()
	b, err := pl.writeWithRetry(jobs)
	if err != nil && len(jobs) > 1 && !isTransient(err) {
		pl.log.Warn("ingest batch failed, retrying events individually", slog.Int("shard", shard), slog.Int("events", len(jobs)), slog.Any("err", err))
		for _, j := range jobs {
			pl.flush(shard, []*job{j})
		}
		return
	}
	pl.stats.LastBatchMs.Store(time.Since(start).Milliseconds())
	if err != nil {
		pl.stats.Failed.Add(int64(len(jobs)))
		pl.log.Error("ingest batch failed", slog.Int("shard", shard), slog.Int("events", len(jobs)), slog.Any("err", err))
		for _, j := range jobs {
			j.done <- Result{Err: err}
		}
		return
	}
	pl.stats.Batches.Add(1)
	pl.stats.Events.Add(int64(len(jobs)))
	for _, j := range jobs {
		o := b.Issues[issueKeyOf(j.p)]
		res := Result{IssueID: o.ID, IssueShortID: o.ShortID, IssueNew: o.Created, IssueRegressed: o.Regressed}
		if o.ShortID != "" && pl.cfg.PublicURL != "" {
			res.IssueURL = pl.cfg.PublicURL + "/o/" + j.p.OrgSlug + "/issues/" + o.ShortID
		}
		j.done <- res
	}
	if pl.hooks.AfterCommit != nil {
		pl.hooks.AfterCommit(b)
	}
}

func (pl *Pipeline) writeWithRetry(jobs []*job) (*Batch, error) {
	var lastErr error
	for attempt := range 4 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * 50 * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		b, err := pl.writeBatch(ctx, jobs)
		cancel()
		if err == nil {
			return b, nil
		}
		lastErr = err
		if !isTransient(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// isTransient reports whether retrying the same batch may succeed.
func isTransient(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "40001", "40P01", "55P03", "57P01", "53300", "08006", "08003":
			return true
		}
		return strings.HasPrefix(pgErr.Code, "08")
	}
	return errors.Is(err, context.DeadlineExceeded) || pgconn.SafeToRetry(err) || strings.Contains(err.Error(), "connection")
}

// ShortID renders an issue short id: "<SLUG>-<base36 seq>".
func ShortID(projectSlug string, seq int32) string {
	if seq <= 0 {
		return ""
	}
	return strings.ToUpper(projectSlug) + "-" + strings.ToUpper(strconv.FormatInt(int64(seq), 36))
}

// ParseShortID splits a short id into project slug and sequence.
func ParseShortID(s string) (slug string, seq int32, ok bool) {
	i := strings.LastIndexByte(s, '-')
	if i <= 0 || i == len(s)-1 {
		return "", 0, false
	}
	n, err := strconv.ParseInt(strings.ToLower(s[i+1:]), 36, 32)
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return strings.ToLower(s[:i]), int32(n), true
}
