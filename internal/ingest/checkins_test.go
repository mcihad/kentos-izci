package ingest_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"

	izci "github.com/mcihad/kentos-izci/sdk/go"

	"github.com/mcihad/kentos-izci/internal/ingest/ingesttest"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
	"github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1/ingestv1connect"
)

// TestGoSDKMonitoredJob runs jobs through the izci Go SDK over both
// protocols and checks what the server stored.
func TestGoSDKMonitoredJob(t *testing.T) {
	e := ingesttest.New(t, ingesttest.Options{})
	ctx := context.Background()
	for _, proto := range []izci.Protocol{izci.ProtocolConnect, izci.ProtocolGRPC} {
		client, err := izci.NewClient(izci.Options{Endpoint: e.Server.URL, Key: e.Key, Protocol: proto,
			Environment: "production", Release: "aktarim@3.1.0"})
		if err != nil {
			t.Fatal(err)
		}
		cfg := &izci.MonitorConfig{Interval: 6, Unit: izci.Hour, FailureThreshold: 3}
		if err := client.WithMonitor(ctx, "Gece Aktarımı", cfg, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
		client.Close(time.Second)
	}
	var runs, ok int
	e.Scalar(t, &runs, `SELECT count(*) FROM monitor_checkins`)
	e.Scalar(t, &ok, `SELECT count(*) FROM monitor_checkins WHERE status = 'ok' AND duration_ms IS NOT NULL
		AND release = 'aktarim@3.1.0'`)
	if runs != 2 || ok != 2 {
		t.Fatalf("runs: %d, finished ok: %d", runs, ok)
	}
	var unit string
	var interval, threshold int
	e.Scalar(t, &unit, `SELECT schedule_unit FROM monitors WHERE slug = 'gece-aktarimi'`)
	e.Scalar(t, &interval, `SELECT schedule_interval FROM monitors WHERE slug = 'gece-aktarimi'`)
	e.Scalar(t, &threshold, `SELECT failure_threshold FROM monitors WHERE slug = 'gece-aktarimi'`)
	if unit != "hour" || interval != 6 || threshold != 3 {
		t.Fatalf("monitor: every %d %s, threshold %d", interval, unit, threshold)
	}
}

// TestSendCheckInErrors covers the errors of IngestService.SendCheckIn.
func TestSendCheckInErrors(t *testing.T) {
	e := ingesttest.New(t, ingesttest.Options{})
	client := ingestv1connect.NewIngestServiceClient(http.DefaultClient, e.Server.URL)
	send := func(ci *ingestv1.CheckIn) error {
		req := connect.NewRequest(&ingestv1.SendCheckInRequest{CheckIn: ci})
		req.Header().Set("Authorization", "Bearer "+e.Key)
		_, err := client.SendCheckIn(context.Background(), req)
		return err
	}
	cases := []struct {
		name string
		ci   *ingestv1.CheckIn
		code connect.Code
	}{
		{"missing check-in", nil, connect.CodeInvalidArgument},
		{"no status", &ingestv1.CheckIn{MonitorSlug: "x"}, connect.CodeInvalidArgument},
		{"bad id", &ingestv1.CheckIn{MonitorSlug: "x", Status: ingestv1.CheckInStatus_CHECK_IN_STATUS_OK, CheckInId: "42"}, connect.CodeInvalidArgument},
		{"unknown monitor", &ingestv1.CheckIn{MonitorSlug: "x", Status: ingestv1.CheckInStatus_CHECK_IN_STATUS_OK}, connect.CodeNotFound},
		{"bad crontab", &ingestv1.CheckIn{MonitorSlug: "x", Status: ingestv1.CheckInStatus_CHECK_IN_STATUS_OK,
			MonitorConfig: &ingestv1.MonitorConfig{Schedule: &ingestv1.MonitorConfig_Crontab{Crontab: "every day"}}}, connect.CodeInvalidArgument},
		{"no schedule", &ingestv1.CheckIn{MonitorSlug: "x", Status: ingestv1.CheckInStatus_CHECK_IN_STATUS_OK,
			MonitorConfig: &ingestv1.MonitorConfig{}}, connect.CodeInvalidArgument},
	}
	for _, tc := range cases {
		err := send(tc.ci)
		var cerr *connect.Error
		if !errors.As(err, &cerr) || cerr.Code() != tc.code {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.code)
		}
	}
}
