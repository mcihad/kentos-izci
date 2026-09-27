package sentry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/monitor"
)

// checkInPayload is a Sentry check_in envelope item.
type checkInPayload struct {
	CheckInID     string                     `json:"check_in_id"`
	MonitorSlug   string                     `json:"monitor_slug"`
	Status        string                     `json:"status"`
	Duration      *float64                   `json:"duration"`
	Release       string                     `json:"release"`
	Environment   string                     `json:"environment"`
	Contexts      map[string]json.RawMessage `json:"contexts"`
	MonitorConfig *monitorConfigPayload      `json:"monitor_config"`
}

// monitorConfigPayload is Sentry's monitor_config.
type monitorConfigPayload struct {
	Schedule struct {
		Type string `json:"type"`
		// Value is the crontab (a string) or the interval (a number).
		Value json.RawMessage `json:"value"`
		Unit  string          `json:"unit"`
	} `json:"schedule"`
	CheckinMargin         *float64 `json:"checkin_margin"`
	MaxRuntime            *float64 `json:"max_runtime"`
	Timezone              string   `json:"timezone"`
	FailureIssueThreshold *float64 `json:"failure_issue_threshold"`
	RecoveryThreshold     *float64 `json:"recovery_threshold"`
}

// DecodeCheckIn decodes a check_in envelope item.
func DecodeCheckIn(data []byte, drift time.Duration) (monitor.CheckIn, error) {
	var p checkInPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return monitor.CheckIn{}, fmt.Errorf("check-in is not valid JSON: %w", err)
	}
	in := monitor.CheckIn{MonitorSlug: p.MonitorSlug, Status: p.Status, Release: p.Release,
		Environment: p.Environment, Received: time.Now().Add(drift)}
	if p.CheckInID != "" {
		id, err := uuid.Parse(p.CheckInID)
		if err != nil {
			return in, errors.New("check_in_id must be a UUID")
		}
		in.ID = id
	}
	if p.Duration != nil {
		in.Duration = seconds(*p.Duration)
	}
	if raw, ok := p.Contexts["trace"]; ok {
		var trace struct {
			TraceID string `json:"trace_id"`
		}
		if json.Unmarshal(raw, &trace) == nil {
			in.TraceID = trace.TraceID
		}
	}
	if p.MonitorConfig != nil {
		cfg, err := p.MonitorConfig.config()
		if err != nil {
			return in, err
		}
		in.Config = &cfg
	}
	return in, nil
}

func (c *monitorConfigPayload) config() (monitor.Config, error) {
	cfg := monitor.Config{ScheduleType: c.Schedule.Type, Unit: c.Schedule.Unit, Timezone: c.Timezone,
		CheckinMargin: minutes(c.CheckinMargin), MaxRuntime: minutes(c.MaxRuntime),
		FailureThreshold: minutes(c.FailureIssueThreshold), RecoveryThreshold: minutes(c.RecoveryThreshold)}
	value := bytes.TrimSpace(c.Schedule.Value)
	switch strings.ToLower(strings.TrimSpace(c.Schedule.Type)) {
	case monitor.ScheduleCrontab:
		if err := json.Unmarshal(value, &cfg.Crontab); err != nil {
			return cfg, errors.New("monitor_config.schedule.value must be a crontab string")
		}
	case monitor.ScheduleInterval:
		var n float64
		if err := json.Unmarshal(value, &n); err != nil || n != math.Trunc(n) {
			// Some SDKs send the number as a string.
			var s string
			if json.Unmarshal(value, &s) != nil {
				return cfg, errors.New("monitor_config.schedule.value must be a whole number")
			}
			v, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return cfg, errors.New("monitor_config.schedule.value must be a whole number")
			}
			n = float64(v)
		}
		cfg.Interval = int(n)
	}
	return cfg, nil
}

// minutes converts an optional JSON number to an int; fractions round up so
// that 0.5 minutes still means "some margin".
func minutes(v *float64) int {
	if v == nil || *v <= 0 {
		return 0
	}
	return int(math.Min(math.Ceil(*v), 1<<30))
}

// seconds converts a duration in (fractional) seconds.
func seconds(v float64) *time.Duration {
	if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	d := time.Duration(math.Min(v, 1e9) * float64(time.Second))
	return &d
}

// cron serves Sentry's HTTP check-in endpoint for jobs without an SDK:
//
//	curl -X POST 'https://izci.example.com/api/<project>/cron/<monitor>/<key>/?status=in_progress'
//	curl -X POST 'https://izci.example.com/api/<project>/cron/<monitor>/<key>/?status=ok&check_in_id=<id>'
//
// The query takes status (in_progress, ok, error; required), check_in_id,
// environment, release and duration (seconds). A JSON body may carry
// monitor_config to create or update the monitor. The response is 202 with
// the id of the run.
func (h *Handler) cron(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	projectID, ok := parseProject(r.PathValue("project"))
	if !ok {
		writeError(w, http.StatusBadRequest, "project id in the URL must be numeric")
		return
	}
	q := r.URL.Query()
	in := monitor.CheckIn{MonitorSlug: r.PathValue("monitor"), Status: q.Get("status"), Environment: q.Get("environment"),
		Release: q.Get("release"), Received: time.Now()}
	if in.Status == "" {
		writeError(w, http.StatusBadRequest, "the status query parameter is required (in_progress, ok or error)")
		return
	}
	if raw := q.Get("check_in_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "check_in_id must be a UUID")
			return
		}
		in.ID = id
	}
	if raw := q.Get("duration"); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "duration must be a number of seconds")
			return
		}
		in.Duration = seconds(v)
	}
	if r.Method == http.MethodPost && r.ContentLength != 0 {
		body, status, err := readBody(w, r)
		if err != nil {
			writeError(w, status, err.Error())
			return
		}
		if len(bytes.TrimSpace(body)) > 0 {
			var payload struct {
				MonitorConfig *monitorConfigPayload `json:"monitor_config"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				writeError(w, http.StatusBadRequest, "body must be JSON with an optional monitor_config")
				return
			}
			if payload.MonitorConfig != nil {
				cfg, err := payload.MonitorConfig.config()
				if err != nil {
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				in.Config = &cfg
			}
		}
	}
	token := r.PathValue("key")
	if token == "" {
		token = authToken(r)
	}
	caller, err := h.svc.AuthenticateForProject(r.Context(), token, projectID, r.Header, r.RemoteAddr)
	if err != nil {
		writeConnectError(w, err)
		return
	}
	id, err := h.svc.AcceptCheckIn(r.Context(), caller, in)
	if err != nil {
		writeCheckInError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": strings.ReplaceAll(id.String(), "-", "")})
}

func writeCheckInError(w http.ResponseWriter, err error) {
	var invalid *monitor.InvalidError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, monitor.ErrUnknownMonitor):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ingest.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, err.Error())
	default:
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "check-in could not be stored, try again")
	}
}
