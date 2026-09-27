package sentry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/andybalholm/brotli"
	"github.com/google/uuid"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"

	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/monitor"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

const (
	maxCompressedBody   = 20 << 20
	maxDecompressedBody = 40 << 20
	maxEventsPerRequest = 100
)

// Handler serves the Sentry-compatible endpoints.
type Handler struct {
	svc *ingest.Service
	log *slog.Logger
}

// NewHandler creates the handler.
func NewHandler(svc *ingest.Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Register mounts the envelope and store endpoints (with and without the
// trailing slash Sentry SDKs use).
func (h *Handler) Register(handle func(pattern string, handler http.Handler)) {
	envelope := ingest.CORS(http.HandlerFunc(h.envelope))
	store := ingest.CORS(http.HandlerFunc(h.store))
	handle("/api/{project}/envelope/{$}", envelope)
	handle("/api/{project}/envelope", envelope)
	handle("/api/{project}/store/{$}", store)
	handle("/api/{project}/store", store)
	cron := ingest.CORS(http.HandlerFunc(h.cron))
	handle("/api/{project}/cron/{monitor}/{key}/{$}", cron)
	handle("/api/{project}/cron/{monitor}/{key}", cron)
	handle("/api/{project}/cron/{monitor}/{$}", cron)
	handle("/api/{project}/cron/{monitor}", cron)
}

type item struct {
	typ     string
	header  itemHeader
	payload []byte
}

// itemHeader holds the item header fields izci reads.
type itemHeader struct {
	Type           string `json:"type"`
	Length         *int   `json:"length"`
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"`
	AttachmentType string `json:"attachment_type"`
}

// envelope serves POST /api/{project}/envelope/: errors, attachments, user
// feedback, sessions, cron check-ins and client reports are processed; other
// items (transactions, spans, profiles, replays, logs) are accepted and
// dropped.
func (h *Handler) envelope(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	projectID, ok := parseProject(r.PathValue("project"))
	if !ok {
		writeError(w, http.StatusBadRequest, "project id in the URL must be numeric")
		return
	}
	body, status, err := readBody(w, r)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	header, items, err := parseEnvelope(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	token := authToken(r)
	if token == "" {
		token = dsnKey(anyString(header["dsn"]))
	}
	caller, err := h.svc.AuthenticateForProject(r.Context(), token, projectID, r.Header, r.RemoteAddr)
	if err != nil {
		writeConnectError(w, err)
		return
	}
	drift := clockDrift(header["sent_at"], time.Now())
	id := strings.ReplaceAll(anyString(header["event_id"]), "-", "")

	var (
		events     []*ingestv1.Event
		files      []ingest.AttachmentFile
		feedback   []ingest.FeedbackInput
		sessions   []ingest.SessionUpdate
		aggregates []ingest.SessionAggregate
		checkIns   []monitor.CheckIn
	)
	for _, it := range items {
		switch it.typ {
		case "event":
			ev, err := Decode(it.payload)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			shiftEvent(ev, drift)
			if ev.EventId == "" {
				ev.EventId = id
			}
			events = append(events, ev)
		case "attachment":
			files = append(files, ingest.AttachmentFile{Name: it.header.Filename, ContentType: it.header.ContentType,
				Type: it.header.AttachmentType, Data: it.payload})
		case "feedback":
			fb, err := DecodeFeedback(it.payload, drift)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			feedback = append(feedback, fb)
		case "user_report":
			fb, err := DecodeUserReport(it.payload)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			feedback = append(feedback, fb)
		case "session":
			u, err := DecodeSession(it.payload, drift)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			sessions = append(sessions, u)
		case "sessions":
			a, err := DecodeSessionAggregates(it.payload, drift)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			aggregates = append(aggregates, a...)
		case "check_in":
			in, err := DecodeCheckIn(it.payload, drift)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			checkIns = append(checkIns, in)
		case "client_report":
			recordClientReport(it.payload)
		default:
			// transaction, span, profile(_chunk), replay_*, log, statsd …:
			// other Sentry products than error monitoring.
			metrics.SentryItemDropped(it.typ)
		}
	}
	if len(events) > maxEventsPerRequest {
		writeError(w, http.StatusRequestEntityTooLarge, "too many events in one envelope")
		return
	}

	h.svc.RecordSessions(caller, sessions, aggregates)
	for _, in := range checkIns {
		// An envelope has no per-item answer; a refused check-in (unknown
		// monitor, rate limit) is logged and counted in metrics.
		if _, err := h.svc.AcceptCheckIn(r.Context(), caller, in); err != nil {
			h.log.Debug("check-in refused", slog.String("monitor", in.MonitorSlug), slog.Any("err", err))
		}
	}
	for _, fb := range feedback {
		if err := h.svc.AcceptFeedback(r.Context(), caller, fb); err != nil && !errors.Is(err, ingest.ErrEmptyFeedback) {
			h.log.Warn("store user feedback", slog.Any("err", err))
		}
	}

	res := response{status: http.StatusOK, stored: true}
	if len(events) > 0 {
		res = h.acceptEvents(r, caller, events)
		if id == "" {
			id = res.id
		}
	}
	// Attachments belong to the envelope's event (or feedback). They are
	// kept when that event was stored, or when they arrive on their own for
	// an event sent earlier.
	if len(files) > 0 {
		attachTo, err := uuid.Parse(id)
		if err != nil || !res.stored {
			metrics.AttachmentsDiscarded.Add(len(files))
		} else if _, err := h.svc.StoreAttachments(r.Context(), caller, attachTo, files); err != nil {
			h.log.Error("store attachments", slog.Any("err", err))
		}
	}
	res.write(w, id)
}

// response is the outcome of an ingest request in Sentry's terms.
type response struct {
	status int
	detail string
	header http.Header
	id     string
	// stored reports that the events were stored (or were duplicates of
	// stored ones), so their attachments may be kept.
	stored bool
}

func (res response) write(w http.ResponseWriter, id string) {
	for k, v := range res.header {
		w.Header()[k] = v
	}
	if res.status != http.StatusOK {
		writeError(w, res.status, res.detail)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (h *Handler) store(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	projectID, ok := parseProject(r.PathValue("project"))
	if !ok {
		writeError(w, http.StatusBadRequest, "project id in the URL must be numeric")
		return
	}
	body, status, err := readBody(w, r)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	ev, err := Decode(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	caller, err := h.svc.AuthenticateForProject(r.Context(), authToken(r), projectID, r.Header, r.RemoteAddr)
	if err != nil {
		writeConnectError(w, err)
		return
	}
	res := h.acceptEvents(r, caller, []*ingestv1.Event{ev})
	res.write(w, res.id)
}

// acceptEvents runs events through the ingest service and maps the first
// failure onto Sentry's status codes and rate limit headers.
func (h *Handler) acceptEvents(r *http.Request, caller *ingest.Caller, events []*ingestv1.Event) response {
	results, retry := h.svc.Accept(r.Context(), caller, events)
	res := response{status: http.StatusOK, header: http.Header{}, stored: len(results) > 0}
	if len(results) > 0 {
		res.id = strings.ReplaceAll(results[0].GetEventId(), "-", "")
	}
	for _, er := range results {
		switch er.GetOutcome() {
		case ingestv1.EventOutcome_EVENT_OUTCOME_RATE_LIMITED:
			scope := "project"
			if strings.Contains(er.GetError(), "quota") {
				scope = "organization:usage_exceeded"
			}
			secs := int(retry.Seconds() + 0.999)
			if secs < 1 {
				secs = 1
			}
			res.header.Set("Retry-After", strconv.Itoa(secs))
			res.header.Set("X-Sentry-Rate-Limits", fmt.Sprintf("%d:error;default;attachment:%s", secs, scope))
			res.status, res.detail = http.StatusTooManyRequests, er.GetError()
			return res
		case ingestv1.EventOutcome_EVENT_OUTCOME_INVALID:
			if strings.HasPrefix(er.GetError(), "temporarily unavailable") {
				res.header.Set("Retry-After", "1")
				res.status, res.detail = http.StatusServiceUnavailable, er.GetError()
				return res
			}
			res.status, res.detail = http.StatusBadRequest, er.GetError()
			return res
		case ingestv1.EventOutcome_EVENT_OUTCOME_FILTERED:
			// Accepted from the SDK's point of view; its files are not kept.
			res.stored = false
		}
	}
	return res
}

// readBody reads and decompresses the request body within size limits.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, int, error) {
	raw := http.MaxBytesReader(w, r.Body, maxCompressedBody)
	var reader io.Reader = raw
	switch enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(raw)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid gzip body: %w", err)
		}
		defer zr.Close()
		reader = zr
	case "deflate":
		// "deflate" is zlib-wrapped per RFC 9110, but some clients send
		// raw DEFLATE; try zlib first.
		data, err := io.ReadAll(raw)
		if err != nil {
			return nil, http.StatusRequestEntityTooLarge, errors.New("request body too large")
		}
		if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
			defer zr.Close()
			reader = zr
		} else {
			reader = flate.NewReader(bytes.NewReader(data))
		}
	case "br":
		reader = brotli.NewReader(raw)
	case "zstd":
		zr, err := zstd.NewReader(raw, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxDecompressedBody+1))
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid zstd body: %w", err)
		}
		defer zr.Close()
		reader = zr
	default:
		return nil, http.StatusUnsupportedMediaType, fmt.Errorf("unsupported content encoding %q", enc)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxDecompressedBody+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, http.StatusRequestEntityTooLarge, errors.New("request body too large")
		}
		return nil, http.StatusBadRequest, fmt.Errorf("read body: %w", err)
	}
	if len(data) > maxDecompressedBody {
		return nil, http.StatusRequestEntityTooLarge, errors.New("decompressed body too large")
	}
	return data, 0, nil
}

// parseEnvelope splits a Sentry envelope into its header and items.
func parseEnvelope(data []byte) (map[string]any, []item, error) {
	line, rest := cutLine(data)
	header := map[string]any{}
	if len(bytes.TrimSpace(line)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&header); err != nil {
			return nil, nil, fmt.Errorf("invalid envelope header: %w", err)
		}
	}
	var items []item
	for len(rest) > 0 {
		var hdrLine []byte
		hdrLine, rest = cutLine(rest)
		if len(bytes.TrimSpace(hdrLine)) == 0 {
			continue
		}
		var ih itemHeader
		if err := json.Unmarshal(hdrLine, &ih); err != nil {
			return nil, nil, fmt.Errorf("invalid envelope item header: %w", err)
		}
		var payload []byte
		if ih.Length != nil {
			n := *ih.Length
			if n < 0 || n > len(rest) {
				return nil, nil, fmt.Errorf("envelope item length %d exceeds the remaining %d bytes", n, len(rest))
			}
			payload, rest = rest[:n], rest[n:]
			if len(rest) > 0 && rest[0] == '\n' {
				rest = rest[1:]
			}
		} else {
			payload, rest = cutLine(rest)
		}
		items = append(items, item{typ: ih.Type, header: ih, payload: payload})
	}
	return header, items, nil
}

func cutLine(b []byte) ([]byte, []byte) {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return bytes.TrimSuffix(b[:i], []byte("\r")), b[i+1:]
	}
	return b, nil
}

// authToken finds the public key in X-Sentry-Auth, "Authorization: Sentry
// …", the sentry_key query parameter or a Bearer/X-Izci-Key header.
func authToken(r *http.Request) string {
	for _, h := range []string{r.Header.Get("X-Sentry-Auth"), r.Header.Get("Authorization")} {
		if v := sentryAuthKey(h); v != "" {
			return v
		}
	}
	if v := r.URL.Query().Get("sentry_key"); v != "" {
		return v
	}
	return ingest.KeyFromHeader(r.Header)
}

func sentryAuthKey(h string) string {
	h = strings.TrimSpace(h)
	if len(h) < 7 || !strings.EqualFold(h[:7], "sentry ") {
		return ""
	}
	for _, part := range strings.Split(h[7:], ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(k) == "sentry_key" {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

// DSN renders the Sentry DSN "https://<key>@host/<public id>" of a project
// for an izci instance reachable at publicURL.
func DSN(publicURL, key string, publicID int64) string {
	scheme, rest, ok := strings.Cut(publicURL, "://")
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s://%s@%s/%d", scheme, key, strings.TrimRight(rest, "/"), publicID)
}

// dsnKey extracts the public key from a DSN URL.
func dsnKey(dsn string) string {
	if dsn == "" {
		return ""
	}
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return ""
	}
	return u.User.Username()
}

func parseProject(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

func writeConnectError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch connect.CodeOf(err) {
	case connect.CodeUnauthenticated:
		status = http.StatusUnauthorized
	case connect.CodePermissionDenied:
		status = http.StatusForbidden
	case connect.CodeUnavailable:
		w.Header().Set("Retry-After", "5")
		status = http.StatusServiceUnavailable
	}
	var cerr *connect.Error
	msg := err.Error()
	if errors.As(err, &cerr) {
		msg = cerr.Message()
	}
	writeError(w, status, msg)
}
