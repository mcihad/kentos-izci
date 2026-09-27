// Package metrics defines izci's Prometheus metrics and serves them at
// /metrics.
//
// Counters and histograms updated on request paths are package-level and
// shared by the whole process. Values owned by a running application (queue
// depth, connection pool, live subscribers) are read at scrape time through
// the gauges of an Instance. Labels never carry tenant data: every series is
// instance-wide, so a scrape reveals nothing about organizations.
package metrics

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vm "github.com/VictoriaMetrics/metrics"

	"github.com/mcihad/kentos-izci/internal/version"
)

// shared holds the process-wide metrics below.
var shared = vm.NewSet()

func init() {
	// TYPE lines let Prometheus and Grafana tell counters from gauges.
	vm.ExposeMetadata(true)
	info := version.Get()
	shared.NewGauge(fmt.Sprintf(`izci_build_info{version=%q,commit=%q,go_version=%q}`,
		info.Version, info.ShortCommit(), info.GoVersion), func() float64 { return 1 })
	shared.NewGauge("izci_http_requests_in_flight", func() float64 { return float64(inFlight.Load()) })
}

// Ingest: izci_ingest_events_total counts every event an ingest call
// answered, by outcome. "unavailable" means the event could not be queued
// or stored and the client should retry.
var (
	EventsAccepted    = shared.NewCounter(`izci_ingest_events_total{outcome="accepted"}`)
	EventsDuplicate   = shared.NewCounter(`izci_ingest_events_total{outcome="duplicate"}`)
	EventsFiltered    = shared.NewCounter(`izci_ingest_events_total{outcome="filtered"}`)
	EventsRateLimited = shared.NewCounter(`izci_ingest_events_total{outcome="rate_limited"}`)
	EventsInvalid     = shared.NewCounter(`izci_ingest_events_total{outcome="invalid"}`)
	EventsUnavailable = shared.NewCounter(`izci_ingest_events_total{outcome="unavailable"}`)
)

// Pipeline: batches written by the group-commit writers.
var (
	// BatchesStored counts committed batch transactions.
	BatchesStored = shared.NewCounter("izci_ingest_batches_total")
	// EventsStored counts events committed to the database.
	EventsStored = shared.NewCounter("izci_ingest_stored_events_total")
	// EventsFailed counts events that could not be stored after retries.
	EventsFailed = shared.NewCounter("izci_ingest_failed_events_total")
	// QueueFull counts submissions refused because a writer queue was full.
	QueueFull = shared.NewCounter("izci_ingest_queue_full_total")
	// BatchDuration is the time to store one batch, retries included.
	BatchDuration = shared.NewPrometheusHistogramExt("izci_ingest_batch_duration_seconds",
		[]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30})
	// IssuesCreated counts issues opened by new fingerprints.
	IssuesCreated = shared.NewCounter("izci_issues_created_total")
	// IssuesRegressed counts resolved issues that received events again.
	IssuesRegressed = shared.NewCounter("izci_issues_regressed_total")
)

// sentryItemTypes are the envelope item types counted by name; anything
// else is counted as "other" so clients cannot create new series.
var sentryItemTypes = []string{
	"transaction", "session", "sessions", "attachment", "client_report", "log", "profile",
	"profile_chunk", "replay_event", "replay_recording", "replay_video", "span", "statsd", "user_report", "feedback",
}

var sentryDropped = func() map[string]*vm.Counter {
	m := map[string]*vm.Counter{}
	for _, t := range append(sentryItemTypes, "other") {
		m[t] = shared.NewCounter(fmt.Sprintf(`izci_sentry_items_dropped_total{type=%q}`, t))
	}
	return m
}()

// SentryItemDropped counts a Sentry envelope item izci does not process
// (performance data, sessions, attachments, …).
func SentryItemDropped(itemType string) {
	c := sentryDropped[itemType]
	if c == nil {
		c = sentryDropped["other"]
	}
	c.Inc()
}

// Cron monitoring: check-ins by outcome, runs izci records itself, and the
// issues raised and resolved for monitors.
var (
	CheckInsAccepted    = shared.NewCounter(`izci_monitor_checkins_total{outcome="accepted"}`)
	CheckInsUnknown     = shared.NewCounter(`izci_monitor_checkins_total{outcome="unknown_monitor"}`)
	CheckInsInvalid     = shared.NewCounter(`izci_monitor_checkins_total{outcome="invalid"}`)
	CheckInsRateLimited = shared.NewCounter(`izci_monitor_checkins_total{outcome="rate_limited"}`)
	// MonitorMissed counts runs that did not check in on time.
	MonitorMissed = shared.NewCounter("izci_monitor_missed_total")
	// MonitorTimeouts counts runs that stayed in progress too long.
	MonitorTimeouts = shared.NewCounter("izci_monitor_timeouts_total")
	// MonitorIncidents counts failure events sent to monitor issues.
	MonitorIncidents = shared.NewCounter("izci_monitor_incident_events_total")
	// MonitorRecoveries counts monitor issues resolved by successful runs.
	MonitorRecoveries = shared.NewCounter("izci_monitor_recoveries_total")
)

// Source maps and debug files: uploads by outcome and stack frames resolved
// with them.
var (
	SymbolUploadsStored    = shared.NewCounter(`izci_symbol_uploads_total{outcome="stored"}`)
	SymbolUploadsDuplicate = shared.NewCounter(`izci_symbol_uploads_total{outcome="duplicate"}`)
	SymbolUploadsRejected  = shared.NewCounter(`izci_symbol_uploads_total{outcome="rejected"}`)

	FramesResolvedSourceMap = shared.NewCounter(`izci_symbolicated_frames_total{kind="sourcemap",outcome="resolved"}`)
	FramesMissingSourceMap  = shared.NewCounter(`izci_symbolicated_frames_total{kind="sourcemap",outcome="missing"}`)
	FramesResolvedNative    = shared.NewCounter(`izci_symbolicated_frames_total{kind="native",outcome="resolved"}`)
	FramesMissingNative     = shared.NewCounter(`izci_symbolicated_frames_total{kind="native",outcome="missing"}`)
)

// Sentry items other than events.
var (
	SessionsAccepted = shared.NewCounter(`izci_sentry_sessions_total{outcome="accepted"}`)
	SessionsInvalid  = shared.NewCounter(`izci_sentry_sessions_total{outcome="invalid"}`)
	SessionsFiltered = shared.NewCounter(`izci_sentry_sessions_total{outcome="filtered"}`)

	AttachmentsStored = shared.NewCounter(`izci_sentry_attachments_total{outcome="stored"}`)
	// AttachmentsDiscarded counts files of projects that discard attachments
	// or of events that were not accepted.
	AttachmentsDiscarded = shared.NewCounter(`izci_sentry_attachments_total{outcome="discarded"}`)
	AttachmentsTooLarge  = shared.NewCounter(`izci_sentry_attachments_total{outcome="too_large"}`)
	AttachmentBytes      = shared.NewCounter("izci_sentry_attachment_bytes_total")

	FeedbackStored    = shared.NewCounter(`izci_sentry_feedback_total{outcome="stored"}`)
	FeedbackDuplicate = shared.NewCounter(`izci_sentry_feedback_total{outcome="duplicate"}`)
	FeedbackRejected  = shared.NewCounter(`izci_sentry_feedback_total{outcome="rejected"}`)
)

var (
	clientReasons = []string{"queue_overflow", "cache_overflow", "buffer_overflow", "ratelimit_backoff", "network_error",
		"sample_rate", "before_send", "event_processor", "send_error", "internal_sdk_error", "insufficient_data",
		"backpressure", "other"}
	clientCategories = []string{"error", "default", "transaction", "span", "session", "attachment", "profile",
		"profile_chunk", "replay", "monitor", "feedback", "log_item", "log_byte", "security", "other"}
)

// ClientDiscarded counts items an SDK reports (client reports) as dropped
// before sending: sampled out, filtered by before_send, lost to a full
// queue …
func ClientDiscarded(reason, category string, n int) {
	if n <= 0 {
		return
	}
	if !slices.Contains(clientReasons, reason) {
		reason = "other"
	}
	if !slices.Contains(clientCategories, category) {
		category = "other"
	}
	// Both labels come from fixed lists, so series are bounded; client
	// reports are rare enough to format the name on each call.
	shared.GetOrCreateCounter(fmt.Sprintf(`izci_sentry_client_discarded_total{reason=%q,category=%q}`, reason, category)).Add(n)
}

// notificationSeries are the channel kinds and outcomes of
// izci_notifications_total: delivered (posted or updated), skipped (the
// channel is muted or gone), retried (scheduled again) or dropped (given up).
var notificationSeries = func() map[[2]string]*vm.Counter {
	out := map[[2]string]*vm.Counter{}
	for _, ch := range []string{"telegram", "email", "slack", "teams", "webhook", "jira", "other"} {
		for _, r := range []string{"delivered", "skipped", "retried", "dropped"} {
			out[[2]string{ch, r}] = shared.NewCounter(fmt.Sprintf(`izci_notifications_total{channel=%q,result=%q}`, ch, r))
		}
	}
	return out
}()

// Notification counts a notification outcome for a channel kind.
func Notification(channel, result string) {
	if c := notificationSeries[[2]string{channel, result}]; c != nil {
		c.Inc()
	}
}

// AI analysis runs.
var (
	aiRuns = map[string]*vm.Counter{
		"succeeded": shared.NewCounter(`izci_ai_runs_total{status="succeeded"}`),
		"failed":    shared.NewCounter(`izci_ai_runs_total{status="failed"}`),
		"cancelled": shared.NewCounter(`izci_ai_runs_total{status="cancelled"}`),
	}
	aiRunDuration = shared.NewPrometheusHistogramExt("izci_ai_run_duration_seconds",
		[]float64{30, 60, 120, 300, 600, 900, 1200, 1800, 3600})
	aiInputTokens  = shared.NewCounter(`izci_ai_tokens_total{kind="input"}`)
	aiOutputTokens = shared.NewCounter(`izci_ai_tokens_total{kind="output"}`)
	aiCost         = shared.NewFloatCounter("izci_ai_cost_usd_total")
)

// AIRunFinished records a finished run: status is "succeeded", "failed" or
// "cancelled"; tokens and cost are what the model reported.
func AIRunFinished(status string, took time.Duration, inputTokens, outputTokens int64, costUSD float64) {
	if c := aiRuns[status]; c != nil {
		c.Inc()
	}
	aiRunDuration.Update(took.Seconds())
	aiInputTokens.AddInt64(max(inputTokens, 0))
	aiOutputTokens.AddInt64(max(outputTokens, 0))
	if costUSD > 0 {
		aiCost.Add(costUSD)
	}
}

// HTTP requests, by handler group and status code. The groups are fixed, so
// request paths cannot create new series.
var (
	inFlight      atomic.Int64
	httpRequests  sync.Map // httpKey → *vm.Counter
	httpDurations = func() map[string]*vm.PrometheusHistogram {
		m := map[string]*vm.PrometheusHistogram{}
		for _, g := range []string{"ingest", "sentry", "api", "mcp", "auth", "docs", "grpc", "health", "metrics",
			"telegram", "integrations", "ui"} {
			m[g] = shared.NewPrometheusHistogram(fmt.Sprintf(`izci_http_request_duration_seconds{handler=%q}`, g))
		}
		return m
	}()
)

type httpKey struct {
	group string
	code  int
}

// RequestStarted counts a request in flight; RequestFinished must follow.
func RequestStarted() { inFlight.Add(1) }

// RequestFinished records a finished request. Streaming calls (Watch…) are
// counted but kept out of the duration histogram, which they would distort.
func RequestFinished(path string, code int, took time.Duration) {
	inFlight.Add(-1)
	g := Group(path)
	key := httpKey{g, code}
	c, ok := httpRequests.Load(key)
	if !ok {
		c, _ = httpRequests.LoadOrStore(key,
			shared.GetOrCreateCounter(fmt.Sprintf(`izci_http_requests_total{handler=%q,code="%d"}`, g, code)))
	}
	c.(*vm.Counter).Inc()
	if h := httpDurations[g]; h != nil {
		h.Update(took.Seconds())
	}
}

// Group classifies a request path into a handler group.
func Group(path string) string {
	rpc := path[strings.LastIndexByte(path, '/')+1:]
	switch {
	case strings.HasPrefix(path, "/izci.ingest.v1."):
		return "ingest"
	case strings.HasPrefix(path, "/izci.api.v1.") || strings.HasPrefix(path, "/grpc."):
		if strings.HasPrefix(rpc, "Watch") {
			return "stream"
		}
		if strings.HasPrefix(path, "/grpc.") {
			return "grpc"
		}
		return "api"
	case strings.HasPrefix(path, "/api/"):
		return "sentry"
	case path == "/mcp" || strings.HasPrefix(path, "/mcp/"):
		return "mcp"
	case strings.HasPrefix(path, "/auth/"):
		return "auth"
	case strings.HasPrefix(path, "/docs/") || strings.HasPrefix(path, "/llms"):
		return "docs"
	case path == "/healthz" || path == "/readyz":
		return "health"
	case path == "/metrics":
		return "metrics"
	case strings.HasPrefix(path, "/telegram/"):
		return "telegram"
	case strings.HasPrefix(path, "/integrations/"):
		return "integrations"
	}
	return "ui"
}

// filteredSeries counts events inbound filters dropped, by reason.
var filteredSeries = func() map[string]*vm.Counter {
	out := map[string]*vm.Counter{}
	for _, r := range []string{"environment", "browser_extension", "web_crawler", "legacy_browser", "localhost",
		"error_message", "ip_address", "release"} {
		out[r] = shared.NewCounter(fmt.Sprintf(`izci_ingest_filtered_total{reason=%q}`, r))
	}
	return out
}()

// EventFiltered counts an event an inbound filter dropped.
func EventFiltered(reason string) {
	if c := filteredSeries[reason]; c != nil {
		c.Inc()
	}
}
