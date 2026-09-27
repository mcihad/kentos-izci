package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	connectcors "connectrpc.com/cors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/clientip"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/monitor"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// ServiceConfig configures the ingest service.
type ServiceConfig struct {
	// Durable makes calls wait until events are committed (default). When
	// false, events are acknowledged once queued.
	Durable        bool
	AckTimeout     time.Duration
	Retention      time.Duration
	TrustedProxies config.PrefixList
	MaxBatch       int
	// MaxAttachmentBytes bounds one stored attachment (default 10 MiB).
	MaxAttachmentBytes int
	// SessionKey keys the hashes of session distinct ids (release health).
	SessionKey []byte
}

// Service implements izci.ingest.v1.IngestService and is shared by the
// Sentry-compatible endpoints.
type Service struct {
	cfg      ServiceConfig
	db       *db.DB
	log      *slog.Logger
	keys     *KeyResolver
	limits   *RateLimiter
	quota    *QuotaTracker
	usage    *UsageRecorder
	dedupe   *Dedupe
	pipeline *Pipeline
	sessions *SessionRecorder
	monitors *monitor.Service
	symbols  Symbolicator
}

// Symbolicator resolves minified and native stack traces with uploaded
// source maps and debug files before events are grouped (internal/symbols).
type Symbolicator interface {
	Symbolicate(ctx context.Context, orgID, projectID uuid.UUID, ev *ingestv1.Event)
}

// SetSymbolicator connects stack trace resolution.
func (s *Service) SetSymbolicator(sym Symbolicator) { s.symbols = sym }

var _ ingestv1connect.IngestServiceHandler = (*Service)(nil)

// NewService wires the ingest service.
func NewService(cfg ServiceConfig, database *db.DB, log *slog.Logger, keys *KeyResolver, limits *RateLimiter,
	quota *QuotaTracker, usage *UsageRecorder, dedupe *Dedupe, pipeline *Pipeline) *Service {
	if cfg.AckTimeout <= 0 {
		cfg.AckTimeout = 15 * time.Second
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = 100
	}
	return &Service{cfg: cfg, db: database, log: log, keys: keys, limits: limits, quota: quota, usage: usage, dedupe: dedupe,
		pipeline: pipeline, sessions: NewSessionRecorder(cfg.SessionKey)}
}

// Keys exposes the key resolver (used by the Sentry endpoints and admin
// invalidation).
func (s *Service) Keys() *KeyResolver { return s.keys }

// Caller is an authenticated ingest client.
type Caller struct {
	Key         *store.ResolvedKey
	TransportIP string
	Origin      string
}

// Authenticate resolves and checks the key of a request.
func (s *Service) Authenticate(ctx context.Context, token string, scope apiv1.ApiKeyScope, header http.Header, peer string) (*Caller, error) {
	return s.authenticate(ctx, func() (*store.ResolvedKey, error) { return s.keys.Resolve(ctx, token) }, scope, header, peer)
}

// AuthenticateForProject is Authenticate restricted to a Sentry public id.
func (s *Service) AuthenticateForProject(ctx context.Context, token string, publicID int64, header http.Header, peer string) (*Caller, error) {
	return s.authenticate(ctx, func() (*store.ResolvedKey, error) { return s.keys.ResolveForProject(ctx, token, publicID) },
		apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST, header, peer)
}

func (s *Service) authenticate(ctx context.Context, resolve func() (*store.ResolvedKey, error), scope apiv1.ApiKeyScope, header http.Header, peer string) (*Caller, error) {
	rk, err := resolve()
	if err != nil {
		if errors.Is(err, ErrUnknownKey) {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		s.log.Error("resolve api key", slog.Any("err", err))
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("cannot verify API key right now"))
	}
	if !rk.Key.HasScope(scope) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("API key lacks the %s scope", strings.TrimPrefix(scope.String(), "API_KEY_SCOPE_")))
	}
	origin := header.Get("Origin")
	if !originAllowed(rk.Key.AllowedOrigins, origin) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("origin %q is not allowed for this API key", origin))
	}
	ip := clientip.Resolve(peer, header, s.cfg.TrustedProxies)
	c := &Caller{Key: rk, Origin: origin}
	if ip.IsValid() {
		c.TransportIP = ip.String()
	}
	return c, nil
}

func (s *Service) auth(ctx context.Context, req connect.AnyRequest, scope apiv1.ApiKeyScope) (*Caller, error) {
	return s.Authenticate(ctx, KeyFromHeader(req.Header()), scope, req.Header(), req.Peer().Addr)
}

// SendEvent implements IngestService.SendEvent.
func (s *Service) SendEvent(ctx context.Context, req *connect.Request[ingestv1.SendEventRequest]) (*connect.Response[ingestv1.SendEventResponse], error) {
	c, err := s.auth(ctx, req, apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetEvent() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event is required"))
	}
	results, retry := s.Accept(ctx, c, []*ingestv1.Event{req.Msg.GetEvent()})
	r := results[0]
	switch r.GetOutcome() {
	case ingestv1.EventOutcome_EVENT_OUTCOME_RATE_LIMITED:
		return nil, retryError(connect.CodeResourceExhausted, r.GetError(), retry)
	case ingestv1.EventOutcome_EVENT_OUTCOME_INVALID:
		if strings.HasPrefix(r.GetError(), unavailablePrefix) {
			return nil, retryError(connect.CodeUnavailable, r.GetError(), time.Second)
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(r.GetError()))
	}
	return connect.NewResponse(&ingestv1.SendEventResponse{Result: r}), nil
}

// SendEvents implements IngestService.SendEvents.
func (s *Service) SendEvents(ctx context.Context, req *connect.Request[ingestv1.SendEventsRequest]) (*connect.Response[ingestv1.SendEventsResponse], error) {
	c, err := s.auth(ctx, req, apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST)
	if err != nil {
		return nil, err
	}
	events := req.Msg.GetEvents()
	if len(events) == 0 || len(events) > s.cfg.MaxBatch {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("a batch must contain 1 to %d events (got %d)", s.cfg.MaxBatch, len(events)))
	}
	results, retry := s.Accept(ctx, c, events)
	resp := connect.NewResponse(&ingestv1.SendEventsResponse{Results: results})
	if retry > 0 {
		resp.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds()+0.999)))
	}
	return resp, nil
}

const unavailablePrefix = "temporarily unavailable"

// Accept runs events through filters, limits, normalization and the
// pipeline, and returns one result per event plus the longest retry delay
// among rate-limited events.
func (s *Service) Accept(ctx context.Context, c *Caller, events []*ingestv1.Event) ([]*ingestv1.EventResult, time.Duration) {
	rk := c.Key
	results := make([]*ingestv1.EventResult, len(events))
	defer countOutcomes(results)
	var retry time.Duration
	project := ProjectContext{
		OrgID: rk.Key.OrgID, OrgSlug: rk.OrgSlug, ProjectID: rk.Key.ProjectID, ProjectSlug: rk.ProjectSlug,
		Settings: rk.ProjectSettings,
	}
	transport := parseAddrLoose(c.TransportIP)
	ignored := rk.ProjectSettings.GetIgnoredEnvironments()

	var pending []*Processed
	var pendingIdx []int
	for i, ev := range events {
		results[i] = &ingestv1.EventResult{EventId: ev.GetEventId()}
		if ev == nil {
			results[i].Outcome, results[i].Error = ingestv1.EventOutcome_EVENT_OUTCOME_INVALID, "event is empty"
			continue
		}
		env := strings.ToLower(strings.TrimSpace(ev.GetEnvironment()))
		if env == "" {
			env = "production"
		}
		if slices.Contains(ignored, env) {
			results[i].Outcome = ingestv1.EventOutcome_EVENT_OUTCOME_FILTERED
			s.usage.Filtered(rk.Key.OrgID, 1)
			continue
		}
		if ok, d := s.limits.Allow(rk, 1); !ok {
			results[i].Outcome, results[i].Error = ingestv1.EventOutcome_EVENT_OUTCOME_RATE_LIMITED, "rate limit exceeded"
			retry = max(retry, d)
			s.usage.RateLimited(rk.Key.OrgID, 1)
			continue
		}
		if !s.quota.Allow(ctx, rk.Key.OrgID, rk.OrgLimits.GetMonthlyEvents(), 1) {
			results[i].Outcome, results[i].Error = ingestv1.EventOutcome_EVENT_OUTCOME_RATE_LIMITED, "monthly event quota exhausted"
			retry = max(retry, untilNextMonth())
			s.usage.RateLimited(rk.Key.OrgID, 1)
			continue
		}
		ResetDerived(ev)
		if s.symbols != nil {
			s.symbols.Symbolicate(ctx, project.OrgID, project.ProjectID, ev)
		}
		p, err := Normalize(ev, NormalizeOptions{Project: project, TransportIP: transport, Retention: s.cfg.Retention})
		if err != nil {
			results[i].Outcome, results[i].Error = ingestv1.EventOutcome_EVENT_OUTCOME_INVALID, err.Error()
			continue
		}
		results[i].EventId = p.EventID.String()
		results[i].Warnings = p.Warnings
		if prev, dup := s.dedupe.Seen(p.ProjectID, p.EventID); dup {
			results[i].Outcome = ingestv1.EventOutcome_EVENT_OUTCOME_DUPLICATE
			fillIssue(results[i], prev)
			continue
		}
		pending = append(pending, p)
		pendingIdx = append(pendingIdx, i)
	}
	if len(pending) == 0 {
		return results, retry
	}

	chans, err := s.pipeline.Submit(pending...)
	if err != nil && chans == nil {
		for _, i := range pendingIdx {
			results[i].Outcome, results[i].Error = ingestv1.EventOutcome_EVENT_OUTCOME_INVALID, unavailablePrefix+": "+err.Error()
		}
		return results, retry
	}

	if !s.cfg.Durable {
		for k, i := range pendingIdx {
			p, ch := pending[k], chans[k]
			select {
			case r := <-ch:
				// Already resolved: the queue was full, or the batch was
				// written in the meantime.
				if r.Err != nil {
					results[i].Outcome, results[i].Error = ingestv1.EventOutcome_EVENT_OUTCOME_INVALID, unavailablePrefix+": "+publicError(r.Err)
					continue
				}
				s.dedupe.Remember(p.ProjectID, p.EventID, r)
				results[i].Outcome = ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED
				fillIssue(results[i], r)
				continue
			default:
			}
			results[i].Outcome = ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED
			go func() {
				if r := <-ch; r.Err == nil {
					s.dedupe.Remember(p.ProjectID, p.EventID, r)
				}
			}()
		}
		return results, retry
	}

	timeout := time.NewTimer(s.cfg.AckTimeout)
	defer timeout.Stop()
	for k, i := range pendingIdx {
		select {
		case r := <-chans[k]:
			if r.Err != nil {
				results[i].Outcome, results[i].Error = ingestv1.EventOutcome_EVENT_OUTCOME_INVALID, unavailablePrefix+": "+publicError(r.Err)
				continue
			}
			s.dedupe.Remember(pending[k].ProjectID, pending[k].EventID, r)
			results[i].Outcome = ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED
			fillIssue(results[i], r)
		case <-timeout.C:
			// The event stays queued and will be stored; report it accepted
			// without issue details rather than inviting a duplicate retry.
			results[i].Outcome = ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED
			results[i].Warnings = append(results[i].Warnings, "acknowledged before storage completed")
		case <-ctx.Done():
			results[i].Outcome = ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED
		}
	}
	return results, retry
}

// countOutcomes adds the answered events to izci_ingest_events_total.
func countOutcomes(results []*ingestv1.EventResult) {
	for _, r := range results {
		switch r.GetOutcome() {
		case ingestv1.EventOutcome_EVENT_OUTCOME_ACCEPTED:
			metrics.EventsAccepted.Inc()
		case ingestv1.EventOutcome_EVENT_OUTCOME_DUPLICATE:
			metrics.EventsDuplicate.Inc()
		case ingestv1.EventOutcome_EVENT_OUTCOME_FILTERED:
			metrics.EventsFiltered.Inc()
		case ingestv1.EventOutcome_EVENT_OUTCOME_RATE_LIMITED:
			metrics.EventsRateLimited.Inc()
		default:
			if strings.HasPrefix(r.GetError(), unavailablePrefix) {
				metrics.EventsUnavailable.Inc()
			} else {
				metrics.EventsInvalid.Inc()
			}
		}
	}
}

func fillIssue(res *ingestv1.EventResult, r Result) {
	if r.IssueID.String() != "00000000-0000-0000-0000-000000000000" {
		res.IssueId = r.IssueID.String()
	}
	res.IssueShortId, res.IssueUrl, res.IssueIsNew, res.IssueRegressed = r.IssueShortID, r.IssueURL, r.IssueNew, r.IssueRegressed
}

func publicError(err error) string {
	if errors.Is(err, ErrOverloaded) {
		return "server busy"
	}
	if errors.Is(err, ErrClosed) {
		return "server shutting down"
	}
	return "storage error"
}

func untilNextMonth() time.Duration {
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return next.Sub(now)
}

// retryError builds an error with a google.rpc.RetryInfo detail.
func retryError(code connect.Code, msg string, retry time.Duration) error {
	err := connect.NewError(code, errors.New(msg))
	if retry > 0 {
		if d, derr := connect.NewErrorDetail(&errdetails.RetryInfo{RetryDelay: durationpb.New(retry)}); derr == nil {
			err.AddDetail(d)
		}
		err.Meta().Set("Retry-After", strconv.Itoa(int(retry.Seconds()+0.999)))
	}
	return err
}

// RegisterRelease implements IngestService.RegisterRelease.
func (s *Service) RegisterRelease(ctx context.Context, req *connect.Request[ingestv1.RegisterReleaseRequest]) (*connect.Response[ingestv1.RegisterReleaseResponse], error) {
	c, err := s.auth(ctx, req, apiv1.ApiKeyScope_API_KEY_SCOPE_RELEASES)
	if err != nil {
		return nil, err
	}
	m := req.Msg
	version := strings.TrimSpace(m.GetVersion())
	if version == "" || len(version) > maxRelease {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("version is required (max 200 characters)"))
	}
	at := time.Now().UTC()
	if m.GetReleasedAt().IsValid() && m.GetReleasedAt().AsTime().Before(at.Add(time.Hour)) {
		at = m.GetReleasedAt().AsTime()
	}
	w := &warnings{}
	in := store.ReleaseInput{
		OrgID: c.Key.Key.OrgID, ProjectID: c.Key.Key.ProjectID, Version: version,
		CommitSHA:  strings.ToLower(w.clean("commit_sha", m.GetCommitSha(), 64)),
		Repository: w.clean("repository", m.GetRepository(), 300), Ref: w.clean("ref", m.GetRef(), maxName),
		URL: w.clean("url", m.GetUrl(), maxURL), Environment: strings.ToLower(w.clean("environment", m.GetEnvironment(), maxShortString)),
		ReleasedAt: at, ReleasedBy: w.clean("released_by", m.GetReleasedBy(), maxName),
	}
	var res ingestv1.RegisterReleaseResponse
	err = s.db.InOrg(ctx, in.OrgID, func(tx pgx.Tx) error {
		id, created, err := store.RegisterRelease(ctx, tx, in)
		res.ReleaseId, res.Created = id.String(), created
		return err
	})
	if err != nil {
		s.log.Error("register release", slog.Any("err", err))
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("cannot store the release right now"))
	}
	return connect.NewResponse(&res), nil
}

// GetClientConfig implements IngestService.GetClientConfig.
func (s *Service) GetClientConfig(ctx context.Context, req *connect.Request[ingestv1.GetClientConfigRequest]) (*connect.Response[ingestv1.GetClientConfigResponse], error) {
	c, err := s.auth(ctx, req, apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST)
	if err != nil {
		return nil, err
	}
	rk := c.Key
	perMinute := rk.Key.RateLimitPerMinute
	if perMinute == 0 {
		perMinute = int32(rk.ProjectSettings.GetRateLimitPerMinute())
	}
	return connect.NewResponse(&ingestv1.GetClientConfigResponse{
		OrganizationSlug:   rk.OrgSlug,
		ProjectSlug:        rk.ProjectSlug,
		ProjectName:        rk.ProjectName,
		SampleRate:         1,
		MaxBreadcrumbs:     maxBreadcrumbs,
		MaxEventBytes:      maxEventBytes,
		MaxBatchSize:       uint32(s.cfg.MaxBatch),
		MaxEventsPerMinute: uint32(max(perMinute, 0)),
		ScrubFields:        rk.ProjectSettings.GetPrivacy().GetScrubFields(),
		CollectIp:          rk.ProjectSettings.GetPrivacy().GetIpStorage() != apiv1.IpStorage_IP_STORAGE_NONE,
		ServerTime:         timestamppb.Now(),
		CacheTtlSeconds:    300,
	}), nil
}

// CORS lets browsers call the ingest endpoints from any origin; the key's
// allowed origins are enforced per request by the service.
func CORS(next http.Handler, extraHeaders ...string) http.Handler {
	allowHeaders := strings.Join(append(append(connectcors.AllowedHeaders(),
		"Authorization", "X-Izci-Key", "Content-Encoding", "X-Sentry-Auth", "Sentry-Trace", "Baggage"), extraHeaders...), ", ")
	exposeHeaders := strings.Join(append(connectcors.ExposedHeaders(), "Retry-After", "X-Sentry-Rate-Limits"), ", ")
	methods := strings.Join(connectcors.AllowedMethods(), ", ")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", exposeHeaders)
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", methods+", OPTIONS")
				h.Set("Access-Control-Allow-Headers", allowHeaders)
				h.Set("Access-Control-Max-Age", "7200")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
