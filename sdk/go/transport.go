package izci

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// transport delivers events asynchronously in batches. It never blocks the
// application: when the queue is full or the server asked to back off,
// events are dropped (and counted).
type transport struct {
	client    ingestv1connect.IngestServiceClient
	key       string
	queue     chan *ingestv1.Event
	flushReq  chan chan struct{}
	done      chan struct{}
	stopped   chan struct{}
	batchSize int
	interval  time.Duration
	log       *slog.Logger

	backoffUntil atomic.Int64
	dropped      atomic.Int64
	delivered    atomic.Int64
	pending      sync.WaitGroup
	closeOnce    sync.Once
}

func newTransport(opts Options) *transport {
	hc := opts.HTTPClient
	grpcOpts := []connect.ClientOption{connect.WithSendGzip()}
	if opts.Protocol == ProtocolGRPC {
		grpcOpts = append(grpcOpts, connect.WithGRPC())
		if hc == nil {
			tr := &http.Transport{Protocols: new(http.Protocols), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
			if strings.HasPrefix(opts.Endpoint, "http://") {
				tr.Protocols.SetUnencryptedHTTP2(true)
			} else {
				tr.Protocols.SetHTTP2(true)
			}
			hc = &http.Client{Transport: tr}
		}
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	t := &transport{
		client:    ingestv1connect.NewIngestServiceClient(hc, strings.TrimRight(opts.Endpoint, "/"), grpcOpts...),
		key:       opts.Key,
		queue:     make(chan *ingestv1.Event, opts.QueueSize),
		flushReq:  make(chan chan struct{}),
		done:      make(chan struct{}),
		stopped:   make(chan struct{}),
		batchSize: opts.BatchSize,
		interval:  opts.FlushInterval,
		log:       opts.Logger,
	}
	go t.run()
	return t
}

// enqueue queues an event; it reports false when the event was dropped.
func (t *transport) enqueue(ev *ingestv1.Event) bool {
	if until := t.backoffUntil.Load(); until > 0 && time.Now().UnixNano() < until {
		t.dropped.Add(1)
		return false
	}
	select {
	case <-t.done:
		t.dropped.Add(1)
		return false
	default:
	}
	select {
	case t.queue <- ev:
		t.pending.Add(1)
		return true
	default:
		t.dropped.Add(1)
		t.debug("izci: queue full, event dropped")
		return false
	}
}

func (t *transport) run() {
	defer close(t.stopped)
	batch := make([]*ingestv1.Event, 0, t.batchSize)
	timer := time.NewTimer(t.interval)
	defer timer.Stop()
	deliverAll := func() {
		if len(batch) > 0 {
			t.deliver(batch)
			batch = batch[:0]
		}
	}
	for {
		select {
		case ev := <-t.queue:
			batch = append(batch, ev)
			if len(batch) >= t.batchSize {
				deliverAll()
			}
		case <-timer.C:
			deliverAll()
			timer.Reset(t.interval)
		case ack := <-t.flushReq:
			for drained := false; !drained; {
				select {
				case ev := <-t.queue:
					batch = append(batch, ev)
					if len(batch) >= t.batchSize {
						deliverAll()
					}
				default:
					drained = true
				}
			}
			deliverAll()
			close(ack)
		case <-t.done:
			for {
				select {
				case ev := <-t.queue:
					batch = append(batch, ev)
					if len(batch) >= t.batchSize {
						deliverAll()
					}
				default:
					deliverAll()
					return
				}
			}
		}
	}
}

// deliver sends one batch with retries for transient failures.
func (t *transport) deliver(batch []*ingestv1.Event) {
	defer func() {
		for range batch {
			t.pending.Done()
		}
	}()
	events := append([]*ingestv1.Event(nil), batch...)
	for attempt := range 4 {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt*attempt) * 300 * time.Millisecond):
			case <-t.done:
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		req := connect.NewRequest(&ingestv1.SendEventsRequest{Events: events})
		req.Header().Set("Authorization", "Bearer "+t.key)
		req.Header().Set("User-Agent", "izci-go/"+Version)
		resp, err := t.client.SendEvents(ctx, req)
		cancel()
		if err == nil {
			for _, r := range resp.Msg.GetResults() {
				switch r.GetOutcome() {
				case ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED, ingestv1.EventOutcome_EVENT_OUTCOME_DUPLICATE:
					t.delivered.Add(1)
				default:
					t.dropped.Add(1)
					t.debug("izci: event not accepted", "outcome", r.GetOutcome().String(), "error", r.GetError())
				}
			}
			if s := resp.Header().Get("Retry-After"); s != "" {
				t.backOff(parseSeconds(s))
			}
			return
		}
		switch connect.CodeOf(err) {
		case connect.CodeResourceExhausted:
			t.backOff(retryDelay(err))
			t.dropped.Add(int64(len(events)))
			t.debug("izci: rate limited, events dropped", "count", len(events))
			return
		case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeUnknown, connect.CodeInternal, connect.CodeAborted:
			t.debug("izci: delivery failed, retrying", "attempt", attempt+1, "err", err)
			continue
		default:
			t.dropped.Add(int64(len(events)))
			t.debug("izci: delivery rejected", "err", err)
			return
		}
	}
	t.dropped.Add(int64(len(events)))
}

func (t *transport) backOff(d time.Duration) {
	if d <= 0 {
		d = 10 * time.Second
	}
	t.backoffUntil.Store(time.Now().Add(d).UnixNano())
}

func retryDelay(err error) time.Duration {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		for _, d := range cerr.Details() {
			if v, derr := d.Value(); derr == nil {
				if ri, ok := v.(*errdetails.RetryInfo); ok {
					return ri.GetRetryDelay().AsDuration()
				}
			}
		}
		if s := cerr.Meta().Get("Retry-After"); s != "" {
			return parseSeconds(s)
		}
	}
	return 0
}

func parseSeconds(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// flush waits until every queued event was delivered or dropped.
func (t *transport) flush(timeout time.Duration) bool {
	ack := make(chan struct{})
	select {
	case t.flushReq <- ack:
	case <-t.stopped:
		return true
	case <-time.After(timeout):
		return false
	}
	done := make(chan struct{})
	go func() { <-ack; t.pending.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (t *transport) close(timeout time.Duration) bool {
	t.closeOnce.Do(func() { close(t.done) })
	select {
	case <-t.stopped:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (t *transport) debug(msg string, args ...any) {
	if t.log != nil {
		t.log.Debug(msg, args...)
	}
}
