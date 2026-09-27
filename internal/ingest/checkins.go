package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/metrics"
	"github.com/mcihad/kentos-izci/internal/monitor"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// SetMonitors connects cron monitoring; without it check-ins are refused.
func (s *Service) SetMonitors(m *monitor.Service) { s.monitors = m }

// ErrMonitoringOff is returned for check-ins when cron monitoring is not
// wired (in tests).
var ErrMonitoringOff = errors.New("cron monitoring is not available")

// AcceptCheckIn applies the project's rate limit to a check-in and stores
// it; the result is the id of the run.
func (s *Service) AcceptCheckIn(ctx context.Context, c *Caller, in monitor.CheckIn) (uuid.UUID, error) {
	if s.monitors == nil {
		return uuid.Nil, ErrMonitoringOff
	}
	if ok, _ := s.limits.Allow(c.Key, 1); !ok {
		metrics.CheckInsRateLimited.Inc()
		return uuid.Nil, ErrRateLimited
	}
	return s.monitors.Process(ctx, monitor.Target{OrgID: c.Key.Key.OrgID, ProjectID: c.Key.Key.ProjectID}, in)
}

// ErrRateLimited rejects a check-in over the project's rate limit.
var ErrRateLimited = errors.New("rate limit exceeded")

// SendCheckIn implements IngestService.SendCheckIn.
func (s *Service) SendCheckIn(ctx context.Context, req *connect.Request[ingestv1.SendCheckInRequest]) (*connect.Response[ingestv1.SendCheckInResponse], error) {
	c, err := s.auth(ctx, req, apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST)
	if err != nil {
		return nil, err
	}
	in, err := checkInFromProto(req.Msg.GetCheckIn())
	if err != nil {
		metrics.CheckInsInvalid.Inc()
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	id, err := s.AcceptCheckIn(ctx, c, in)
	if err != nil {
		return nil, CheckInError(err)
	}
	return connect.NewResponse(&ingestv1.SendCheckInResponse{CheckInId: id.String()}), nil
}

// CheckInError maps a check-in failure onto a Connect error.
func CheckInError(err error) error {
	var invalid *monitor.InvalidError
	switch {
	case errors.As(err, &invalid):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, monitor.ErrUnknownMonitor):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, ErrRateLimited):
		return retryError(connect.CodeResourceExhausted, err.Error(), time.Minute)
	case errors.Is(err, ErrMonitoringOff):
		return connect.NewError(connect.CodeUnimplemented, err)
	}
	return connect.NewError(connect.CodeUnavailable, fmt.Errorf("%s: %w", unavailablePrefix, err))
}

var checkInStatuses = map[ingestv1.CheckInStatus]string{
	ingestv1.CheckInStatus_CHECK_IN_STATUS_IN_PROGRESS: monitor.StatusInProgress,
	ingestv1.CheckInStatus_CHECK_IN_STATUS_OK:          monitor.StatusOK,
	ingestv1.CheckInStatus_CHECK_IN_STATUS_ERROR:       monitor.StatusError,
}

var intervalUnits = map[ingestv1.IntervalUnit]string{
	ingestv1.IntervalUnit_INTERVAL_UNIT_MINUTE: "minute",
	ingestv1.IntervalUnit_INTERVAL_UNIT_HOUR:   "hour",
	ingestv1.IntervalUnit_INTERVAL_UNIT_DAY:    "day",
	ingestv1.IntervalUnit_INTERVAL_UNIT_WEEK:   "week",
	ingestv1.IntervalUnit_INTERVAL_UNIT_MONTH:  "month",
	ingestv1.IntervalUnit_INTERVAL_UNIT_YEAR:   "year",
}

func checkInFromProto(p *ingestv1.CheckIn) (monitor.CheckIn, error) {
	if p == nil {
		return monitor.CheckIn{}, errors.New("check_in is required")
	}
	in := monitor.CheckIn{MonitorSlug: p.GetMonitorSlug(), Status: checkInStatuses[p.GetStatus()],
		Environment: p.GetEnvironment(), Release: p.GetRelease(), TraceID: p.GetTraceId()}
	if raw := p.GetCheckInId(); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return in, errors.New("check_in_id must be a UUID")
		}
		in.ID = id
	}
	if in.Status == "" {
		return in, errors.New("status is required")
	}
	if d := p.GetDuration(); d != nil {
		v := d.AsDuration()
		in.Duration = &v
	}
	if mc := p.GetMonitorConfig(); mc != nil {
		cfg := monitor.Config{Timezone: mc.GetTimezone(), CheckinMargin: int(mc.GetCheckinMarginMinutes()),
			MaxRuntime: int(mc.GetMaxRuntimeMinutes()), FailureThreshold: int(mc.GetFailureIssueThreshold()),
			RecoveryThreshold: int(mc.GetRecoveryThreshold())}
		switch sched := mc.GetSchedule().(type) {
		case *ingestv1.MonitorConfig_Crontab:
			cfg.ScheduleType, cfg.Crontab = monitor.ScheduleCrontab, sched.Crontab
		case *ingestv1.MonitorConfig_Interval:
			cfg.ScheduleType, cfg.Interval = monitor.ScheduleInterval, int(sched.Interval.GetValue())
			cfg.Unit = intervalUnits[sched.Interval.GetUnit()]
		}
		in.Config = &cfg
	}
	return in, nil
}

// IncidentEmitter turns failed monitor runs into events, so that they are
// grouped into one issue per monitor and environment and alert rules notify
// about them like about any other error.
type IncidentEmitter struct {
	db        *db.DB
	pipeline  *Pipeline
	retention time.Duration
}

// NewIncidentEmitter creates the emitter.
func NewIncidentEmitter(database *db.DB, pipeline *Pipeline, retention time.Duration) *IncidentEmitter {
	return &IncidentEmitter{db: database, pipeline: pipeline, retention: retention}
}

var _ monitor.Emitter = (*IncidentEmitter)(nil)

// incidentReasons are the Turkish explanations shown with the event.
var incidentReasons = map[string]string{
	monitor.StatusError:   "Görev hata ile bitti.",
	monitor.StatusMissed:  "Görev beklenen zamanda çalışmadı.",
	monitor.StatusTimeout: "Görev izin verilen sürede bitmedi.",
}

// EmitIncident stores the event of a failed run and waits for the issue it
// was grouped into.
func (e *IncidentEmitter) EmitIncident(ctx context.Context, inc monitor.Incident) (uuid.UUID, error) {
	var (
		p   *store.Project
		org *store.Organization
	)
	err := e.db.InOrg(ctx, inc.OrgID, func(tx pgx.Tx) error {
		var err error
		if p, err = store.GetProject(ctx, tx, inc.ProjectID); err != nil {
			return err
		}
		org, err = store.GetOrganization(ctx, tx, inc.OrgID)
		return err
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("load project: %w", err)
	}
	processed, err := Normalize(incidentEvent(inc), NormalizeOptions{
		Project: ProjectContext{OrgID: inc.OrgID, OrgSlug: org.Slug, ProjectID: p.ID, ProjectSlug: p.Slug,
			Platform: p.Platform, Settings: p.Settings},
		Retention: e.retention,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("normalize incident event: %w", err)
	}
	chans, err := e.pipeline.Submit(processed)
	if err != nil {
		return uuid.Nil, fmt.Errorf("submit incident event: %w", err)
	}
	select {
	case r := <-chans[0]:
		return r.IssueID, r.Err
	case <-time.After(15 * time.Second):
		return uuid.Nil, errors.New("incident event not stored in time")
	case <-ctx.Done():
		return uuid.Nil, ctx.Err()
	}
}

// incidentEvent builds the event of a failed run. All failures of a monitor
// in an environment share one fingerprint, hence one issue; the message is
// the same for every reason so the issue title stays true, and the reason
// travels in the monitor.status tag and the extra data.
func incidentEvent(inc monitor.Incident) *ingestv1.Event {
	extra := map[string]any{
		"monitor":  inc.MonitorSlug,
		"schedule": inc.Schedule,
		"reason":   inc.Reason,
		"failures": inc.Failures,
		"run":      inc.CheckInID.String(),
		"summary":  incidentReasons[inc.Reason],
	}
	if inc.ExpectedAt != nil {
		extra["expected_at"] = inc.ExpectedAt.UTC().Format(time.RFC3339)
	}
	st, _ := structpb.NewStruct(extra)
	return &ingestv1.Event{
		EventId:     strings.ReplaceAll(uuid.New().String(), "-", ""),
		Timestamp:   timestamppb.New(inc.At),
		Level:       ingestv1.Level_LEVEL_ERROR,
		Platform:    "other",
		Logger:      "izci.cron",
		Transaction: inc.MonitorSlug,
		Environment: inc.Environment,
		Release:     inc.Release,
		Message: &ingestv1.Message{
			Formatted: "Zamanlanmış görev başarısız: " + inc.MonitorName,
		},
		Tags:        map[string]string{"monitor.slug": inc.MonitorSlug, "monitor.status": inc.Reason},
		Extra:       st,
		Fingerprint: []string{"izci-cron", inc.MonitorID.String(), inc.Environment},
	}
}
