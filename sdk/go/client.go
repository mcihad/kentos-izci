package izci

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Version is the SDK version reported with every event.
const Version = "0.1.0"

// Options configure a Client. Endpoint and Key are required.
type Options struct {
	// Endpoint is the base URL of the izci server, e.g.
	// "https://izci.example.com".
	Endpoint string
	// Key is the project API key ("izk_…").
	Key string

	// Environment, e.g. "production" (default).
	Environment string
	// Release of the application, e.g. "1.4.2" or a commit SHA.
	Release string
	// Dist is the build variant of the release.
	Dist string
	// ServerName defaults to the host name.
	ServerName string
	// CustomerID/CustomerName set the default customer, e.g. for a
	// deployment dedicated to one municipality. Scopes can override it.
	CustomerID   string
	CustomerName string
	// TenantID/TenantName set the default tenant.
	TenantID   string
	TenantName string
	// Tags are added to every event.
	Tags map[string]string

	// SampleRate is the fraction of events sent (0 means 1.0).
	SampleRate float64
	// MaxBreadcrumbs kept per scope (default 100).
	MaxBreadcrumbs int
	// AttachStacktrace adds the current stack to message events.
	AttachStacktrace bool
	// InAppInclude / InAppExclude are package path prefixes that are
	// (not) application code. By default the main module is in-app.
	InAppInclude []string
	InAppExclude []string
	// DisableContextLines stops reading source files for code context.
	DisableContextLines bool
	// DisableRuntimeContext stops attaching CPU, memory and process data.
	DisableRuntimeContext bool
	// IncludeModules attaches dependency versions to every event.
	IncludeModules bool
	// BeforeSend may modify or drop (return nil) events before sending.
	BeforeSend func(*ingestv1.Event) *ingestv1.Event

	// Protocol selects Connect (default) or native gRPC.
	Protocol Protocol
	// HTTPClient overrides the HTTP client.
	HTTPClient *http.Client
	// QueueSize is the maximum number of events waiting for delivery
	// (default 1000). Further events are dropped.
	QueueSize int
	// BatchSize is the maximum number of events per request (default 50).
	BatchSize int
	// FlushInterval is how often queued events are sent (default 1s).
	FlushInterval time.Duration
	// Logger receives SDK diagnostics at debug level; nil disables them.
	Logger *slog.Logger

	// ContextLines is derived from DisableContextLines.
	ContextLines bool
}

// Client captures events and sends them to izci.
type Client struct {
	opts      Options
	scope     *Scope
	transport *transport
}

// NewClient validates options and starts the background transport.
func NewClient(opts Options) (*Client, error) {
	if opts.Endpoint == "" || opts.Key == "" {
		return nil, errors.New("izci: Endpoint and Key are required")
	}
	if !strings.HasPrefix(opts.Endpoint, "http://") && !strings.HasPrefix(opts.Endpoint, "https://") {
		return nil, fmt.Errorf("izci: Endpoint must be an http(s) URL, got %q", opts.Endpoint)
	}
	if opts.SampleRate <= 0 || opts.SampleRate > 1 {
		opts.SampleRate = 1
	}
	if opts.MaxBreadcrumbs <= 0 {
		opts.MaxBreadcrumbs = 100
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 1000
	}
	if opts.BatchSize <= 0 || opts.BatchSize > 100 {
		opts.BatchSize = 50
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = time.Second
	}
	if opts.Environment == "" {
		opts.Environment = os.Getenv("IZCI_ENVIRONMENT")
	}
	if opts.ServerName == "" {
		opts.ServerName, _ = os.Hostname()
	}
	opts.ContextLines = !opts.DisableContextLines

	c := &Client{opts: opts, scope: NewScope(opts.MaxBreadcrumbs)}
	if opts.CustomerID != "" || opts.CustomerName != "" {
		c.scope.SetCustomer(opts.CustomerID, opts.CustomerName)
	}
	if opts.TenantID != "" || opts.TenantName != "" {
		c.scope.SetTenant(opts.TenantID, opts.TenantName)
	}
	for k, v := range opts.Tags {
		c.scope.SetTag(k, v)
	}
	c.transport = newTransport(opts)
	return c, nil
}

// Scope returns the client's global scope.
func (c *Client) Scope() *Scope { return c.scope }

// Flush waits until queued events are delivered or timeout elapses.
func (c *Client) Flush(timeout time.Duration) bool { return c.transport.flush(timeout) }

// Close flushes and stops the client.
func (c *Client) Close(timeout time.Duration) bool {
	ok := c.transport.flush(timeout)
	return c.transport.close(timeout) && ok
}

// Dropped returns the number of events dropped (queue full, rate limited,
// rejected).
func (c *Client) Dropped() int64 { return c.transport.dropped.Load() }

// CaptureError reports an error with the stack trace of the call site (or
// the one recorded in the error) and returns the event id.
func (c *Client) CaptureError(ctx context.Context, err error, opts ...EventOption) string {
	if err == nil {
		return ""
	}
	ev := c.errorEvent(err, true, "generic", 1)
	return c.CaptureEvent(ctx, ev, opts...)
}

// CapturePanic reports a recovered panic value as an unhandled error.
func (c *Client) CapturePanic(ctx context.Context, recovered any, opts ...EventOption) string {
	err, ok := recovered.(error)
	if !ok {
		err = &panicValue{v: recovered}
	}
	ev := c.errorEvent(err, false, "panic", 2)
	if st := c.buildStacktrace(panicCallers()); st != nil {
		ev.Exceptions[len(ev.Exceptions)-1].Stacktrace = st
	}
	ev.Level = ingestv1.Level_LEVEL_FATAL
	return c.CaptureEvent(ctx, ev, opts...)
}

// CaptureMessage reports a message.
func (c *Client) CaptureMessage(ctx context.Context, level Level, msg string, opts ...EventOption) string {
	ev := &ingestv1.Event{Level: ingestv1.Level(level), Message: &ingestv1.Message{Formatted: msg}}
	if c.opts.AttachStacktrace {
		if st := c.buildStacktrace(callers(1)); st != nil {
			ev.Threads = []*ingestv1.Thread{{Id: "current", Current: true, Stacktrace: st}}
		}
	}
	return c.CaptureEvent(ctx, ev, opts...)
}

// CaptureEvent enriches and sends a prepared event.
func (c *Client) CaptureEvent(ctx context.Context, ev *ingestv1.Event, opts ...EventOption) string {
	if c.opts.SampleRate < 1 && mrand.Float64() >= c.opts.SampleRate {
		return ""
	}
	if ev.EventId == "" {
		ev.EventId = newUUID()
	}
	if ev.Timestamp == nil {
		ev.Timestamp = timestamppb.Now()
	}
	ev.Platform = "go"
	setDefault(&ev.Environment, c.opts.Environment)
	setDefault(&ev.Release, c.opts.Release)
	setDefault(&ev.Dist, c.opts.Dist)
	setDefault(&ev.ServerName, c.opts.ServerName)
	ev.Sdk = &ingestv1.Sdk{Name: "izci.go", Version: Version,
		Packages: []*ingestv1.SdkPackage{{Name: "go:" + sdkPackage, Version: Version}}}

	for _, o := range opts {
		o(ev)
	}
	if s := FromContext(ctx); s != nil {
		s.apply(ev)
	} else {
		c.scope.apply(ev)
	}
	if !c.opts.DisableRuntimeContext {
		rc := runtimeContexts()
		if ev.Contexts == nil {
			ev.Contexts = rc
		} else {
			if ev.Contexts.Runtime == nil {
				ev.Contexts.Runtime = rc.Runtime
			}
			if ev.Contexts.Os == nil {
				ev.Contexts.Os = rc.Os
			}
			if ev.Contexts.Device == nil {
				ev.Contexts.Device = rc.Device
			}
			if ev.Contexts.Process == nil {
				ev.Contexts.Process = rc.Process
			}
		}
	}
	if c.opts.IncludeModules {
		ev.Modules = buildModules()
	}
	if c.opts.BeforeSend != nil {
		if ev = c.opts.BeforeSend(ev); ev == nil {
			return ""
		}
	}
	if !c.transport.enqueue(ev) {
		return ""
	}
	return ev.EventId
}

// errorEvent builds an event from an error chain. skip counts frames above
// the public capture function.
func (c *Client) errorEvent(err error, handled bool, mechanism string, skip int) *ingestv1.Event {
	chain := errorChain(err)
	ev := &ingestv1.Event{Level: ingestv1.Level_LEVEL_ERROR}
	// izci orders the chain from the root cause to the primary error.
	for i := len(chain) - 1; i >= 0; i-- {
		e := chain[i]
		ex := &ingestv1.Exception{Type: typeName(e), Value: e.Error()}
		if pcs := stackOf(e); len(pcs) > 0 {
			ex.Stacktrace = c.buildStacktrace(pcs)
		}
		ev.Exceptions = append(ev.Exceptions, ex)
	}
	primary := ev.Exceptions[len(ev.Exceptions)-1]
	if primary.Stacktrace == nil {
		primary.Stacktrace = c.buildStacktrace(callers(skip + 2))
	}
	h := handled
	primary.Mechanism = &ingestv1.Mechanism{Type: mechanism, Handled: &h}
	return ev
}

// errorChain returns err and the errors it wraps, outermost first. Joined
// errors contribute their first member to the chain (up to 10 errors).
func errorChain(err error) []error {
	var chain []error
	seen := map[error]bool{}
	for err != nil && len(chain) < 10 {
		if isComparable(err) {
			if seen[err] {
				break
			}
			seen[err] = true
		}
		if !isTransparentWrapper(err) || len(chain) == 0 {
			chain = append(chain, err)
		}
		switch u := err.(type) {
		case interface{ Unwrap() error }:
			err = u.Unwrap()
		case interface{ Unwrap() []error }:
			errs := u.Unwrap()
			if len(errs) == 0 {
				err = nil
			} else {
				err = errs[0]
			}
		default:
			err = nil
		}
	}
	return chain
}

func isComparable(err error) bool {
	return reflect.TypeOf(err).Comparable()
}

// isTransparentWrapper reports whether err only adds context to the error
// it wraps (fmt.Errorf with %w); such links are merged into their parent's
// message instead of appearing as separate exceptions.
func isTransparentWrapper(err error) bool {
	switch reflect.TypeOf(err).String() {
	case "*fmt.wrapError", "*fmt.wrapErrors":
		return true
	}
	return false
}

// typeName renders the dynamic type of an error, with friendlier names for
// the standard library's anonymous error types.
func typeName(err error) string {
	if p, ok := err.(*panicValue); ok {
		return p.typeName()
	}
	if _, ok := err.(runtime.Error); ok {
		return "runtime.Error"
	}
	t := reflect.TypeOf(err).String()
	switch t {
	case "*errors.errorString", "*fmt.wrapError", "*fmt.wrapErrors":
		// Anonymous errors carry no type information; the message is the
		// title and the stack trace groups them.
		return ""
	case "*errors.joinError":
		return "errors.Join"
	}
	return t
}

type panicValue struct{ v any }

func (p *panicValue) Error() string { return fmt.Sprint(p.v) }

func (p *panicValue) typeName() string { return "panic" }

func setDefault(dst *string, v string) {
	if *dst == "" {
		*dst = v
	}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// EventOption modifies an event at capture time.
type EventOption func(*ingestv1.Event)

// WithLevel sets the event level.
func WithLevel(l Level) EventOption { return func(ev *ingestv1.Event) { ev.Level = ingestv1.Level(l) } }

// WithTag adds a tag to the event.
func WithTag(key, value string) EventOption {
	return func(ev *ingestv1.Event) {
		if ev.Tags == nil {
			ev.Tags = map[string]string{}
		}
		ev.Tags[key] = value
	}
}

// WithFingerprint overrides grouping for the event.
func WithFingerprint(parts ...string) EventOption {
	return func(ev *ingestv1.Event) { ev.Fingerprint = parts }
}

// WithCustomer sets the customer of the event.
func WithCustomer(id, name string) EventOption {
	return func(ev *ingestv1.Event) { ev.Customer = &ingestv1.Customer{Id: id, Name: name} }
}
