package izci

import (
	"context"
	"sync/atomic"
	"time"
)

var current atomic.Pointer[Client]

// Init creates the global client used by the package-level functions.
//
//	if err := izci.Init(izci.Options{
//	    Endpoint:    "https://izci.example.com",
//	    Key:         os.Getenv("IZCI_KEY"),
//	    Release:     version,
//	    Environment: "production",
//	}); err != nil {
//	    log.Fatal(err)
//	}
//	defer izci.Close(2 * time.Second)
func Init(opts Options) error {
	c, err := NewClient(opts)
	if err != nil {
		return err
	}
	if prev := current.Swap(c); prev != nil {
		prev.Close(time.Second)
	}
	return nil
}

// CurrentClient returns the global client, or nil before Init.
func CurrentClient() *Client { return current.Load() }

// Flush waits for queued events to be delivered.
func Flush(timeout time.Duration) bool {
	if c := current.Load(); c != nil {
		return c.Flush(timeout)
	}
	return true
}

// Close flushes and stops the global client.
func Close(timeout time.Duration) bool {
	if c := current.Swap(nil); c != nil {
		return c.Close(timeout)
	}
	return true
}

// ConfigureScope modifies the global scope.
func ConfigureScope(f func(*Scope)) {
	if c := current.Load(); c != nil {
		f(c.scope)
	}
}

// CaptureError reports err using the global scope.
func CaptureError(err error, opts ...EventOption) string {
	return CaptureErrorContext(context.Background(), err, opts...)
}

// CaptureErrorContext reports err using the scope in ctx (falling back to
// the global scope).
func CaptureErrorContext(ctx context.Context, err error, opts ...EventOption) string {
	c := current.Load()
	if c == nil || err == nil {
		return ""
	}
	ev := c.errorEvent(err, true, "generic", 1)
	return c.CaptureEvent(ctx, ev, opts...)
}

// CaptureMessage reports a message at info level.
func CaptureMessage(msg string, opts ...EventOption) string {
	return CaptureMessageContext(context.Background(), LevelInfo, msg, opts...)
}

// CaptureMessageContext reports a message with a level, using the scope in
// ctx.
func CaptureMessageContext(ctx context.Context, level Level, msg string, opts ...EventOption) string {
	if c := current.Load(); c != nil {
		return c.CaptureMessage(ctx, level, msg, opts...)
	}
	return ""
}

// Recover captures a panic and stops it. Use it as `defer izci.Recover()`
// in goroutines whose failure must not crash the process.
func Recover() {
	if r := recover(); r != nil {
		if c := current.Load(); c != nil {
			c.CapturePanic(context.Background(), r)
		}
	}
}

// RecoverRepanic captures a panic, waits up to two seconds for delivery
// and panics again. Use it as `defer izci.RecoverRepanic()` at the top of
// main so crashes are reported before the process exits.
func RecoverRepanic() {
	if r := recover(); r != nil {
		if c := current.Load(); c != nil {
			c.CapturePanic(context.Background(), r)
			c.Flush(2 * time.Second)
		}
		panic(r)
	}
}

// AddBreadcrumb records a breadcrumb in the scope of ctx (or the global
// scope).
func AddBreadcrumb(ctx context.Context, b Breadcrumb) {
	if s := FromContext(ctx); s != nil {
		s.AddBreadcrumb(b)
		return
	}
	if c := current.Load(); c != nil {
		c.scope.AddBreadcrumb(b)
	}
}
