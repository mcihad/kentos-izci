package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/mcihad/kentos-izci/internal/metrics"
)

type ctxKey int

const requestIDKey ctxKey = iota

// RequestID returns the request id stored by the middleware, if any.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// middleware wraps the mux with panic recovery, request ids, metrics and
// access logs.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		metrics.RequestStarted()

		id := r.Header.Get("X-Request-Id")
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		r = r.WithContext(ctx)

		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			rec := recover()
			status := sw.status
			if rec != nil && !sw.wroteHeader {
				status = http.StatusInternalServerError
			}
			if status == 0 {
				status = http.StatusOK
			}
			metrics.RequestFinished(r.URL.Path, status, time.Since(start))
			if rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("panic while serving request",
					slog.Any("err", &PanicError{Value: rec, pcs: panicStack()}),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("request_id", id),
					slog.String("stack", string(debug.Stack())))
				if !sw.wroteHeader {
					http.Error(sw, "internal server error", http.StatusInternalServerError)
				}
			}
			if skipAccessLog(r.URL.Path) {
				return
			}
			level := slog.LevelDebug
			if status >= 500 {
				level = slog.LevelWarn
			}
			s.log.Log(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("proto", r.Proto),
				slog.Int("status", status),
				slog.Int64("bytes", sw.bytes),
				slog.Duration("took", time.Since(start)),
				slog.String("request_id", id))
		}()
		next.ServeHTTP(sw, r)
	})
}

// PanicError is a panic recovered while serving a request. It keeps the
// stack of the panicking goroutine cut at the panic, so that error reports
// (self-monitoring reads Callers) point at the code that panicked instead
// of at the recovery.
type PanicError struct {
	Value any
	pcs   []uintptr
}

func (e *PanicError) Error() string { return fmt.Sprintf("panic: %v", e.Value) }

// Unwrap returns the panic value when it is an error.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// Callers returns the program counters of the panic, innermost first.
func (e *PanicError) Callers() []uintptr { return e.pcs }

// panicStack captures the stack inside a deferred recover and drops the
// frames of the recovery: everything up to runtime.gopanic and the runtime
// helpers that raised the panic (sigpanic, panicIndex, …).
func panicStack() []uintptr {
	pcs := make([]uintptr, 64)
	pcs = pcs[:runtime.Callers(2, pcs)]
	name := func(pc uintptr) string {
		if f := runtime.FuncForPC(pc - 1); f != nil {
			return f.Name()
		}
		return ""
	}
	for i, pc := range pcs {
		if name(pc) == "runtime.gopanic" {
			pcs = pcs[i+1:]
			for len(pcs) > 1 && strings.HasPrefix(name(pcs[0]), "runtime.") {
				pcs = pcs[1:]
			}
			break
		}
	}
	return pcs
}

func skipAccessLog(path string) bool {
	return path == "/healthz" || path == "/readyz" || path == "/metrics"
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c == '-' || c == '_' || c == '.' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// statusWriter records the status code and body size. It exposes Unwrap so
// http.ResponseController (used by connect-go for streaming and deadlines)
// reaches the underlying writer, and forwards Flush explicitly.
type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// isBrowserNavigation reports whether the request looks like a top-level HTML
// navigation (used to decide on SPA fallback vs. 404).
func isBrowserNavigation(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return accept == "" || strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")
}
