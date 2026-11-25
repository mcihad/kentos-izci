package api_test

import (
	"context"
	"strings"
	"testing"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
)

// TestPersonalNotifications checks the inbox: assignments, mentions and
// comments on followed issues, following, reading and settings.
func TestPersonalNotifications(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, issue := setupOrg(t, e)
	inv, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "mehmet@example.com", Role: apiv1.Role_ROLE_MEMBER}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	member := e.client(map[string]string{"Izci-Org": "kentos"})
	acc, err := member.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: url[strings.LastIndex(url, "/")+1:],
		Name: "Mehmet", Password: "mehmetin parolası"}))
	if err != nil {
		t.Fatal(err)
	}
	memberID := acc.Msg.GetSession().GetUser().GetId()
	inbox := func(c *client) *apiv1.ListNotificationsResponse {
		t.Helper()
		res, err := c.user.ListNotifications(ctx, req(&apiv1.ListNotificationsRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg
	}
	following := func(c *client) bool {
		t.Helper()
		res, err := c.issue.GetIssue(ctx, req(&apiv1.GetIssueRequest{Issue: issue}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg.GetIssue().GetFollowing()
	}
	comment := func(c *client, text string) {
		t.Helper()
		if _, err := c.issue.AddIssueComment(ctx, req(&apiv1.AddIssueCommentRequest{IssueId: issue, Text: text})); err != nil {
			t.Fatal(err)
		}
	}

	// The assignee hears about it and follows the issue.
	if _, err := owner.issue.UpdateIssues(ctx, req(&apiv1.UpdateIssuesRequest{IssueIds: []string{issue}, AssigneeId: &memberID})); err != nil {
		t.Fatal(err)
	}
	got := inbox(member)
	if got.GetUnreadCount() != 1 || len(got.GetNotifications()) != 1 {
		t.Fatalf("member inbox: %v", got)
	}
	n := got.GetNotifications()[0]
	if n.GetKind() != apiv1.UserNotificationKind_USER_NOTIFICATION_KIND_ASSIGNED || n.GetActor().GetEmail() != "owner@example.com" ||
		n.GetIssue().GetShortId() != "KALEM-1" || n.GetOrganization().GetSlug() != "kentos" || n.GetRead() {
		t.Fatalf("assignment: %v", n)
	}
	if len(inbox(owner).GetNotifications()) != 0 || !following(member) || following(owner) {
		t.Fatal("only the assignee is told and follows")
	}

	// Mentions reach the member named; the author follows from now on.
	comment(owner, "@mehmet bakar mısın?")
	if got := inbox(member); got.GetUnreadCount() != 2 ||
		got.GetNotifications()[0].GetKind() != apiv1.UserNotificationKind_USER_NOTIFICATION_KIND_MENTIONED ||
		got.GetNotifications()[0].GetText() != "@mehmet bakar mısın?" {
		t.Fatalf("mention: %v", got)
	}
	if !following(owner) {
		t.Fatal("the author must follow the issue")
	}
	// Followers hear about comments, the author does not.
	comment(member, "Bakıyorum")
	if got := inbox(owner); got.GetUnreadCount() != 1 ||
		got.GetNotifications()[0].GetKind() != apiv1.UserNotificationKind_USER_NOTIFICATION_KIND_COMMENTED {
		t.Fatalf("comment for a follower: %v", got)
	}
	if got := inbox(member); got.GetUnreadCount() != 2 {
		t.Fatalf("the author was told about their own comment: %v", got)
	}

	// Reading.
	first := inbox(member).GetNotifications()[0].GetId()
	read, err := member.user.MarkNotificationsRead(ctx, req(&apiv1.MarkNotificationsReadRequest{Ids: []string{first}}))
	if err != nil || read.Msg.GetUnreadCount() != 1 {
		t.Fatalf("mark one read: %v %v", read, err)
	}
	unread, err := member.user.ListNotifications(ctx, req(&apiv1.ListNotificationsRequest{UnreadOnly: true}))
	if err != nil || len(unread.Msg.GetNotifications()) != 1 {
		t.Fatalf("unread only: %v %v", unread, err)
	}
	if read, err := member.user.MarkNotificationsRead(ctx, req(&apiv1.MarkNotificationsReadRequest{All: true})); err != nil || read.Msg.GetUnreadCount() != 0 {
		t.Fatalf("mark all read: %v %v", read, err)
	}

	// Unfollowing stops comments.
	if res, err := owner.issue.FollowIssue(ctx, req(&apiv1.FollowIssueRequest{IssueId: issue, Follow: false})); err != nil || res.Msg.GetFollowing() {
		t.Fatalf("unfollow: %v %v", res, err)
	}
	comment(member, "Çözdüm")
	if got := inbox(owner); got.GetUnreadCount() != 1 {
		t.Fatalf("comment after unfollowing: %v", got)
	}

	// Settings: defaults, then a change.
	s, err := member.user.GetNotificationSettings(ctx, req(&apiv1.GetNotificationSettingsRequest{}))
	if err != nil || !s.Msg.GetSettings().GetAssigned().GetEmail() || s.Msg.GetSettings().GetCommented().GetEmail() ||
		s.Msg.GetEmailAvailable() || s.Msg.GetTelegramAvailable() {
		t.Fatalf("default settings: %v %v", s, err)
	}
	settings := s.Msg.GetSettings()
	settings.Assigned.Email, settings.Commented.Email = false, true
	if _, err := member.user.UpdateNotificationSettings(ctx, req(&apiv1.UpdateNotificationSettingsRequest{Settings: settings})); err != nil {
		t.Fatal(err)
	}
	s, err = member.user.GetNotificationSettings(ctx, req(&apiv1.GetNotificationSettingsRequest{}))
	if err != nil || s.Msg.GetSettings().GetAssigned().GetEmail() || !s.Msg.GetSettings().GetCommented().GetEmail() ||
		!s.Msg.GetSettings().GetAssigned().GetTelegram() {
		t.Fatalf("changed settings: %v %v", s, err)
	}
}
