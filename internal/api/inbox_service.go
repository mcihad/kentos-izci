package api

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
)

// inboxScope limits an access token bound to an organization to that
// organization's notifications.
func inboxScope(p *auth.Principal) *uuid.UUID {
	if p.Token != nil {
		return p.Token.OrgID
	}
	return nil
}

// ListNotifications implements UserService.ListNotifications.
func (s *UserServer) ListNotifications(ctx context.Context, req *connect.Request[apiv1.ListNotificationsRequest]) (*connect.Response[apiv1.ListNotificationsResponse], error) {
	p := auth.PrincipalFrom(ctx)
	limit := pageSize(req.Msg.GetPageSize())
	if req.Msg.GetPageSize() == 0 {
		limit = 30
	}
	var before *time.Time
	var beforeID uuid.UUID
	if at, id, ok := decodeCursor(req.Msg.GetPageToken()); ok {
		before, beforeID = &at, id
	}
	var entries []*store.InboxEntry
	var unread int
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if entries, err = store.ListInbox(ctx, tx, p.User.ID, inboxScope(p), req.Msg.GetUnreadOnly(), before, beforeID, limit+1); err != nil {
			return err
		}
		unread, err = store.UnreadCount(ctx, tx, p.User.ID, inboxScope(p))
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list notifications", err)
	}
	out := &apiv1.ListNotificationsResponse{UnreadCount: uint32(max(unread, 0))}
	if len(entries) > limit {
		entries = entries[:limit]
		last := entries[limit-1]
		out.NextPageToken = encodeCursor(last.CreatedAt, last.ID)
	}
	for _, e := range entries {
		out.Notifications = append(out.Notifications, toUserNotification(e))
	}
	return connect.NewResponse(out), nil
}

func toUserNotification(e *store.InboxEntry) *apiv1.UserNotification {
	n := &apiv1.UserNotification{
		Id: e.ID.String(), Kind: apiv1.UserNotificationKind(e.Kind),
		Organization: &apiv1.OrganizationRef{Id: e.OrgID.String(), Slug: e.OrgSlug, Name: e.OrgName},
		Issue:        &apiv1.IssueRef{Id: e.IssueID.String(), ShortId: ingest.ShortID(e.ProjectSlug, e.IssueSeq), Title: e.IssueTitle},
		Text:         e.Data["text"], CreatedAt: ts(e.CreatedAt), Read: e.ReadAt != nil,
	}
	if e.ActorID != nil {
		n.Actor = &apiv1.UserRef{Id: e.ActorID.String(), Name: e.ActorName, Email: e.ActorEmail}
	}
	// Why izci assigned the issue: the ownership rule, or the commit.
	if n.Text == "" && e.Kind == store.NotifyAssigned {
		if rule := e.Data["rule"]; rule != "" {
			n.Text = rule
		} else if sha := e.Data["commit"]; sha != "" {
			n.Text = "commit " + sha[:min(len(sha), 12)]
		}
	}
	return n
}

// MarkNotificationsRead implements UserService.MarkNotificationsRead.
func (s *UserServer) MarkNotificationsRead(ctx context.Context, req *connect.Request[apiv1.MarkNotificationsReadRequest]) (*connect.Response[apiv1.MarkNotificationsReadResponse], error) {
	p := auth.PrincipalFrom(ctx)
	var ids []uuid.UUID
	if !req.Msg.GetAll() {
		ids = []uuid.UUID{}
		for _, raw := range req.Msg.GetIds() {
			id, _ := uuid.Parse(raw)
			ids = append(ids, id)
		}
	}
	var unread int
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		if err := store.MarkRead(ctx, tx, p.User.ID, inboxScope(p), ids); err != nil {
			return err
		}
		var err error
		unread, err = store.UnreadCount(ctx, tx, p.User.ID, inboxScope(p))
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "mark notifications read", err)
	}
	return connect.NewResponse(&apiv1.MarkNotificationsReadResponse{UnreadCount: uint32(max(unread, 0))}), nil
}

// GetNotificationSettings implements UserService.GetNotificationSettings.
func (s *UserServer) GetNotificationSettings(ctx context.Context, _ *connect.Request[apiv1.GetNotificationSettingsRequest]) (*connect.Response[apiv1.GetNotificationSettingsResponse], error) {
	p := auth.PrincipalFrom(ctx)
	settings, err := store.GetNotificationSettings(ctx, s.db.Pool, p.User.ID)
	if err != nil {
		return nil, internal(ctx, s.log, "notification settings", err)
	}
	return connect.NewResponse(&apiv1.GetNotificationSettingsResponse{
		Settings:          settings,
		EmailAvailable:    s.inbox.EmailAvailable(),
		TelegramAvailable: s.inbox.TelegramAvailable() && p.User.TelegramUserID != nil,
	}), nil
}

// UpdateNotificationSettings implements UserService.UpdateNotificationSettings.
func (s *UserServer) UpdateNotificationSettings(ctx context.Context, req *connect.Request[apiv1.UpdateNotificationSettingsRequest]) (*connect.Response[apiv1.UpdateNotificationSettingsResponse], error) {
	p := auth.PrincipalFrom(ctx)
	if err := store.SetNotificationSettings(ctx, s.db.Pool, p.User.ID, req.Msg.GetSettings()); err != nil {
		return nil, internal(ctx, s.log, "update notification settings", err)
	}
	settings, err := store.GetNotificationSettings(ctx, s.db.Pool, p.User.ID)
	if err != nil {
		return nil, internal(ctx, s.log, "notification settings", err)
	}
	return connect.NewResponse(&apiv1.UpdateNotificationSettingsResponse{Settings: settings}), nil
}
