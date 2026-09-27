package api_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/store"
)

// TestAttachmentsFeedbackAndReleaseHealth reads what Sentry SDKs send besides
// errors through the API: attachments (and their download rules), user
// feedback and release health.
func TestAttachmentsFeedbackAndReleaseHealth(t *testing.T) {
	e, owner := seeded(t)
	ctx := context.Background()
	issue := listIssues(t, owner, "")[0]
	evRes, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{IssueId: issue.GetId(), Pick: apiv1.EventPick_EVENT_PICK_LATEST}))
	if err != nil {
		t.Fatal(err)
	}
	detail := evRes.Msg.GetEvent()
	eventID := uuid.MustParse(detail.GetEventId())
	projectID := uuid.MustParse(detail.GetProject().GetId())

	var org uuid.UUID
	err = e.db.InSystem(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT org_id FROM projects WHERE id = $1`, projectID).Scan(&org); err != nil {
			return err
		}
		if err := store.InsertAttachments(ctx, tx, []store.Attachment{
			{OrgID: org, ProjectID: projectID, EventID: eventID, Name: "ekran.png", ContentType: "image/png",
				AttachmentType: "event.attachment", Data: []byte("\x89PNG fake")},
			{OrgID: org, ProjectID: projectID, EventID: eventID, Name: "sayfa.html", ContentType: "text/html",
				AttachmentType: "event.attachment", Data: []byte("<script>alert(1)</script>")},
		}); err != nil {
			return err
		}
		for i, msg := range []string{"Ödeme sayfası dondu", "Rapor boş geliyor"} {
			f := &store.Feedback{OrgID: org, ProjectID: projectID, FeedbackID: uuid.New(), Name: "Ayşe",
				Email: "ayse@example.com", Message: msg, Environment: "production", Release: "kalem@9.9.9", Source: "widget"}
			if i == 0 {
				f.EventID = &eventID
			}
			if _, err := store.InsertFeedback(ctx, tx, f); err != nil {
				return err
			}
		}
		hour := time.Now().UTC().Truncate(time.Hour)
		if err := store.AddSessionCounts(ctx, tx, []store.SessionCounts{
			{OrgID: org, ProjectID: projectID, Release: "kalem@9.9.9", Environment: "production", Hour: hour, Started: 200, Errored: 10, Crashed: 4},
			{OrgID: org, ProjectID: projectID, Release: "kalem@9.9.8", Environment: "production", Hour: hour, Started: 50},
		}); err != nil {
			return err
		}
		if err := store.AddSessionUsers(ctx, tx, []store.SessionUser{
			{OrgID: org, ProjectID: projectID, Release: "kalem@9.9.9", Environment: "production", Day: hour, Hash: []byte("a")},
			{OrgID: org, ProjectID: projectID, Release: "kalem@9.9.9", Environment: "production", Day: hour, Hash: []byte("b"), Crashed: true},
		}); err != nil {
			return err
		}
		return store.TouchReleases(ctx, tx, []store.ReleaseSeen{
			{OrgID: org, ProjectID: projectID, Release: "kalem@9.9.9", Environment: "production", At: hour},
			{OrgID: org, ProjectID: projectID, Release: "kalem@9.9.8", Environment: "production", At: hour.Add(-time.Hour)},
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	// The event lists its files and feedback.
	evRes, err = owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{Id: detail.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	files := evRes.Msg.GetEvent().GetAttachments()
	if len(files) != 2 || files[0].GetName() != "ekran.png" || !strings.HasPrefix(files[0].GetUrl(), "/attachments/kentos/") {
		t.Fatalf("attachments: %v", files)
	}
	if fb := evRes.Msg.GetEvent().GetFeedback(); len(fb) != 1 || fb[0].GetMessage() != "Ödeme sayfası dondu" || fb[0].GetIssue().GetId() != issue.GetId() {
		t.Fatalf("event feedback: %v", fb)
	}

	get := func(c *http.Client, path string) *http.Response {
		t.Helper()
		res, err := c.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := get(owner.hc, files[0].GetUrl())
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || string(body) != "\x89PNG fake" || res.Header.Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(res.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("image download: %d %v", res.StatusCode, res.Header)
	}
	// HTML is never rendered from izci's origin.
	res = get(owner.hc, files[1].GetUrl())
	res.Body.Close()
	if res.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") ||
		!strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("html download: %v", res.Header)
	}
	if res := get(http.DefaultClient, files[0].GetUrl()); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous download: %d", res.StatusCode)
	}
	// A user of another organization cannot fetch it, even by guessing ids.
	if _, err := owner.org.CreateOrganization(ctx, req(&apiv1.CreateOrganizationRequest{Name: "Başka", Slug: "baska"})); err != nil {
		t.Fatal(err)
	}
	baska := e.client(map[string]string{"Izci-Org": "baska"})
	if _, err := baska.auth.Login(ctx, req(&apiv1.LoginRequest{Email: "owner@example.com", Password: "uzun ve güçlü parola"})); err != nil {
		t.Fatal(err)
	}
	inv, err := baska.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "yabanci@example.com", Role: apiv1.Role_ROLE_OWNER}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	outsider := e.client(map[string]string{"Izci-Org": "baska"})
	if _, err := outsider.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: url[strings.LastIndex(url, "/")+1:],
		Name: "Yabancı", Password: "yabancının parolası"})); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{files[0].GetUrl(), strings.Replace(files[0].GetUrl(), "/kentos/", "/baska/", 1)} {
		if res := get(outsider.hc, path); res.StatusCode != http.StatusNotFound {
			t.Errorf("outsider %s: %d", path, res.StatusCode)
		}
	}

	// Feedback lists and filters.
	all, err := owner.feedback.ListFeedback(ctx, req(&apiv1.ListFeedbackRequest{PageSize: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Msg.GetFeedback()) != 1 || all.Msg.GetNextPageToken() == "" {
		t.Fatalf("first page: %v", all.Msg)
	}
	next, err := owner.feedback.ListFeedback(ctx, req(&apiv1.ListFeedbackRequest{PageSize: 1, PageToken: all.Msg.GetNextPageToken()}))
	if err != nil || len(next.Msg.GetFeedback()) != 1 || next.Msg.GetFeedback()[0].GetId() == all.Msg.GetFeedback()[0].GetId() {
		t.Fatalf("second page: %v %v", next.Msg, err)
	}
	byIssue, err := owner.feedback.ListFeedback(ctx, req(&apiv1.ListFeedbackRequest{IssueId: issue.GetId()}))
	if err != nil || len(byIssue.Msg.GetFeedback()) != 1 {
		t.Fatalf("issue feedback: %v %v", byIssue.Msg, err)
	}
	found, err := owner.feedback.ListFeedback(ctx, req(&apiv1.ListFeedbackRequest{Project: detail.GetProject().GetSlug(), Query: "rapor"}))
	if err != nil || len(found.Msg.GetFeedback()) != 1 || found.Msg.GetFeedback()[0].GetMessage() != "Rapor boş geliyor" {
		t.Fatalf("search: %v %v", found.Msg, err)
	}
	if _, err := outsider.feedback.ListFeedback(ctx, req(&apiv1.ListFeedbackRequest{})); err != nil {
		t.Fatal(err)
	} else if res, _ := outsider.feedback.ListFeedback(ctx, req(&apiv1.ListFeedbackRequest{})); len(res.Msg.GetFeedback()) != 0 {
		t.Error("feedback leaked across organizations")
	}

	// Release health.
	rel, err := owner.project.ListReleases(ctx, req(&apiv1.ListReleasesRequest{Project: detail.GetProject().GetSlug(), HealthPeriod: apiv1.StatsPeriod_STATS_PERIOD_LAST_24_HOURS}))
	if err != nil {
		t.Fatal(err)
	}
	var h *apiv1.ReleaseHealth
	for _, r := range rel.Msg.GetReleases() {
		if r.GetVersion() == "kalem@9.9.9" {
			h = r.GetHealth()
		}
	}
	if h == nil || h.GetSessions() != 200 || h.GetCrashFreeSessions() != 98 || h.GetUsers() != 2 || h.GetCrashFreeUsers() != 50 ||
		h.GetAdoption() != 80 {
		t.Fatalf("health: %v", h)
	}
}
