// Package izcislog connects log/slog to izci: records at or above
// EventLevel become events (an error attribute becomes the exception),
// records at or above BreadcrumbLevel become breadcrumbs of the current
// scope. Records are always passed on to the wrapped handler.
//
//	logger := slog.New(izcislog.New(slog.NewJSONHandler(os.Stderr, nil), izcislog.Options{}))
package izcislog

import (
	"context"
	"log/slog"

	izci "github.com/mcihad/kentos-izci/sdk/go"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// Options configure the handler.
type Options struct {
	// EventLevel is the minimum level reported as an event (default Error).
	EventLevel slog.Level
	// BreadcrumbLevel is the minimum level kept as a breadcrumb (default
	// Info).
	BreadcrumbLevel slog.Level
}

// Handler is a slog.Handler.
type Handler struct {
	next   slog.Handler
	opts   Options
	attrs  []slog.Attr
	groups []string
}

// New wraps next.
func New(next slog.Handler, opts Options) *Handler {
	if opts.EventLevel == 0 {
		opts.EventLevel = slog.LevelError
	}
	return &Handler{next: next, opts: opts}
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.opts.BreadcrumbLevel || h.next.Enabled(ctx, l)
}

// Handle implements slog.Handler.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= h.opts.EventLevel {
		h.capture(ctx, r)
	} else if r.Level >= h.opts.BreadcrumbLevel {
		data := map[string]any{}
		h.collect(r, data, nil)
		izci.AddBreadcrumb(ctx, izci.Breadcrumb{Type: "default", Category: "log", Level: level(r.Level),
			Message: r.Message, Data: data, Timestamp: r.Time})
	}
	if h.next.Enabled(ctx, r.Level) {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *Handler) capture(ctx context.Context, r slog.Record) {
	c := izci.CurrentClient()
	if c == nil {
		return
	}
	extra := map[string]any{}
	var errAttr error
	h.collect(r, extra, &errAttr)
	opt := func(ev *ingestv1.Event) {
		ev.Logger = "slog"
		if len(extra) > 0 {
			st, _ := structpb.NewStruct(jsonable(extra))
			ev.Extra = st
		}
	}
	if errAttr != nil {
		c.CaptureError(ctx, errAttr, izci.WithLevel(level(r.Level)), opt, func(ev *ingestv1.Event) {
			ev.Message = &ingestv1.Message{Formatted: r.Message}
		})
		return
	}
	c.CaptureMessage(ctx, level(r.Level), r.Message, opt)
}

// collect flattens attributes; the first error value is returned in errOut.
func (h *Handler) collect(r slog.Record, out map[string]any, errOut *error) {
	add := func(a slog.Attr) bool {
		v := a.Value.Resolve()
		if e, ok := v.Any().(error); ok && errOut != nil && *errOut == nil {
			*errOut = e
			return true
		}
		out[a.Key] = v.Any()
		return true
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(add)
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	c.next = h.next.WithAttrs(attrs)
	return &c
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	c := *h
	c.groups = append(append([]string(nil), h.groups...), name)
	c.next = h.next.WithGroup(name)
	return &c
}

func level(l slog.Level) izci.Level {
	switch {
	case l >= slog.LevelError+4:
		return izci.LevelFatal
	case l >= slog.LevelError:
		return izci.LevelError
	case l >= slog.LevelWarn:
		return izci.LevelWarning
	case l >= slog.LevelInfo:
		return izci.LevelInfo
	}
	return izci.LevelDebug
}

func jsonable(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case error:
			out[k] = x.Error()
		case string, bool, float64, nil:
			out[k] = x
		case int:
			out[k] = float64(x)
		case int64:
			out[k] = float64(x)
		case uint64:
			out[k] = float64(x)
		default:
			out[k] = slog.AnyValue(x).String()
		}
	}
	return out
}
