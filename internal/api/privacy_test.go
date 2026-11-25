package api_test

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/sample"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// sendEvent stores an event in a project of "kentos" through the pipeline
// and returns its server id and issue id.
func sendEvent(t *testing.T, e *env, project, fingerprint string, edit func(*ingestv1.Event)) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var org *store.Organization
	var p *store.Project
	err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if org, err = store.GetOrganizationBySlug(ctx, tx, "kentos"); err != nil {
			return err
		}
		p, err = store.GetProjectBySlug(ctx, tx, org.ID, project)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := sample.Event(rand.New(rand.NewPCG(1, 2)), sample.Options{Template: 0})
	ev.Fingerprint = []string{fingerprint}
	ev.User, ev.Customer = nil, nil
	edit(ev)
	processed, err := ingest.Normalize(ev, ingest.NormalizeOptions{
		Project:   ingest.ProjectContext{OrgID: org.ID, OrgSlug: org.Slug, ProjectID: p.ID, ProjectSlug: p.Slug, Platform: p.Platform, Settings: p.Settings},
		Retention: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	chans, err := e.pipeline.Submit(processed)
	if err != nil {
		t.Fatal(err)
	}
	r := <-chans[0]
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	return processed.ID, r.IssueID
}

func countRows(t *testing.T, e *env, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.InSystem(context.Background(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestDataSubjectRequests finds, exports and deletes the data of a person
// and of a customer.
func TestDataSubjectRequests(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	privacy := owner.privacy
	ayse := func(ev *ingestv1.Event) {
		ev.User = &ingestv1.User{Id: "42", Email: "Ayse@Example.com", IpAddress: "10.1.2.3"}
		ev.Customer = &ingestv1.Customer{Id: "sivas-belediyesi"}
	}
	// Ayşe in two issues, by e-mail in one event and by id alone in another.
	first, issueA := sendEvent(t, e, "kalem", "a", ayse)
	sendEvent(t, e, "kalem", "b", ayse)
	sendEvent(t, e, "kalem", "b", func(ev *ingestv1.Event) { ev.User = &ingestv1.User{Id: "42"} })
	// Mehmet of another customer, in the same issue.
	other, _ := sendEvent(t, e, "kalem", "a", func(ev *ingestv1.Event) {
		ev.User = &ingestv1.User{Email: "mehmet@example.com"}
		ev.Customer = &ingestv1.Customer{Id: "kayseri"}
	})
	err := e.db.InSystem(ctx, func(tx pgx.Tx) error {
		var org, project uuid.UUID
		var eventID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT org_id, project_id, event_id FROM events WHERE id = $1`, first).Scan(&org, &project, &eventID); err != nil {
			return err
		}
		if err := store.InsertAttachments(ctx, tx, []store.Attachment{{OrgID: org, ProjectID: project, EventID: eventID, Name: "ekran.png",
			ContentType: "image/png", AttachmentType: "event.attachment", Data: []byte("png")}}); err != nil {
			return err
		}
		for _, f := range []*store.Feedback{
			{OrgID: org, ProjectID: project, FeedbackID: uuid.New(), EventID: &eventID, Message: "Sayfa açılmıyor"},
			{OrgID: org, ProjectID: project, FeedbackID: uuid.New(), Email: "ayse@example.com", Message: "Yine olmadı"},
			{OrgID: org, ProjectID: project, FeedbackID: uuid.New(), Email: "mehmet@example.com", Message: "Bende de"},
		} {
			if _, err := store.InsertFeedback(ctx, tx, f); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// The migration fills issue_subjects for older events with SQL: its
	// hashes must be the ones ingest writes.
	if n := countRows(t, e, `
		SELECT count(*) FROM (
		  SELECT DISTINCT e.issue_id, substring(sha256(convert_to(k, 'UTF8')) FROM 1 FOR 8) AS h
		  FROM events e CROSS JOIN LATERAL unnest(izci_subject_keys(e.payload -> 'user', e.client_ip)) AS k
		) b FULL JOIN issue_subjects s ON s.issue_id = b.issue_id AND s.key_hash = b.h
		WHERE s.issue_id IS NULL OR b.issue_id IS NULL`); n != 0 {
		t.Fatalf("issue_subjects differs from the migration's backfill in %d rows", n)
	}

	person := &apiv1.DataSubject{Email: "AYSE@example.com"}
	found, err := privacy.FindSubjectData(ctx, req(&apiv1.FindSubjectDataRequest{Subject: person}))
	if err != nil {
		t.Fatal(err)
	}
	if m := found.Msg; m.GetEvents() != 2 || m.GetIssues() != 2 || m.GetAttachments() != 1 || m.GetFeedback() != 2 || len(m.GetLatest()) != 2 ||
		!strings.HasPrefix(m.GetLatest()[0].GetIssueShortId(), "KALEM-") {
		t.Fatalf("by e-mail: %v", m)
	}
	// Every identifier of the person counts.
	both := &apiv1.DataSubject{Email: "ayse@example.com", UserId: "42"}
	if found, err := privacy.FindSubjectData(ctx, req(&apiv1.FindSubjectDataRequest{Subject: both})); err != nil || found.Msg.GetEvents() != 3 {
		t.Fatalf("by e-mail and id: %v %v", found, err)
	}
	if found, err := privacy.FindSubjectData(ctx, req(&apiv1.FindSubjectDataRequest{Subject: &apiv1.DataSubject{IpAddress: "10.1.2.3"}})); err != nil ||
		found.Msg.GetEvents() != 2 {
		t.Fatalf("by IP: %v %v", found, err)
	}
	customer := &apiv1.DataSubject{Customer: "Sivas-Belediyesi"}
	if found, err := privacy.FindSubjectData(ctx, req(&apiv1.FindSubjectDataRequest{Subject: customer})); err != nil || found.Msg.GetEvents() != 2 {
		t.Fatalf("by customer: %v %v", found, err)
	}
	for _, bad := range []*apiv1.DataSubject{{}, {Email: "a@b.c", Customer: "x"}, {IpAddress: "yok"}} {
		if _, err := privacy.FindSubjectData(ctx, req(&apiv1.FindSubjectDataRequest{Subject: bad})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
			t.Errorf("subject %v: %v", bad, err)
		}
	}

	// The export pages through events oldest first; feedback comes first.
	page, err := privacy.ExportSubjectData(ctx, req(&apiv1.ExportSubjectDataRequest{Subject: both, PageSize: 2}))
	if err != nil || len(page.Msg.GetEventsJson()) != 2 || page.Msg.GetNextPageToken() == "" || len(page.Msg.GetFeedbackJson()) != 2 {
		t.Fatalf("export page 1: %v %v", page, err)
	}
	var exported struct {
		ID          string `json:"id"`
		Issue       string `json:"issue"`
		Attachments []struct {
			Name string `json:"name"`
		} `json:"attachments"`
		Event struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"event"`
	}
	if err := json.Unmarshal([]byte(page.Msg.GetEventsJson()[0]), &exported); err != nil || exported.ID != first.String() ||
		exported.Event.User.Email != "Ayse@Example.com" || len(exported.Attachments) != 1 || !strings.HasPrefix(exported.Issue, "KALEM-") {
		t.Fatalf("exported event: %+v %v", exported, err)
	}
	next, err := privacy.ExportSubjectData(ctx, req(&apiv1.ExportSubjectDataRequest{Subject: both, PageSize: 2, PageToken: page.Msg.GetNextPageToken()}))
	if err != nil || len(next.Msg.GetEventsJson()) != 1 || next.Msg.GetNextPageToken() != "" || len(next.Msg.GetFeedbackJson()) != 0 {
		t.Fatalf("export page 2: %v %v", next, err)
	}

	// Members cannot read other people's data; admins can.
	if _, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "uye@example.com", Role: apiv1.Role_ROLE_MEMBER})); err != nil {
		t.Fatal(err)
	}

	// Deleting the person removes their events, attachments, feedback and
	// the records that named them, and leaves Mehmet alone.
	del, err := privacy.DeleteSubjectData(ctx, req(&apiv1.DeleteSubjectDataRequest{Subject: both}))
	if err != nil || !del.Msg.GetDone() || del.Msg.GetEvents() != 3 || del.Msg.GetAttachments() != 1 || del.Msg.GetFeedback() != 2 {
		t.Fatalf("delete: %v %v", del, err)
	}
	if found, err := privacy.FindSubjectData(ctx, req(&apiv1.FindSubjectDataRequest{Subject: both})); err != nil || found.Msg.GetEvents() != 0 ||
		found.Msg.GetFeedback() != 0 {
		t.Fatalf("after delete: %v %v", found, err)
	}
	if n := countRows(t, e, `SELECT count(*) FROM events WHERE id = $1`, other); n != 1 {
		t.Fatalf("other person's event: %d", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM issue_tags WHERE key = 'user' AND value ILIKE 'ayse%'`); n != 0 {
		t.Fatalf("user tags left: %d", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM issue_users WHERE issue_id = $1`, issueA); n != 1 {
		t.Fatalf("affected users of the shared issue: %d", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM user_feedback WHERE email = 'mehmet@example.com'`); n != 1 {
		t.Fatalf("other person's feedback: %d", n)
	}
	audit, err := owner.org.ListAuditLog(ctx, req(&apiv1.ListAuditLogRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var logged []string
	for _, a := range audit.Msg.GetEntries() {
		if strings.HasPrefix(a.GetAction(), "privacy.") {
			logged = append(logged, a.GetAction()+" "+a.GetTargetName())
		}
	}
	if strings.Join(logged, ",") != "privacy.delete ay***@example.com,privacy.export ay***@example.com" {
		t.Fatalf("audit: %v", logged)
	}

	// A customer's data goes with the customer.
	del, err = privacy.DeleteSubjectData(ctx, req(&apiv1.DeleteSubjectDataRequest{Subject: &apiv1.DataSubject{Customer: "kayseri"}}))
	if err != nil || !del.Msg.GetDone() || del.Msg.GetEvents() != 1 {
		t.Fatalf("delete customer: %v %v", del, err)
	}
	if n := countRows(t, e, `SELECT count(*) FROM customers WHERE key = 'kayseri'`); n != 0 {
		t.Fatalf("customer left: %d", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM issue_tags WHERE key = 'customer' AND value = 'kayseri'`); n != 0 {
		t.Fatalf("customer tags left: %d", n)
	}
}

// TestDeleteIssuesAndEvents deletes single events and whole issues.
func TestDeleteIssuesAndEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	user := func(ev *ingestv1.Event) { ev.User = &ingestv1.User{Email: "ali@example.com"} }
	one, issue := sendEvent(t, e, "kalem", "x", user)
	two, _ := sendEvent(t, e, "kalem", "x", user)

	if _, err := owner.event.DeleteEvent(ctx, req(&apiv1.DeleteEventRequest{Id: one.String()})); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{Id: one.String()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("deleted event: %v", err)
	}
	if _, err := owner.event.DeleteEvent(ctx, req(&apiv1.DeleteEventRequest{Id: one.String()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("deleting twice: %v", err)
	}
	if _, err := owner.event.GetEvent(ctx, req(&apiv1.GetEventRequest{Id: two.String()})); err != nil {
		t.Fatalf("other event: %v", err)
	}

	res, err := owner.issue.DeleteIssues(ctx, req(&apiv1.DeleteIssuesRequest{IssueIds: []string{issue.String(), uuid.NewString()}}))
	if err != nil || res.Msg.GetDeleted() != 1 {
		t.Fatalf("delete issues: %v %v", res, err)
	}
	if _, err := owner.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: issue.String()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("deleted issue: %v", err)
	}
	// Its events go in the background.
	if n, err := e.db.PurgeDeletedIssues(ctx, 10); err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	for table, sql := range map[string]string{
		"events":         `SELECT count(*) FROM events WHERE issue_id = $1`,
		"issue_hourly":   `SELECT count(*) FROM issue_hourly WHERE issue_id = $1`,
		"issue_tags":     `SELECT count(*) FROM issue_tags WHERE issue_id = $1`,
		"deleted_issues": `SELECT count(*) FROM deleted_issues WHERE issue_id = $1`,
	} {
		if n := countRows(t, e, sql, issue); n != 0 {
			t.Errorf("%s left: %d", table, n)
		}
	}
	// The same fingerprint opens a new issue.
	if _, again := sendEvent(t, e, "kalem", "x", user); again == issue {
		t.Fatal("event joined the deleted issue")
	}
}

// TestDeleteAccountAndOrganization deletes accounts and organizations, and
// limits retention.
func TestDeleteAccountAndOrganization(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(ctx, req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner", Email: "owner@example.com",
		Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos", ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	inv, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "veli@example.com", Role: apiv1.Role_ROLE_ADMIN}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	veli := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := veli.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: url[strings.LastIndex(url, "/")+1:],
		Name: "Veli", Password: "velinin uzun parolası"})); err != nil {
		t.Fatal(err)
	}
	_, issue := sendEvent(t, e, "kalem", "y", func(*ingestv1.Event) {})
	if _, err := veli.issue.UpdateIssues(ctx, req(&apiv1.UpdateIssuesRequest{IssueIds: []string{issue.String()},
		AssigneeId: ptr(e.userID(t, "veli@example.com"))})); err != nil {
		t.Fatal(err)
	}

	// Retention: shorter than the server's only, and shortening needs a
	// fresh sign-in (setup and invitation acceptance count as one).
	org, err := owner.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{}))
	if err != nil || org.Msg.GetOrganization().GetMaxRetentionDays() != 30 {
		t.Fatalf("max retention: %v %v", org, err)
	}
	settings := org.Msg.GetOrganization().GetSettings()
	settings.RetentionDays = 45
	if _, err := owner.org.UpdateOrganization(ctx, req(&apiv1.UpdateOrganizationRequest{Settings: settings})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("too long: %v", err)
	}
	settings.RetentionDays = 7
	if _, err := owner.org.UpdateOrganization(ctx, req(&apiv1.UpdateOrganizationRequest{Settings: settings})); err != nil {
		t.Fatalf("retention: %v", err)
	}
	p, err := owner.project.GetProject(ctx, req(&apiv1.GetProjectRequest{Slug: "kalem"}))
	if err != nil {
		t.Fatal(err)
	}
	ps := p.Msg.GetProject().GetSettings()
	ps.Privacy.RetentionDays = 10
	if _, err := owner.project.UpdateProject(ctx, req(&apiv1.UpdateProjectRequest{Slug: "kalem", Settings: ps})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("project longer than the organization: %v", err)
	}
	ps.Privacy.RetentionDays = 3
	if _, err := owner.project.UpdateProject(ctx, req(&apiv1.UpdateProjectRequest{Slug: "kalem", Settings: ps})); err != nil {
		t.Fatalf("project retention: %v", err)
	}
	short, err := e.db.ShortRetentionProjects(ctx, 30)
	if err != nil || len(short) != 1 || short[0].Days != 3 {
		t.Fatalf("short retention: %v %v", short, err)
	}
	if n, err := e.db.ApplyProjectRetention(ctx, short[0].ProjectID, time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("project retention: %d %v", n, err)
	}

	// The only owner cannot leave; an account goes with its memberships.
	if _, err := owner.user.DeleteAccount(ctx, req(&apiv1.DeleteAccountRequest{ConfirmEmail: "owner@example.com"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_LAST_OWNER {
		t.Fatalf("only owner: %v", err)
	}
	if _, err := veli.user.DeleteAccount(ctx, req(&apiv1.DeleteAccountRequest{ConfirmEmail: "yanlis@example.com"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("wrong confirmation: %v", err)
	}
	if _, err := veli.user.DeleteAccount(ctx, req(&apiv1.DeleteAccountRequest{ConfirmEmail: "VELI@example.com"})); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if _, err := veli.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{})); reason(err) != apiv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED {
		t.Fatalf("session of a deleted account: %v", err)
	}
	if n := countRows(t, e, `SELECT count(*) FROM issues WHERE assignee_id IS NOT NULL`); n != 0 {
		t.Fatalf("assignments left: %d", n)
	}
	members, err := owner.org.ListMembers(ctx, req(&apiv1.ListMembersRequest{}))
	if err != nil || len(members.Msg.GetMembers()) != 1 {
		t.Fatalf("members: %v %v", members, err)
	}

	// Deleting the organization hides it at once and frees its slug.
	if _, err := owner.org.DeleteOrganization(ctx, req(&apiv1.DeleteOrganizationRequest{ConfirmSlug: "yanlis"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("wrong slug: %v", err)
	}
	if _, err := owner.org.DeleteOrganization(ctx, req(&apiv1.DeleteOrganizationRequest{ConfirmSlug: "kentos"})); err != nil {
		t.Fatalf("delete organization: %v", err)
	}
	if _, err := owner.org.GetOrganization(ctx, req(&apiv1.GetOrganizationRequest{})); err == nil {
		t.Fatal("deleted organization still answers")
	}
	sess, err := owner.auth.GetSession(ctx, req(&apiv1.GetSessionRequest{}))
	if err != nil || len(sess.Msg.GetSession().GetMemberships()) != 0 {
		t.Fatalf("memberships: %v %v", sess, err)
	}
	if _, err := e.db.PurgeDeletedProjects(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if ids, err := e.db.PurgeDeletedOrganizations(ctx, 10); err != nil || len(ids) != 1 {
		t.Fatalf("purge organizations: %v %v", ids, err)
	}
	if n := countRows(t, e, `SELECT count(*) FROM organizations`); n != 0 {
		t.Fatalf("organizations left: %d", n)
	}
	// The owner, now in no organization, can delete their account; the
	// last server administrator cannot.
	if _, err := owner.user.DeleteAccount(ctx, req(&apiv1.DeleteAccountRequest{ConfirmEmail: "owner@example.com"})); reason(err) != apiv1.ErrorReason_ERROR_REASON_LAST_SUPERADMIN {
		t.Fatalf("last administrator: %v", err)
	}
}

// userID looks up a user by e-mail.
func (e *env) userID(t *testing.T, email string) string {
	t.Helper()
	var id uuid.UUID
	if err := e.db.Pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id.String()
}
