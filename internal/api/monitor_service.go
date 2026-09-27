package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/monitor"
	"github.com/mcihad/kentos-izci/internal/slug"
	"github.com/mcihad/kentos-izci/internal/store"
)

// MonitorServer implements izci.api.v1.MonitorService.
type MonitorServer struct {
	*base
	apiv1connect.UnimplementedMonitorServiceHandler
}

var _ apiv1connect.MonitorServiceHandler = (*MonitorServer)(nil)

// recentCheckIns is how many runs ListMonitors returns per monitor.
const recentCheckIns = 30

// ListMonitors implements MonitorService.ListMonitors.
func (s *MonitorServer) ListMonitors(ctx context.Context, req *connect.Request[apiv1.ListMonitorsRequest]) (*connect.Response[apiv1.ListMonitorsResponse], error) {
	var out apiv1.ListMonitorsResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		f := store.MonitorFilter{OrgID: t.OrgID(), Query: strings.TrimSpace(req.Msg.GetQuery())}
		if slug := req.Msg.GetProject(); slug != "" {
			p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), strings.ToLower(slug))
			if errors.Is(err, store.ErrNotFound) {
				return errNotFound("project")
			}
			if err != nil {
				return err
			}
			f.ProjectID = &p.ID
		}
		monitors, err := store.ListMonitors(ctx, tx, f)
		if err != nil || len(monitors) == 0 {
			return err
		}
		ids := make([]uuid.UUID, len(monitors))
		for i := range monitors {
			ids[i] = monitors[i].ID
		}
		envs, err := store.ListMonitorEnvs(ctx, tx, ids)
		if err != nil {
			return err
		}
		recent, err := store.RecentCheckIns(ctx, tx, ids, recentCheckIns)
		if err != nil {
			return err
		}
		for i := range monitors {
			m := &monitors[i]
			out.Monitors = append(out.Monitors, toMonitor(m, envs[m.ID], recent[m.ID]))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list monitors", err)
	}
	return connect.NewResponse(&out), nil
}

// GetMonitor implements MonitorService.GetMonitor.
func (s *MonitorServer) GetMonitor(ctx context.Context, req *connect.Request[apiv1.GetMonitorRequest]) (*connect.Response[apiv1.GetMonitorResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, fieldError("id", "must be a UUID")
	}
	var out apiv1.GetMonitorResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		m, err := s.monitor(ctx, tx, t, id)
		if err != nil {
			return err
		}
		out.Monitor, err = s.withState(ctx, tx, m)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get monitor", err)
	}
	return connect.NewResponse(&out), nil
}

// CreateMonitor implements MonitorService.CreateMonitor.
func (s *MonitorServer) CreateMonitor(ctx context.Context, req *connect.Request[apiv1.CreateMonitorRequest]) (*connect.Response[apiv1.CreateMonitorResponse], error) {
	r := req.Msg
	name := strings.TrimSpace(r.GetName())
	if name == "" {
		return nil, fieldError("name", "a name is required")
	}
	monitorSlug := monitor.NormalizeSlug(r.GetSlug())
	if r.GetSlug() == "" {
		monitorSlug = monitor.NormalizeSlug(slug.Make(name, 50))
	}
	if monitorSlug == "" {
		return nil, fieldError("slug", "the slug needs letters or digits")
	}
	cfg := monitor.Config{Timezone: r.GetTimezone(), CheckinMargin: int(r.GetCheckinMarginMinutes()),
		MaxRuntime: int(r.GetMaxRuntimeMinutes()), FailureThreshold: int(r.GetFailureThreshold()),
		RecoveryThreshold: int(r.GetRecoveryThreshold())}
	setSchedule(&cfg, r.GetSchedule())
	if err := cfg.Normalize(); err != nil {
		return nil, fieldError("schedule", err.Error())
	}
	var out apiv1.CreateMonitorResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), strings.ToLower(r.GetProject()))
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound("project")
		}
		if err != nil {
			return err
		}
		m := &store.Monitor{OrgID: t.OrgID(), ProjectID: p.ID, ProjectSlug: p.Slug, ProjectName: p.Name,
			Slug: monitorSlug, Name: truncateRunes(name, 100)}
		monitor.ApplyConfig(m, cfg)
		if err := store.InsertMonitor(ctx, tx, m); err != nil {
			if store.UniqueViolation(err, "") {
				return apiError(connect.CodeAlreadyExists, apiv1.ErrorReason_ERROR_REASON_SLUG_TAKEN,
					"the project already has a monitor with this slug")
			}
			return err
		}
		if err := s.audit(ctx, tx, "monitor.create", "monitor", m.ID.String(), m.Name, nil); err != nil {
			return err
		}
		out.Monitor = toMonitor(m, nil, nil)
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "create monitor", err)
	}
	return connect.NewResponse(&out), nil
}

// UpdateMonitor implements MonitorService.UpdateMonitor.
func (s *MonitorServer) UpdateMonitor(ctx context.Context, req *connect.Request[apiv1.UpdateMonitorRequest]) (*connect.Response[apiv1.UpdateMonitorResponse], error) {
	r := req.Msg
	id, err := uuid.Parse(r.GetId())
	if err != nil {
		return nil, fieldError("id", "must be a UUID")
	}
	var out apiv1.UpdateMonitorResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		m, err := s.monitor(ctx, tx, t, id)
		if err != nil {
			return err
		}
		before := monitor.ConfigOf(m)
		wasPaused := m.Paused
		cfg := before
		if r.Name != nil {
			if m.Name = truncateRunes(strings.TrimSpace(r.GetName()), 100); m.Name == "" {
				return fieldError("name", "a name is required")
			}
		}
		if r.GetSchedule() != nil {
			setSchedule(&cfg, r.GetSchedule())
		}
		if r.Timezone != nil {
			cfg.Timezone = r.GetTimezone()
		}
		if r.CheckinMarginMinutes != nil {
			cfg.CheckinMargin = int(r.GetCheckinMarginMinutes())
		}
		if r.MaxRuntimeMinutes != nil {
			cfg.MaxRuntime = int(r.GetMaxRuntimeMinutes())
		}
		if r.FailureThreshold != nil {
			cfg.FailureThreshold = int(r.GetFailureThreshold())
		}
		if r.RecoveryThreshold != nil {
			cfg.RecoveryThreshold = int(r.GetRecoveryThreshold())
		}
		if r.Paused != nil {
			m.Paused = r.GetPaused()
		}
		if err := cfg.Normalize(); err != nil {
			return fieldError("schedule", err.Error())
		}
		monitor.ApplyConfig(m, cfg)
		if err := store.UpdateMonitor(ctx, tx, m); err != nil {
			return err
		}
		// A new schedule, or a monitor coming back from a pause, expects its
		// next run from now on rather than at a time already past.
		if monitor.ScheduleChanged(before, cfg) || (wasPaused && !m.Paused) {
			if err := monitor.Reschedule(ctx, tx, m, time.Now()); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, tx, "monitor.update", "monitor", m.ID.String(), m.Name, nil); err != nil {
			return err
		}
		out.Monitor, err = s.withState(ctx, tx, m)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update monitor", err)
	}
	return connect.NewResponse(&out), nil
}

// DeleteMonitor implements MonitorService.DeleteMonitor.
func (s *MonitorServer) DeleteMonitor(ctx context.Context, req *connect.Request[apiv1.DeleteMonitorRequest]) (*connect.Response[apiv1.DeleteMonitorResponse], error) {
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, fieldError("id", "must be a UUID")
	}
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		m, err := s.monitor(ctx, tx, t, id)
		if err != nil {
			return err
		}
		if err := store.DeleteMonitor(ctx, tx, t.OrgID(), id); err != nil {
			return err
		}
		return s.audit(ctx, tx, "monitor.delete", "monitor", m.ID.String(), m.Name, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "delete monitor", err)
	}
	return connect.NewResponse(&apiv1.DeleteMonitorResponse{}), nil
}

// ListCheckIns implements MonitorService.ListCheckIns.
func (s *MonitorServer) ListCheckIns(ctx context.Context, req *connect.Request[apiv1.ListCheckInsRequest]) (*connect.Response[apiv1.ListCheckInsResponse], error) {
	id, err := uuid.Parse(req.Msg.GetMonitorId())
	if err != nil {
		return nil, fieldError("monitor_id", "must be a UUID")
	}
	limit := pageSize(req.Msg.GetPageSize())
	f := store.CheckInFilter{MonitorID: id, Environment: strings.ToLower(req.Msg.GetEnvironment()), Limit: limit + 1}
	if before, beforeID, ok := decodeCursor(req.Msg.GetPageToken()); ok {
		f.Before, f.BeforeID = &before, beforeID
	}
	var out apiv1.ListCheckInsResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		if _, err := s.monitor(ctx, tx, t, id); err != nil {
			return err
		}
		rows, err := store.ListCheckIns(ctx, tx, f)
		if err != nil {
			return err
		}
		if len(rows) > limit {
			last := rows[limit-1]
			out.NextPageToken = encodeCursor(last.CreatedAt, last.ID)
			rows = rows[:limit]
		}
		for i := range rows {
			out.CheckIns = append(out.CheckIns, toCheckIn(&rows[i]))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list check-ins", err)
	}
	return connect.NewResponse(&out), nil
}

func (s *MonitorServer) monitor(ctx context.Context, tx pgx.Tx, t *auth.Tenant, id uuid.UUID) (*store.Monitor, error) {
	m, err := store.GetMonitor(ctx, tx, t.OrgID(), id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound("monitor")
	}
	return m, err
}

// withState converts a monitor together with its environments.
func (s *MonitorServer) withState(ctx context.Context, tx pgx.Tx, m *store.Monitor) (*apiv1.Monitor, error) {
	envs, err := store.ListMonitorEnvs(ctx, tx, []uuid.UUID{m.ID})
	if err != nil {
		return nil, err
	}
	return toMonitor(m, envs[m.ID], nil), nil
}

var intervalUnitNames = map[apiv1.MonitorIntervalUnit]string{
	apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_MINUTE: "minute",
	apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_HOUR:   "hour",
	apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_DAY:    "day",
	apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_WEEK:   "week",
	apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_MONTH:  "month",
	apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_YEAR:   "year",
}

func setSchedule(cfg *monitor.Config, s *apiv1.MonitorSchedule) {
	switch k := s.GetKind().(type) {
	case *apiv1.MonitorSchedule_Crontab:
		cfg.ScheduleType, cfg.Crontab = monitor.ScheduleCrontab, k.Crontab
	case *apiv1.MonitorSchedule_Interval:
		cfg.ScheduleType, cfg.Interval = monitor.ScheduleInterval, int(k.Interval.GetValue())
		cfg.Unit = intervalUnitNames[k.Interval.GetUnit()]
	default:
		cfg.ScheduleType = ""
	}
}

var monitorStatuses = map[string]apiv1.MonitorStatus{
	monitor.StatusActive:  apiv1.MonitorStatus_MONITOR_STATUS_WAITING,
	monitor.StatusOK:      apiv1.MonitorStatus_MONITOR_STATUS_OK,
	monitor.StatusError:   apiv1.MonitorStatus_MONITOR_STATUS_ERROR,
	monitor.StatusMissed:  apiv1.MonitorStatus_MONITOR_STATUS_MISSED,
	monitor.StatusTimeout: apiv1.MonitorStatus_MONITOR_STATUS_TIMEOUT,
}

var checkInStatuses = map[string]apiv1.CheckInStatus{
	monitor.StatusInProgress: apiv1.CheckInStatus_CHECK_IN_STATUS_IN_PROGRESS,
	monitor.StatusOK:         apiv1.CheckInStatus_CHECK_IN_STATUS_OK,
	monitor.StatusError:      apiv1.CheckInStatus_CHECK_IN_STATUS_ERROR,
	monitor.StatusMissed:     apiv1.CheckInStatus_CHECK_IN_STATUS_MISSED,
	monitor.StatusTimeout:    apiv1.CheckInStatus_CHECK_IN_STATUS_TIMEOUT,
}

func toMonitor(m *store.Monitor, envs []store.MonitorEnv, recent []store.CheckIn) *apiv1.Monitor {
	out := &apiv1.Monitor{
		Id:                   m.ID.String(),
		Project:              &apiv1.ProjectRef{Id: m.ProjectID.String(), Slug: m.ProjectSlug, Name: m.ProjectName},
		Slug:                 m.Slug,
		Name:                 m.Name,
		Schedule:             &apiv1.MonitorSchedule{},
		Timezone:             m.Timezone,
		CheckinMarginMinutes: uint32(m.CheckinMargin),
		MaxRuntimeMinutes:    uint32(m.MaxRuntime),
		FailureThreshold:     uint32(m.FailureThreshold),
		RecoveryThreshold:    uint32(m.RecoveryThreshold),
		Paused:               m.Paused,
		CreatedAt:            ts(m.CreatedAt),
		UpdatedAt:            ts(m.UpdatedAt),
	}
	if m.ScheduleType == monitor.ScheduleInterval {
		unit := apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_UNSPECIFIED
		for k, v := range intervalUnitNames {
			if v == m.Unit {
				unit = k
			}
		}
		out.Schedule.Kind = &apiv1.MonitorSchedule_Interval{Interval: &apiv1.MonitorInterval{Value: uint32(m.Interval), Unit: unit}}
	} else {
		out.Schedule.Kind = &apiv1.MonitorSchedule_Crontab{Crontab: m.Crontab}
	}
	for i := range envs {
		e := &envs[i]
		pe := &apiv1.MonitorEnvironment{
			Environment:       e.Environment,
			Status:            monitorStatuses[e.Status],
			LastCheckInAt:     tsp(e.LastCheckinAt),
			NextCheckInAt:     tsp(e.NextCheckinAt),
			NextCheckInLatest: tsp(e.NextCheckinLatest),
			Failures:          uint32(e.Failures),
		}
		if e.IssueID != nil {
			pe.Issue = &apiv1.IssueRef{Id: e.IssueID.String(), ShortId: ingest.ShortID(m.ProjectSlug, e.IssueSeq)}
		}
		out.Environments = append(out.Environments, pe)
	}
	for i := range recent {
		out.RecentCheckIns = append(out.RecentCheckIns, toCheckIn(&recent[i]))
	}
	return out
}

func toCheckIn(c *store.CheckIn) *apiv1.CheckIn {
	out := &apiv1.CheckIn{
		Id:          c.ID.String(),
		CheckInId:   c.CheckInID.String(),
		Environment: c.Environment,
		Status:      checkInStatuses[c.Status],
		Release:     c.Release,
		ExpectedAt:  tsp(c.ExpectedAt),
		StartedAt:   ts(c.CreatedAt),
		UpdatedAt:   ts(c.UpdatedAt),
	}
	if c.DurationMS != nil {
		out.Duration = durationpb.New(time.Duration(*c.DurationMS) * time.Millisecond)
	}
	return out
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
