package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/monitor"
)

// joinAs invites a new user with the role and returns their signed-in client.
func joinAs(t *testing.T, e *env, owner *client, email string, role apiv1.Role) *client {
	t.Helper()
	ctx := context.Background()
	inv, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: email, Role: role}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	c := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := c.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{
		Token: url[strings.LastIndex(url, "/")+1:], Name: "Üye", Password: "üyenin uzun parolası"})); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMonitors(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}

	crontab := &apiv1.MonitorSchedule{Kind: &apiv1.MonitorSchedule_Crontab{Crontab: "30 2 * * *"}}
	created, err := owner.monitor.CreateMonitor(ctx, req(&apiv1.CreateMonitorRequest{Project: "kalem",
		Name: "Günlük tahsilat", Schedule: crontab, Timezone: "Europe/Istanbul", FailureThreshold: 2}))
	if err != nil {
		t.Fatal(err)
	}
	m := created.Msg.GetMonitor()
	if m.GetSlug() != "gunluk-tahsilat" || m.GetSchedule().GetCrontab() != "30 2 * * *" || m.GetCheckinMarginMinutes() != 1 ||
		m.GetMaxRuntimeMinutes() != 30 || m.GetFailureThreshold() != 2 || m.GetProject().GetSlug() != "kalem" {
		t.Fatalf("created: %v", m)
	}
	// The slug is taken, schedules are validated.
	if _, err := owner.monitor.CreateMonitor(ctx, req(&apiv1.CreateMonitorRequest{Project: "kalem", Name: "Başka",
		Slug: "gunluk-tahsilat", Schedule: crontab})); reason(err) != apiv1.ErrorReason_ERROR_REASON_SLUG_TAKEN {
		t.Fatalf("duplicate slug: %v", err)
	}
	if _, err := owner.monitor.CreateMonitor(ctx, req(&apiv1.CreateMonitorRequest{Project: "kalem", Name: "Bozuk",
		Schedule: &apiv1.MonitorSchedule{Kind: &apiv1.MonitorSchedule_Crontab{Crontab: "her gün"}}})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("bad crontab: %v", err)
	}
	if _, err := owner.monitor.CreateMonitor(ctx, req(&apiv1.CreateMonitorRequest{Project: "kalem", Name: "Bozuk",
		Schedule: crontab, Timezone: "Mars/Olympus"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("bad time zone: %v", err)
	}

	// Runs arrive from a job.
	org, err := owner.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	svc := monitor.NewService(e.db, e.log, ingest.NewIncidentEmitter(e.db, e.pipeline, 30*24*time.Hour))
	target := monitor.Target{OrgID: uuid.MustParse(org.Msg.GetOrganization().GetId()),
		ProjectID: uuid.MustParse(m.GetProject().GetId())}
	for i, status := range []string{monitor.StatusOK, monitor.StatusError, monitor.StatusError} {
		d := time.Duration(i+1) * time.Second
		if _, err := svc.Process(ctx, target, monitor.CheckIn{MonitorSlug: "gunluk-tahsilat", Status: status,
			Duration: &d, Environment: "production"}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := owner.monitor.ListMonitors(ctx, req(&apiv1.ListMonitorsRequest{}))
	if err != nil || len(list.Msg.GetMonitors()) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	lm := list.Msg.GetMonitors()[0]
	envs := lm.GetEnvironments()
	if len(envs) != 1 || envs[0].GetStatus() != apiv1.MonitorStatus_MONITOR_STATUS_ERROR || envs[0].GetFailures() != 2 ||
		envs[0].GetIssue() == nil || !strings.HasPrefix(envs[0].GetIssue().GetShortId(), "KALEM-") ||
		envs[0].GetNextCheckInAt() == nil {
		t.Fatalf("environment: %v", envs)
	}
	if runs := lm.GetRecentCheckIns(); len(runs) != 3 || runs[0].GetStatus() != apiv1.CheckInStatus_CHECK_IN_STATUS_ERROR ||
		runs[0].GetDuration().AsDuration() != 3*time.Second {
		t.Fatalf("recent runs: %v", runs)
	}
	// The monitor's issue is an ordinary issue.
	issues := listIssues(t, owner, "monitor.slug:gunluk-tahsilat")
	if len(issues) != 1 || issues[0].GetTitle() != "Zamanlanmış görev başarısız: Günlük tahsilat" {
		t.Fatalf("monitor issue: %v", issues)
	}
	if list, _ := owner.monitor.ListMonitors(ctx, req(&apiv1.ListMonitorsRequest{Query: "yok"})); len(list.Msg.GetMonitors()) != 0 {
		t.Fatalf("query filter: %v", list.Msg.GetMonitors())
	}

	// Runs page newest first.
	page, err := owner.monitor.ListCheckIns(ctx, req(&apiv1.ListCheckInsRequest{MonitorId: m.GetId(), PageSize: 2}))
	if err != nil || len(page.Msg.GetCheckIns()) != 2 || page.Msg.GetNextPageToken() == "" {
		t.Fatalf("first page: %v %v", page, err)
	}
	rest, err := owner.monitor.ListCheckIns(ctx, req(&apiv1.ListCheckInsRequest{MonitorId: m.GetId(), PageSize: 2,
		PageToken: page.Msg.GetNextPageToken()}))
	if err != nil || len(rest.Msg.GetCheckIns()) != 1 || rest.Msg.GetCheckIns()[0].GetStatus() != apiv1.CheckInStatus_CHECK_IN_STATUS_OK {
		t.Fatalf("second page: %v %v", rest, err)
	}

	// Pausing keeps the history; a new schedule moves the expected run.
	paused, name := true, "Gece tahsilatı"
	interval := &apiv1.MonitorSchedule{Kind: &apiv1.MonitorSchedule_Interval{Interval: &apiv1.MonitorInterval{
		Value: 2, Unit: apiv1.MonitorIntervalUnit_MONITOR_INTERVAL_UNIT_HOUR}}}
	upd, err := owner.monitor.UpdateMonitor(ctx, req(&apiv1.UpdateMonitorRequest{Id: m.GetId(), Paused: &paused, Name: &name,
		Schedule: interval}))
	if err != nil {
		t.Fatal(err)
	}
	um := upd.Msg.GetMonitor()
	next := um.GetEnvironments()[0].GetNextCheckInAt().AsTime()
	if !um.GetPaused() || um.GetName() != name || um.GetSchedule().GetInterval().GetValue() != 2 ||
		time.Until(next) < 110*time.Minute || time.Until(next) > 121*time.Minute {
		t.Fatalf("updated: %v (next in %v)", um, time.Until(next))
	}

	// Viewers read, only admins change.
	viewer := joinAs(t, e, owner, "izleyici@example.com", apiv1.Role_ROLE_VIEWER)
	if _, err := viewer.monitor.GetMonitor(ctx, req(&apiv1.GetMonitorRequest{Id: m.GetId()})); err != nil {
		t.Fatalf("viewer read: %v", err)
	}
	if _, err := viewer.monitor.DeleteMonitor(ctx, req(&apiv1.DeleteMonitorRequest{Id: m.GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("viewer delete: %v", err)
	}

	if _, err := owner.monitor.DeleteMonitor(ctx, req(&apiv1.DeleteMonitorRequest{Id: m.GetId()})); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.monitor.GetMonitor(ctx, req(&apiv1.GetMonitorRequest{Id: m.GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("deleted monitor: %v", err)
	}
	// The issue outlives its monitor.
	if len(listIssues(t, owner, "monitor.slug:gunluk-tahsilat")) != 1 {
		t.Fatal("monitor issue deleted with the monitor")
	}
}
