package metrics

import (
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"

	vm "github.com/VictoriaMetrics/metrics"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Instance holds the gauges of one running application. Tests may build
// several applications in one process, so these are not registered
// globally.
type Instance struct {
	set *vm.Set
}

// NewInstance returns an empty instance.
func NewInstance() *Instance { return &Instance{set: vm.NewSet()} }

// Gauge registers a gauge read at scrape time.
func (i *Instance) Gauge(name string, f func() float64) { i.set.NewGauge(name, f) }

// Pool exports the statistics of a pgx connection pool.
func (i *Instance) Pool(p *pgxpool.Pool) {
	i.set.RegisterMetricsWriter(func(w io.Writer) {
		s := p.Stat()
		vm.WriteMetadataIfNeeded(w, "izci_db_pool_connections", "gauge")
		fmt.Fprintf(w, "izci_db_pool_connections{state=\"acquired\"} %d\n", s.AcquiredConns())
		fmt.Fprintf(w, "izci_db_pool_connections{state=\"idle\"} %d\n", s.IdleConns())
		fmt.Fprintf(w, "izci_db_pool_connections{state=\"constructing\"} %d\n", s.ConstructingConns())
		vm.WriteGaugeUint64(w, "izci_db_pool_max_connections", uint64(max(s.MaxConns(), 0)))
		vm.WriteCounterUint64(w, "izci_db_pool_acquires_total", uint64(max(s.AcquireCount(), 0)))
		// Acquires that found no idle connection and had to wait or dial.
		vm.WriteCounterUint64(w, "izci_db_pool_empty_acquires_total", uint64(max(s.EmptyAcquireCount(), 0)))
		vm.WriteCounterFloat64(w, "izci_db_pool_empty_acquire_wait_seconds_total", s.EmptyAcquireWaitTime().Seconds())
		vm.WriteCounterUint64(w, "izci_db_pool_canceled_acquires_total", uint64(max(s.CanceledAcquireCount(), 0)))
	})
}

// Handler serves the metrics in the Prometheus text format: izci's own,
// this instance's gauges and the Go runtime and process metrics.
//
// With a token, every request must carry "Authorization: Bearer <token>".
// Without one, only direct requests from loopback and private addresses
// are answered: a request relayed by a proxy (X-Forwarded-For, Forwarded,
// X-Real-Ip) is refused, so a public reverse proxy does not expose the
// endpoint by accident.
func (i *Instance) Handler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case token != "":
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="izci metrics"`)
				http.Error(w, "metrics need a bearer token", http.StatusUnauthorized)
				return
			}
		case !localRequest(r):
			http.Error(w, "metrics are served to local networks only; set METRICS_TOKEN to scrape from elsewhere",
				http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		shared.WritePrometheus(w)
		i.set.WritePrometheus(w)
		vm.WriteProcessMetrics(w)
	})
}

// localRequest reports whether r came straight from a loopback or private
// address.
func localRequest(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	addr := ap.Addr().Unmap()
	return addr.IsLoopback() || addr.IsPrivate()
}
