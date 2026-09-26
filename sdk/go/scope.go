package izci

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Scope holds contextual data attached to every event captured with it:
// user, customer, tenant, tags, extra data, named contexts, breadcrumbs,
// fingerprint and the current request. Scopes are safe for concurrent use.
//
// The global scope is configured with ConfigureScope. Per-request scopes are
// derived with NewContext / FromContext (the net/http integration does this
// for you), so that data of one request never leaks into another.
type Scope struct {
	mu          sync.RWMutex
	user        *ingestv1.User
	customer    *ingestv1.Customer
	tenant      *ingestv1.Tenant
	tags        map[string]string
	extra       map[string]any
	contexts    map[string]map[string]any
	breadcrumbs []*ingestv1.Breadcrumb
	maxCrumbs   int
	fingerprint []string
	level       ingestv1.Level
	transaction string
	request     *ingestv1.Request
	database    *ingestv1.DatabaseContext
}

// NewScope returns an empty scope keeping up to maxBreadcrumbs breadcrumbs.
func NewScope(maxBreadcrumbs int) *Scope {
	if maxBreadcrumbs <= 0 {
		maxBreadcrumbs = 100
	}
	return &Scope{maxCrumbs: maxBreadcrumbs}
}

// Clone returns an independent copy.
func (s *Scope) Clone() *Scope {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &Scope{
		tags:        maps.Clone(s.tags),
		extra:       maps.Clone(s.extra),
		contexts:    maps.Clone(s.contexts),
		breadcrumbs: slices.Clone(s.breadcrumbs),
		maxCrumbs:   s.maxCrumbs,
		fingerprint: slices.Clone(s.fingerprint),
		level:       s.level,
		transaction: s.transaction,
	}
	if s.user != nil {
		c.user = proto.CloneOf(s.user)
	}
	if s.customer != nil {
		c.customer = proto.CloneOf(s.customer)
	}
	if s.tenant != nil {
		c.tenant = proto.CloneOf(s.tenant)
	}
	if s.request != nil {
		c.request = proto.CloneOf(s.request)
	}
	if s.database != nil {
		c.database = proto.CloneOf(s.database)
	}
	return c
}

// SetUser identifies the affected end user. Provide at least one of id,
// username, email or IP; use "{{auto}}" as IP to let izci use the address the
// event was sent from.
func (s *Scope) SetUser(u User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.user = &ingestv1.User{Id: u.ID, Username: u.Username, Email: u.Email, Name: u.Name, IpAddress: u.IPAddress,
		Segment: u.Segment, Attributes: maps.Clone(u.Attributes)}
}

// SetCustomer sets the customer (e.g. the municipality) the application is
// serving.
func (s *Scope) SetCustomer(id, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.customer = &ingestv1.Customer{Id: id, Name: name}
}

// SetTenant sets the tenant inside a multi-tenant application.
func (s *Scope) SetTenant(id, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenant = &ingestv1.Tenant{Id: id, Name: name}
}

// SetTag sets an indexed tag (searchable and charted in izci).
func (s *Scope) SetTag(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tags == nil {
		s.tags = map[string]string{}
	}
	s.tags[key] = value
}

// SetExtra attaches arbitrary, non-indexed data.
func (s *Scope) SetExtra(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.extra == nil {
		s.extra = map[string]any{}
	}
	s.extra[key] = value
}

// SetContext attaches a named structured block, e.g. SetContext("payment",
// map[string]any{"provider": "iyzico"}).
func (s *Scope) SetContext(name string, value map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.contexts == nil {
		s.contexts = map[string]map[string]any{}
	}
	s.contexts[name] = value
}

// SetFingerprint overrides grouping; "{{ default }}" keeps the default
// fingerprint as one component.
func (s *Scope) SetFingerprint(parts ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fingerprint = slices.Clone(parts)
}

// SetLevel overrides the level of captured events.
func (s *Scope) SetLevel(l Level) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.level = ingestv1.Level(l)
}

// SetTransaction names the current operation, e.g. "GET /orders/{id}".
func (s *Scope) SetTransaction(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transaction = name
}

// SetRequest attaches the HTTP request being handled.
func (s *Scope) SetRequest(r *ingestv1.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.request = r
}

// SetDatabase records the database operation in progress (the pgx
// integration uses it to attach failing queries).
func (s *Scope) SetDatabase(d *ingestv1.DatabaseContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.database = d
}

// AddBreadcrumb records something that happened before an error.
func (s *Scope) AddBreadcrumb(b Breadcrumb) {
	crumb := &ingestv1.Breadcrumb{Type: b.Type, Category: b.Category, Level: ingestv1.Level(b.Level), Message: b.Message}
	if b.Timestamp.IsZero() {
		b.Timestamp = time.Now()
	}
	crumb.Timestamp = timestamppb.New(b.Timestamp)
	if len(b.Data) > 0 {
		crumb.Data, _ = structpb.NewStruct(jsonable(b.Data))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.breadcrumbs = append(s.breadcrumbs, crumb)
	if over := len(s.breadcrumbs) - s.maxCrumbs; over > 0 {
		s.breadcrumbs = slices.Delete(s.breadcrumbs, 0, over)
	}
}

// ClearBreadcrumbs removes all breadcrumbs.
func (s *Scope) ClearBreadcrumbs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.breadcrumbs = nil
}

// apply copies the scope into an event without overriding explicit event
// data.
func (s *Scope) apply(ev *ingestv1.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ev.User == nil && s.user != nil {
		ev.User = proto.CloneOf(s.user)
	}
	if ev.Customer == nil && s.customer != nil {
		ev.Customer = proto.CloneOf(s.customer)
	}
	if ev.Tenant == nil && s.tenant != nil {
		ev.Tenant = proto.CloneOf(s.tenant)
	}
	if len(s.tags) > 0 {
		if ev.Tags == nil {
			ev.Tags = map[string]string{}
		}
		for k, v := range s.tags {
			if _, set := ev.Tags[k]; !set {
				ev.Tags[k] = v
			}
		}
	}
	if len(s.extra) > 0 {
		merged := map[string]any{}
		if ev.Extra != nil {
			merged = ev.Extra.AsMap()
		}
		for k, v := range s.extra {
			if _, set := merged[k]; !set {
				merged[k] = v
			}
		}
		ev.Extra, _ = structpb.NewStruct(jsonable(merged))
	}
	if len(s.contexts) > 0 {
		if ev.Contexts == nil {
			ev.Contexts = &ingestv1.Contexts{}
		}
		if ev.Contexts.Custom == nil {
			ev.Contexts.Custom = map[string]*structpb.Struct{}
		}
		for name, v := range s.contexts {
			if st, err := structpb.NewStruct(jsonable(v)); err == nil {
				ev.Contexts.Custom[name] = st
			}
		}
	}
	if s.database != nil {
		if ev.Contexts == nil {
			ev.Contexts = &ingestv1.Contexts{}
		}
		if ev.Contexts.Database == nil {
			ev.Contexts.Database = proto.CloneOf(s.database)
		}
	}
	if len(s.breadcrumbs) > 0 {
		crumbs := make([]*ingestv1.Breadcrumb, 0, len(s.breadcrumbs)+len(ev.Breadcrumbs))
		for _, b := range s.breadcrumbs {
			crumbs = append(crumbs, proto.CloneOf(b))
		}
		ev.Breadcrumbs = append(crumbs, ev.Breadcrumbs...)
	}
	if len(ev.Fingerprint) == 0 && len(s.fingerprint) > 0 {
		ev.Fingerprint = slices.Clone(s.fingerprint)
	}
	if s.level != ingestv1.Level_LEVEL_UNSPECIFIED {
		ev.Level = s.level
	}
	if ev.Transaction == "" {
		ev.Transaction = s.transaction
	}
	if ev.Request == nil && s.request != nil {
		ev.Request = proto.CloneOf(s.request)
	}
}

type scopeKey struct{}

// NewContext returns a context carrying scope.
func NewContext(ctx context.Context, scope *Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// FromContext returns the scope stored in ctx, or nil.
func FromContext(ctx context.Context) *Scope {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(scopeKey{}).(*Scope)
	return s
}

// WithScope derives a request/job scope from the one in ctx (or the global
// scope), lets configure modify it and returns a context carrying it.
func WithScope(ctx context.Context, configure func(*Scope)) context.Context {
	base := FromContext(ctx)
	if base == nil {
		if c := CurrentClient(); c != nil {
			base = c.scope
		} else {
			base = NewScope(100)
		}
	}
	s := base.Clone()
	if configure != nil {
		configure(s)
	}
	return NewContext(ctx, s)
}

// jsonable converts values so that structpb accepts them.
func jsonable(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = jsonValue(v)
	}
	return out
}

func jsonValue(v any) any {
	switch x := v.(type) {
	case nil, bool, string, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		switch n := x.(type) {
		case int:
			return float64(n)
		case int8:
			return float64(n)
		case int16:
			return float64(n)
		case int32:
			return float64(n)
		case int64:
			return float64(n)
		case uint:
			return float64(n)
		case uint8:
			return float64(n)
		case uint16:
			return float64(n)
		case uint32:
			return float64(n)
		case uint64:
			return float64(n)
		case float32:
			return float64(n)
		}
		return x
	case map[string]any:
		return jsonable(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = jsonValue(item)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = item
		}
		return out
	case error:
		return x.Error()
	case time.Time:
		return x.Format(time.RFC3339Nano)
	case interface{ String() string }:
		return x.String()
	default:
		return fmtValue(x)
	}
}
