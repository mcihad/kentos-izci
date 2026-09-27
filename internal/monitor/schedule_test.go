package monitor

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeDefaults(t *testing.T) {
	c := Config{ScheduleType: " Crontab ", Crontab: "0  3 * * *"}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if c.Crontab != "0 3 * * *" || c.Timezone != "UTC" || c.CheckinMargin != 1 || c.MaxRuntime != 30 ||
		c.FailureThreshold != 1 || c.RecoveryThreshold != 1 || c.ScheduleType != ScheduleCrontab {
		t.Fatalf("defaults not applied: %+v", c)
	}
}

func TestNormalizeRejects(t *testing.T) {
	cases := []struct {
		name string
		c    Config
		want string
	}{
		{"no type", Config{}, "type must be"},
		{"empty crontab", Config{ScheduleType: "crontab"}, "crontab expression is required"},
		{"bad crontab", Config{ScheduleType: "crontab", Crontab: "61 * * * *"}, "invalid crontab"},
		{"six fields", Config{ScheduleType: "crontab", Crontab: "0 0 3 * * *"}, "invalid crontab"},
		{"every", Config{ScheduleType: "crontab", Crontab: "@every 5m"}, "interval schedule instead"},
		{"zero interval", Config{ScheduleType: "interval", Unit: "hour"}, "interval must be between"},
		{"bad unit", Config{ScheduleType: "interval", Interval: 2, Unit: "fortnight"}, "unit must be one of"},
		{"bad zone", Config{ScheduleType: "crontab", Crontab: "@daily", Timezone: "Mars/Olympus"}, "not a known IANA"},
		{"margin", Config{ScheduleType: "crontab", Crontab: "@daily", CheckinMargin: -1}, "checkin_margin"},
		{"runtime", Config{ScheduleType: "crontab", Crontab: "@daily", MaxRuntime: 20000}, "max_runtime"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Normalize()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Normalize() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestPluralUnitAccepted(t *testing.T) {
	c := Config{ScheduleType: "interval", Interval: 2, Unit: "Hours"}
	if err := c.Normalize(); err != nil || c.Unit != "hour" {
		t.Fatalf("Normalize() = %v, unit %q", err, c.Unit)
	}
}

func mustSchedule(t *testing.T, c Config) Schedule {
	t.Helper()
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	s, err := c.Schedule()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCrontabInTimeZone(t *testing.T) {
	s := mustSchedule(t, Config{ScheduleType: "crontab", Crontab: "0 3 * * *", Timezone: "Europe/Istanbul"})
	// 03:00 in Istanbul (UTC+3) is 00:00 UTC.
	from := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	want := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	if got := s.Next(from); !got.Equal(want) {
		t.Fatalf("Next(%v) = %v, want %v", from, got, want)
	}
	// Strictly after: a run at the scheduled minute expects the next day.
	if got := s.Next(want); !got.Equal(want.AddDate(0, 0, 1)) {
		t.Fatalf("Next(%v) = %v", want, got)
	}
}

func TestCrontabDescriptorAndDST(t *testing.T) {
	s := mustSchedule(t, Config{ScheduleType: "crontab", Crontab: "@daily", Timezone: "Europe/Berlin"})
	// The night Berlin leaves summer time: midnight is 22:00 UTC before and
	// 23:00 UTC after the change.
	from := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if got, want := s.Next(from), time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("before DST end: %v, want %v", got, want)
	}
	from = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if got, want := s.Next(from), time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("after DST end: %v, want %v", got, want)
	}
}

func TestIntervalNext(t *testing.T) {
	from := time.Date(2026, 1, 31, 10, 30, 0, 0, time.UTC)
	cases := []struct {
		n    int
		unit string
		want time.Time
	}{
		{15, "minute", from.Add(15 * time.Minute)},
		{2, "hour", from.Add(2 * time.Hour)},
		{1, "day", from.AddDate(0, 0, 1)},
		{2, "week", from.AddDate(0, 0, 14)},
		{1, "month", from.AddDate(0, 1, 0)},
		{1, "year", from.AddDate(1, 0, 0)},
	}
	for _, tc := range cases {
		s := mustSchedule(t, Config{ScheduleType: "interval", Interval: tc.n, Unit: tc.unit})
		if got := s.Next(from); !got.Equal(tc.want) {
			t.Errorf("every %d %s: Next = %v, want %v", tc.n, tc.unit, got, tc.want)
		}
	}
}

func TestNormalizeSlug(t *testing.T) {
	cases := map[string]string{
		"daily_report":          "daily_report",
		"Günlük Rapor":          "gunluk-rapor",
		"  Tahsilat / Gece  ":   "tahsilat-gece",
		"2fa-cleanup":           "2fa-cleanup",
		"---":                   "",
		strings.Repeat("a", 60): strings.Repeat("a", 50),
	}
	for in, want := range cases {
		if got := NormalizeSlug(in); got != want {
			t.Errorf("NormalizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
