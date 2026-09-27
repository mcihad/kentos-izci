package izci

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// CheckInStatus is what a check-in reports about a scheduled job.
type CheckInStatus int

// Check-in statuses.
const (
	// CheckInInProgress: the job started.
	CheckInInProgress CheckInStatus = iota + 1
	// CheckInOK: the job finished successfully.
	CheckInOK
	// CheckInError: the job failed.
	CheckInError
)

// IntervalUnit is the unit of an interval schedule.
type IntervalUnit int

// Interval units.
const (
	Minute IntervalUnit = iota + 1
	Hour
	Day
	Week
	Month
	Year
)

// CheckIn reports a run of a scheduled job to a cron monitor.
type CheckIn struct {
	// ID of the run; empty lets the server assign one. The id returned for
	// an in-progress check-in finishes the run when sent with OK or Error.
	ID string
	// MonitorSlug names the monitor within the project, e.g. "daily-billing".
	MonitorSlug string
	Status      CheckInStatus
	// Duration of the run; measured by the server when zero.
	Duration time.Duration
	// Environment and Release default to the client's options.
	Environment string
	Release     string
}

// MonitorConfig creates or updates the monitor with a check-in. Set either
// Crontab or Interval with Unit.
type MonitorConfig struct {
	// Crontab expression, e.g. "0 3 * * *" (03:00 every day).
	Crontab string
	// Interval between runs in Units, e.g. 2 Hour.
	Interval uint32
	Unit     IntervalUnit
	// Timezone the crontab is read in, e.g. "Europe/Istanbul" (default UTC).
	Timezone string
	// CheckinMargin a run may be late (default 1 minute).
	CheckinMargin time.Duration
	// MaxRuntime a run may take (default 30 minutes).
	MaxRuntime time.Duration
	// FailureThreshold failed runs in a row open an issue (default 1).
	FailureThreshold uint32
	// RecoveryThreshold successful runs in a row resolve it (default 1).
	RecoveryThreshold uint32
}

// CaptureCheckIn sends a check-in right away (not through the event queue)
// and returns the id of the run.
func (c *Client) CaptureCheckIn(ctx context.Context, in CheckIn, cfg *MonitorConfig) (string, error) {
	if in.MonitorSlug == "" {
		return "", errors.New("izci: CheckIn.MonitorSlug is required")
	}
	p := &ingestv1.CheckIn{
		CheckInId:   in.ID,
		MonitorSlug: in.MonitorSlug,
		Status:      ingestv1.CheckInStatus(in.Status),
		Environment: in.Environment,
		Release:     in.Release,
	}
	if p.Environment == "" {
		p.Environment = c.opts.Environment
	}
	if p.Release == "" {
		p.Release = c.opts.Release
	}
	if in.Duration > 0 {
		p.Duration = durationpb.New(in.Duration)
	}
	if cfg != nil {
		mc := &ingestv1.MonitorConfig{
			Timezone:              cfg.Timezone,
			CheckinMarginMinutes:  minutesOf(cfg.CheckinMargin),
			MaxRuntimeMinutes:     minutesOf(cfg.MaxRuntime),
			FailureIssueThreshold: cfg.FailureThreshold,
			RecoveryThreshold:     cfg.RecoveryThreshold,
		}
		if cfg.Crontab != "" {
			mc.Schedule = &ingestv1.MonitorConfig_Crontab{Crontab: cfg.Crontab}
		} else {
			mc.Schedule = &ingestv1.MonitorConfig_Interval{Interval: &ingestv1.MonitorInterval{
				Value: cfg.Interval, Unit: ingestv1.IntervalUnit(cfg.Unit)}}
		}
		p.MonitorConfig = mc
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req := connect.NewRequest(&ingestv1.SendCheckInRequest{CheckIn: p})
	req.Header().Set("Authorization", "Bearer "+c.transport.key)
	req.Header().Set("User-Agent", "izci-go/"+Version)
	res, err := c.transport.client.SendCheckIn(ctx, req)
	if err != nil {
		return "", fmt.Errorf("izci: check-in for %q: %w", in.MonitorSlug, err)
	}
	return res.Msg.GetCheckInId(), nil
}

// WithMonitor runs job as a run of the monitor: it checks in when the job
// starts and reports OK or Error (with the duration) when it returns. The
// job's error is returned unchanged; check-in failures are only logged.
//
//	err := client.WithMonitor(ctx, "daily-billing", &izci.MonitorConfig{Crontab: "0 3 * * *",
//	    Timezone: "Europe/Istanbul"}, runBilling)
func (c *Client) WithMonitor(ctx context.Context, slug string, cfg *MonitorConfig, job func(context.Context) error) error {
	start := time.Now()
	id, err := c.CaptureCheckIn(ctx, CheckIn{MonitorSlug: slug, Status: CheckInInProgress}, cfg)
	if err != nil {
		c.transport.debug("izci: in-progress check-in failed", "err", err)
	}
	jobErr := job(ctx)
	status := CheckInOK
	if jobErr != nil {
		status = CheckInError
	}
	// The closing check-in must not be skipped because the job's context was
	// cancelled.
	if _, err := c.CaptureCheckIn(context.WithoutCancel(ctx), CheckIn{ID: id, MonitorSlug: slug, Status: status,
		Duration: time.Since(start)}, cfg); err != nil {
		c.transport.debug("izci: closing check-in failed", "err", err)
	}
	return jobErr
}

// CaptureCheckIn sends a check-in with the global client.
func CaptureCheckIn(ctx context.Context, in CheckIn, cfg *MonitorConfig) (string, error) {
	c := current.Load()
	if c == nil {
		return "", errors.New("izci: Init has not been called")
	}
	return c.CaptureCheckIn(ctx, in, cfg)
}

// WithMonitor runs job as a monitored run with the global client; without
// Init it just runs the job.
func WithMonitor(ctx context.Context, slug string, cfg *MonitorConfig, job func(context.Context) error) error {
	c := current.Load()
	if c == nil {
		return job(ctx)
	}
	return c.WithMonitor(ctx, slug, cfg, job)
}

func minutesOf(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	return uint32((d + time.Minute - 1) / time.Minute)
}
