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
	"sync/atomic"

	"connectrpc.com/connect"
	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"

	"github.com/mcihad/kentos-izci/internal/ingest"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

const (
	maxCompressedBody   = 20 << 20
	maxDecompressedBody = 40 << 20
	maxEventsPerRequest = 100
)

// Stats counts envelope items for metrics.
type Stats struct {
	Events  atomic.Int64
	Dropped atomic.Int64
}

// Handler serves the Sentry-compatible endpoints.
type Handler struct {
	svc   *ingest.Service
	log   *slog.Logger
	stats Stats
}

// NewHandler creates the handler.
func NewHandler(svc *ingest.Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Stats exposes counters.
func (h *Handler) Stats() *Stats { return &h.stats }

// Register mounts the envelope and store endpoints (with and without the
// trailing slash Sentry SDKs use).
func (h *Handler) Register(handle func(pattern string, handler http.Handler)) {
	envelope := ingest.CORS(http.HandlerFunc(h.envelope))
	store := ingest.CORS(http.HandlerFunc(h.store))
	handle("/api/{project}/envelope/{$}", envelope)
	handle("/api/{project}/envelope", envelope)
	handle("/api/{project}/store/{$}", store)
	handle("/api/{project}/store", store)
}

type item struct {
	typ     string
	payload []byte
}

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

	var events []*ingestv1.Event
	for _, it := range items {
		switch it.typ {
		case "event":
			ev, err := Decode(it.payload)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			events = append(events, ev)
		default:
			// transaction, session(s), attachment, client_report, check_in,
			// log, profile, replay_*, user_report, feedback, span, statsd …
			h.stats.Dropped.Add(1)
		}
	}
	if len(events) > maxEventsPerRequest {
		writeError(w, http.StatusRequestEntityTooLarge, "too many events in one envelope")
		return
	}
	caller, err := h.svc.AuthenticateForProject(r.Context(), token, projectID, r.Header, r.RemoteAddr)
	if err != nil {
		writeConnectError(w, err)
		return
	}
	id := strings.ReplaceAll(anyString(header["event_id"]), "-", "")
	if len(events) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
		return
	}
	h.accept(w, r, caller, events, id)
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
	h.accept(w, r, caller, []*ingestv1.Event{ev}, "")
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request, caller *ingest.Caller, events []*ingestv1.Event, id string) {
	results, retry := h.svc.Accept(r.Context(), caller, events)
	h.stats.Events.Add(int64(len(events)))
	for _, res := range results {
		switch res.GetOutcome() {
		case ingestv1.EventOutcome_EVENT_OUTCOME_RATE_LIMITED:
			scope := "project"
			if strings.Contains(res.GetError(), "quota") {
				scope = "organization:usage_exceeded"
			}
			secs := int(retry.Seconds() + 0.999)
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			w.Header().Set("X-Sentry-Rate-Limits", fmt.Sprintf("%d:error;default:%s", secs, scope))
			writeError(w, http.StatusTooManyRequests, res.GetError())
			return
		case ingestv1.EventOutcome_EVENT_OUTCOME_INVALID:
			if strings.HasPrefix(res.GetError(), "temporarily unavailable") {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusServiceUnavailable, res.GetError())
				return
			}
			writeError(w, http.StatusBadRequest, res.GetError())
			return
		}
	}
	if id == "" && len(results) > 0 {
		id = strings.ReplaceAll(results[0].GetEventId(), "-", "")
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
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
		var ih struct {
			Type   string `json:"type"`
			Length *int   `json:"length"`
		}
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
		items = append(items, item{typ: ih.Type, payload: payload})
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
