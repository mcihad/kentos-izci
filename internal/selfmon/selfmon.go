// Package selfmon makes izci report its own errors to an izci server with
// the Go SDK (IZCI_SELF_URL, IZCI_SELF_KEY): error records of the server log
// become events, info and warning records become their breadcrumbs.
//
// The target may be this very instance. A failure while storing a reported
// error can log another error, so reports are rate limited to keep such a
// loop from feeding itself; the SDK's own diagnostics never reach the
// reporting logger.
package selfmon

import (
	"log/slog"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/version"
	izci "github.com/mcihad/kentos-izci/sdk/go"
	"github.com/mcihad/kentos-izci/sdk/go/izcislog"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// Reports per minute; bursts up to the same number are let through.
const perMinute = 60

// Monitor is running self-monitoring.
type Monitor struct {
	log     *slog.Logger
	dropped atomic.Int64
}

// Start initializes the SDK and returns a monitor whose Logger reports error
// records. It returns nil when self-monitoring is not configured.
func Start(cfg *config.Config, log *slog.Logger) (*Monitor, error) {
	if !cfg.Self.Enabled() {
		return nil, nil
	}
	m := &Monitor{}
	limit := rate.NewLimiter(rate.Every(time.Minute/perMinute), perMinute)
	err := izci.Init(izci.Options{
		Endpoint:    cfg.Self.URL,
		Key:         cfg.Self.Key.Value(),
		Environment: cfg.Self.Environment,
		Release:     "izci@" + version.Get().Version,
		Tags:        map[string]string{"component": "server"},
		// The binary is built with -trimpath and ships without sources.
		DisableContextLines: true,
		BeforeSend: func(ev *ingestv1.Event) *ingestv1.Event {
			if !limit.Allow() {
				m.dropped.Add(1)
				return nil
			}
			return ev
		},
	})
	if err != nil {
		return nil, err
	}
	m.log = slog.New(izcislog.New(log.Handler(), izcislog.Options{}))
	log.Info("self-monitoring enabled", slog.String("url", cfg.Self.URL), slog.String("environment", cfg.Self.Environment))
	return m, nil
}

// Logger returns the reporting logger.
func (m *Monitor) Logger() *slog.Logger { return m.log }

// Close sends queued reports, waiting at most timeout.
func (m *Monitor) Close(timeout time.Duration) {
	if n := m.dropped.Load(); n > 0 {
		m.log.Warn("self-monitoring reports dropped by the rate limit", slog.Int64("count", n))
	}
	izci.Close(timeout)
}
