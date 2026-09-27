// Package monitor implements cron monitoring: the schedules of monitored
// jobs, check-in processing, and the sweep that records missed and timed out
// runs and raises (and resolves) issues for failing monitors.
package monitor

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule types.
const (
	ScheduleCrontab  = "crontab"
	ScheduleInterval = "interval"
)

// Interval units, as Sentry names them.
var intervalUnits = []string{"minute", "hour", "day", "week", "month", "year"}

// Config is a monitor's schedule and failure policy (Sentry monitor_config).
type Config struct {
	// ScheduleType is ScheduleCrontab or ScheduleInterval.
	ScheduleType string
	// Crontab is a five-field expression or a descriptor such as "@daily".
	Crontab string
	// Interval and Unit: every Interval Units, e.g. 2 "hour".
	Interval int
	Unit     string
	// Timezone the crontab is read in (IANA name); empty means UTC.
	Timezone string
	// CheckinMargin is how many minutes a run may be late (default 1).
	CheckinMargin int
	// MaxRuntime is how many minutes a run may take (default 30).
	MaxRuntime int
	// FailureThreshold failed runs in a row open an issue (default 1).
	FailureThreshold int
	// RecoveryThreshold successful runs in a row resolve it (default 1).
	RecoveryThreshold int
}

// Limits of the policy fields.
const (
	maxMargin    = 30 * 24 * 60 // 30 days, in minutes
	maxRuntime   = 7 * 24 * 60  // 7 days, in minutes
	maxThreshold = 720
	maxInterval  = 100_000
)

var crontabParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Normalize fills defaults and validates c; the error message names the
// offending field.
func (c *Config) Normalize() error {
	c.ScheduleType = strings.ToLower(strings.TrimSpace(c.ScheduleType))
	c.Crontab = strings.Join(strings.Fields(c.Crontab), " ")
	c.Unit = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.Unit)), "s")
	c.Timezone = strings.TrimSpace(c.Timezone)
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}
	if c.CheckinMargin == 0 {
		c.CheckinMargin = 1
	}
	if c.MaxRuntime == 0 {
		c.MaxRuntime = 30
	}
	if c.FailureThreshold == 0 {
		c.FailureThreshold = 1
	}
	if c.RecoveryThreshold == 0 {
		c.RecoveryThreshold = 1
	}
	switch {
	case c.CheckinMargin < 1 || c.CheckinMargin > maxMargin:
		return fmt.Errorf("checkin_margin must be between 1 and %d minutes", maxMargin)
	case c.MaxRuntime < 1 || c.MaxRuntime > maxRuntime:
		return fmt.Errorf("max_runtime must be between 1 and %d minutes", maxRuntime)
	case c.FailureThreshold < 1 || c.FailureThreshold > maxThreshold:
		return fmt.Errorf("failure_issue_threshold must be between 1 and %d", maxThreshold)
	case c.RecoveryThreshold < 1 || c.RecoveryThreshold > maxThreshold:
		return fmt.Errorf("recovery_threshold must be between 1 and %d", maxThreshold)
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("timezone %q is not a known IANA time zone", c.Timezone)
	}
	switch c.ScheduleType {
	case ScheduleCrontab:
		c.Interval, c.Unit = 0, ""
		if c.Crontab == "" {
			return errors.New("schedule: a crontab expression is required")
		}
		if strings.HasPrefix(c.Crontab, "@every") {
			return errors.New("schedule: use an interval schedule instead of @every")
		}
		if _, err := crontabParser.Parse(c.Crontab); err != nil {
			return fmt.Errorf("schedule: invalid crontab %q: %v", c.Crontab, err)
		}
	case ScheduleInterval:
		c.Crontab = ""
		if c.Interval < 1 || c.Interval > maxInterval {
			return fmt.Errorf("schedule: interval must be between 1 and %d", maxInterval)
		}
		if !validUnit(c.Unit) {
			return fmt.Errorf("schedule: unit must be one of %s", strings.Join(intervalUnits, ", "))
		}
	default:
		return errors.New(`schedule: type must be "crontab" or "interval"`)
	}
	return nil
}

func validUnit(u string) bool {
	for _, v := range intervalUnits {
		if u == v {
			return true
		}
	}
	return false
}

// Schedule computes when runs are due.
type Schedule interface {
	// Next returns the first time a run is due strictly after t.
	Next(t time.Time) time.Time
}

// Schedule builds the schedule of a normalized config.
func (c Config) Schedule() (Schedule, error) {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load time zone %q: %w", c.Timezone, err)
	}
	switch c.ScheduleType {
	case ScheduleCrontab:
		s, err := crontabParser.Parse(c.Crontab)
		if err != nil {
			return nil, fmt.Errorf("parse crontab %q: %w", c.Crontab, err)
		}
		return crontabSchedule{s: s, loc: loc}, nil
	case ScheduleInterval:
		if c.Interval < 1 || !validUnit(c.Unit) {
			return nil, fmt.Errorf("invalid interval %d %q", c.Interval, c.Unit)
		}
		return intervalSchedule{n: c.Interval, unit: c.Unit}, nil
	}
	return nil, fmt.Errorf("unknown schedule type %q", c.ScheduleType)
}

type crontabSchedule struct {
	s   cron.Schedule
	loc *time.Location
}

// Next evaluates the crontab in the monitor's time zone, so "0 3 * * *" in
// Europe/Istanbul means 03:00 local time, across daylight saving changes.
func (c crontabSchedule) Next(t time.Time) time.Time {
	return c.s.Next(t.In(c.loc)).UTC()
}

type intervalSchedule struct {
	n    int
	unit string
}

// Next counts the interval from t: an interval monitor expects the next run
// one interval after the previous one.
func (i intervalSchedule) Next(t time.Time) time.Time {
	switch i.unit {
	case "minute":
		return t.Add(time.Duration(i.n) * time.Minute)
	case "hour":
		return t.Add(time.Duration(i.n) * time.Hour)
	case "day":
		return t.AddDate(0, 0, i.n)
	case "week":
		return t.AddDate(0, 0, 7*i.n)
	case "month":
		return t.AddDate(0, i.n, 0)
	default: // year
		return t.AddDate(i.n, 0, 0)
	}
}
