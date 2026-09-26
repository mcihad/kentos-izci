// Package izcihttp reports panics (and optionally 5xx responses) of
// net/http handlers to izci, with the request attached to every event
// captured while the request is handled.
//
//	mux := http.NewServeMux()
//	…
//	http.ListenAndServe(":8080", izcihttp.New(izcihttp.Options{}).Handle(mux))
package izcihttp

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	izci "github.com/mcihad/kentos-izci/sdk/go"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Options configure the middleware.
type Options struct {
	// Repanic re-raises panics after reporting them (for outer recovery
	// middleware). By default the middleware answers 500 itself.
	Repanic bool
	// CaptureServerErrors also reports responses with status >= 500 that
	// did not panic.
	CaptureServerErrors bool
	// WaitForDelivery flushes the event before responding to a panicking
	// request (useful for serverless platforms).
	WaitForDelivery bool
}

// Handler is the middleware.
type Handler struct{ opts Options }

// New creates the middleware.
func New(opts Options) *Handler { return &Handler{opts: opts} }

// sensitiveHeaders are never sent (izci scrubs them too).
var sensitiveHeaders = map[string]bool{
	"authorization": true, "cookie": true, "set-cookie": true, "proxy-authorization": true,
	"x-api-key": true, "x-izci-key": true,
}

// Handle wraps next.
func (h *Handler) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := izci.WithScope(r.Context(), func(s *izci.Scope) {
			s.SetRequest(Request(r))
			s.AddBreadcrumb(izci.Breadcrumb{Type: "http", Category: "http.server", Level: izci.LevelInfo,
				Message: r.Method + " " + r.URL.Path})
		})
		r = r.WithContext(ctx)
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				if c := izci.CurrentClient(); c != nil {
					// The mux sets r.Pattern while routing: refresh the
					// request now that the route is known.
					scope := izci.FromContext(ctx)
					scope.SetRequest(Request(r))
					if pattern := r.Pattern; pattern != "" {
						scope.SetTransaction(pattern)
					}
					c.CapturePanic(ctx, v)
					if h.opts.WaitForDelivery {
						c.Flush(2 * time.Second)
					}
				}
				if h.opts.Repanic {
					panic(v)
				}
				if !rec.wrote {
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(rec, r)
		if h.opts.CaptureServerErrors && rec.status >= 500 {
			scope := izci.FromContext(ctx)
			scope.SetRequest(Request(r))
			if r.Pattern != "" {
				scope.SetTransaction(r.Pattern)
			}
			izci.CaptureMessageContext(ctx, izci.LevelError,
				fmt.Sprintf("HTTP %d %s", rec.status, http.StatusText(rec.status)),
				izci.WithTag("http.status_code", fmt.Sprint(rec.status)))
		}
	})
}

// Request converts an *http.Request into the izci request model (without
// credentials and body).
func Request(r *http.Request) *ingestv1.Request {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	out := &ingestv1.Request{
		Method:      r.Method,
		Url:         scheme + "://" + host + r.URL.Path,
		Route:       r.Pattern,
		QueryString: r.URL.RawQuery,
		Protocol:    r.Proto,
		BodySize:    r.ContentLength,
	}
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		out.RemoteAddr = h
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		out.RemoteAddr = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	for name, values := range r.Header {
		if sensitiveHeaders[strings.ToLower(name)] {
			continue
		}
		for _, v := range values {
			out.Headers = append(out.Headers, &ingestv1.Header{Name: name, Value: v})
		}
	}
	return out
}

type recorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.status, r.wrote = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
