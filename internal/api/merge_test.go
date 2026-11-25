package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ingest"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

func setupKentos(t *testing.T, e *env) *client {
	t.Helper()
	owner := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := owner.auth.CompleteSetup(context.Background(), req(&apiv1.CompleteSetupRequest{SetupToken: testSetupToken, Name: "Owner",
		Email: "owner@example.com", Password: "uzun ve güçlü parola", OrganizationName: "KentOS", OrganizationSlug: "kentos",
		ProjectName: "Kalem"})); err != nil {
		t.Fatal(err)
	}
	return owner
}

func getIssue(t *testing.T, c *client, ref string) *apiv1.Issue {
	t.Helper()
	res, err := c.issue.GetIssue(context.Background(), req(&apiv1.GetIssueRequest{Issue: ref}))
	if err != nil {
		t.Fatalf("get issue %s: %v", ref, err)
	}
	return res.Msg.GetIssue()
}

func moveEvents(t *testing.T, e *env) {
	t.Helper()
	if _, err := ingest.ProcessIssueMoves(context.Background(), e.db, 10); err != nil {
		t.Fatal(err)
	}
}

// TestMergeAndUnmerge merges an issue into another, follows its events and
// new events there, and moves it out again.
func TestMergeAndUnmerge(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := setupKentos(t, e)
	as := func(id string) func(*ingestv1.Event) {
		return func(ev *ingestv1.Event) { ev.User = &ingestv1.User{Id: id} }
	}
	_, a := sendEvent(t, e, "kalem", "a", as("1"))
	sendEvent(t, e, "kalem", "a", as("2"))
	_, b := sendEvent(t, e, "kalem", "b", func(ev *ingestv1.Event) {
		ev.User = &ingestv1.User{Id: "3"}
		ev.Customer = &ingestv1.Customer{Id: "kayseri"}
	})
	sendEvent(t, e, "kalem", "b", as("3"))
	bShort := getIssue(t, owner, b.String()).GetSummary().GetShortId()
	if _, err := owner.issue.AddIssueComment(ctx, req(&apiv1.AddIssueCommentRequest{IssueId: b.String(), Text: "Aynı hata"})); err != nil {
		t.Fatal(err)
	}

	if _, err := owner.issue.MergeIssues(ctx, req(&apiv1.MergeIssuesRequest{TargetIssueId: a.String(), IssueIds: []string{a.String()}})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_ARGUMENT {
		t.Fatalf("merge into itself: %v", err)
	}
	res, err := owner.issue.MergeIssues(ctx, req(&apiv1.MergeIssuesRequest{TargetIssueId: a.String(), IssueIds: []string{b.String()}}))
	if err != nil {
		t.Fatal(err)
	}
	if is := res.Msg.GetIssue(); res.Msg.GetMerged() != 1 || is.GetTimesSeen() != 4 || is.GetUsersCount() != 3 || is.GetCustomersCount() != 1 {
		t.Fatalf("merged: %v", res.Msg)
	}
	// The merged issue's id and short id lead to the target.
	for _, ref := range []string{b.String(), bShort} {
		if got := getIssue(t, owner, ref); got.GetSummary().GetId() != a.String() {
			t.Fatalf("%s leads to %s", ref, got.GetSummary().GetId())
		}
	}
	target := getIssue(t, owner, a.String())
	if len(target.GetMerged()) != 1 || target.GetMerged()[0].GetShortId() != bShort || target.GetMerged()[0].GetMergedBy().GetEmail() != "owner@example.com" {
		t.Fatalf("merged list: %v", target.GetMerged())
	}
	fingerprint := target.GetMerged()[0].GetFingerprint()

	// Events move in the background; new events of the fingerprint join.
	moveEvents(t, e)
	if n := countRows(t, e, `SELECT count(*) FROM events WHERE issue_id = $1`, a); n != 4 {
		t.Fatalf("events after the move: %d", n)
	}
	if _, joined := sendEvent(t, e, "kalem", "b", as("4")); joined != a {
		t.Fatalf("new event of the merged fingerprint went to %s", joined)
	}
	if n := countRows(t, e, `SELECT count(*) FROM events WHERE issue_id = $1 AND grouping_hash IS NOT NULL`, a); n != 3 {
		t.Fatalf("events keeping their fingerprint: %d", n)
	}
	activity, err := owner.issue.ListIssueActivity(ctx, req(&apiv1.ListIssueActivityRequest{IssueId: a.String()}))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ac := range activity.Msg.GetActivities() {
		kinds = append(kinds, ac.GetKind().String())
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, "ACTIVITY_KIND_MERGED") || !strings.Contains(joined, "ACTIVITY_KIND_COMMENT") {
		t.Fatalf("activity: %v", kinds)
	}

	// Unmerging brings the issue back under its old short id.
	un, err := owner.issue.UnmergeIssue(ctx, req(&apiv1.UnmergeIssueRequest{IssueId: a.String(), Fingerprint: fingerprint}))
	if err != nil {
		t.Fatal(err)
	}
	back := un.Msg.GetIssue()
	if back.GetShortId() != bShort {
		t.Fatalf("unmerged: %v", back)
	}
	moveEvents(t, e)
	c, err := uuid.Parse(back.GetId())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id     uuid.UUID
		events uint64
		users  uint32
	}{{a, 2, 2}, {c, 3, 2}} {
		is := getIssue(t, owner, tc.id.String()).GetSummary()
		if is.GetTimesSeen() != tc.events || is.GetUsersCount() != tc.users {
			t.Errorf("issue %s: %d events, %d users", is.GetShortId(), is.GetTimesSeen(), is.GetUsersCount())
		}
	}
	if n := countRows(t, e, `SELECT count(*) FROM events WHERE issue_id = $1 AND grouping_hash IS NULL`, c); n != 3 {
		t.Fatalf("events of the unmerged issue: %d", n)
	}
	if n := countRows(t, e, `SELECT count(*) FROM issue_tags WHERE issue_id = $1 AND key = 'customer'`, c); n != 1 {
		t.Fatalf("facets of the unmerged issue: %d", n)
	}
	if _, joined := sendEvent(t, e, "kalem", "b", as("4")); joined != c {
		t.Fatalf("new event after unmerge went to %s", joined)
	}
	if len(getIssue(t, owner, a.String()).GetMerged()) != 0 {
		t.Fatal("fingerprint still listed as merged")
	}
	if _, err := owner.issue.UnmergeIssue(ctx, req(&apiv1.UnmergeIssueRequest{IssueId: a.String(), Fingerprint: fingerprint})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("unmerge twice: %v", err)
	}
}

// TestGroupingRulesGroupNewEvents saves grouping rules, checks them and
// sees new events follow them.
func TestGroupingRulesGroupNewEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := setupKentos(t, e)
	eventID, before := sendEvent(t, e, "kalem", "x", func(*ingestv1.Event) {})

	p, err := owner.project.GetProject(ctx, req(&apiv1.GetProjectRequest{Slug: "kalem"}))
	if err != nil {
		t.Fatal(err)
	}
	settings := p.Msg.GetProject().GetSettings()
	settings.Grouping = &apiv1.GroupingSettings{FingerprintRules: "error.type:* -> hepsi\nyanlis satir"}
	if _, err := owner.project.UpdateProject(ctx, req(&apiv1.UpdateProjectRequest{Slug: "kalem", Settings: settings})); field(err) != "settings.grouping.fingerprint_rules" {
		t.Fatalf("invalid rules saved: %v", err)
	}
	check, err := owner.project.TestGroupingRules(ctx, req(&apiv1.TestGroupingRulesRequest{Project: "kalem", Grouping: settings.Grouping,
		EventId: eventID.String()}))
	if err != nil || len(check.Msg.GetProblems()) != 1 || check.Msg.GetProblems()[0].GetLine() != 2 || check.Msg.GetGrouping().GetStrategy() != "rule" ||
		check.Msg.GetSameIssue() || check.Msg.GetIssueShortId() != "" {
		t.Fatalf("check: %v %v", check, err)
	}
	settings.Grouping.FingerprintRules = "error.type:* -> hepsi"
	if _, err := owner.project.UpdateProject(ctx, req(&apiv1.UpdateProjectRequest{Slug: "kalem", Settings: settings})); err != nil {
		t.Fatal(err)
	}
	_, one := sendEvent(t, e, "kalem", "y", func(*ingestv1.Event) {})
	_, two := sendEvent(t, e, "kalem", "z", func(*ingestv1.Event) {})
	if one != two || one == before {
		t.Fatalf("rules did not group new events: %s %s (before %s)", one, two, before)
	}
	g := getIssue(t, owner, one.String()).GetGrouping()
	if g.GetStrategy() != "rule" || g.GetRule() != "error.type:* -> hepsi" {
		t.Fatalf("grouping: %v", g)
	}
	check, err = owner.project.TestGroupingRules(ctx, req(&apiv1.TestGroupingRulesRequest{Project: "kalem", Grouping: settings.Grouping,
		EventId: eventID.String()}))
	if err != nil || check.Msg.GetIssueShortId() != getIssue(t, owner, one.String()).GetSummary().GetShortId() || check.Msg.GetSameIssue() {
		t.Fatalf("check after saving: %v %v", check, err)
	}
}
