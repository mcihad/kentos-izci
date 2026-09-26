package api

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/store"
)

func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func tsp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func toUserRef(u *store.User) *apiv1.UserRef {
	if u == nil {
		return nil
	}
	return &apiv1.UserRef{Id: u.ID.String(), Name: u.Name, Email: u.Email, AvatarUrl: u.AvatarURL}
}

func toUser(u *store.User, providers []string) *apiv1.User {
	out := &apiv1.User{
		Id: u.ID.String(), Email: u.Email, Name: u.Name, AvatarUrl: u.AvatarURL, Superadmin: u.Superadmin,
		HasPassword: u.PasswordHash != nil, LinkedProviders: providers, CreatedAt: ts(u.CreatedAt),
	}
	if u.TelegramUserID != nil {
		out.Telegram = &apiv1.TelegramAccount{UserId: *u.TelegramUserID, Username: u.TelegramUsername, LinkedAt: tsp(u.TelegramLinkedAt)}
	}
	return out
}

func toOrgRef(o *store.Organization) *apiv1.OrganizationRef {
	if o == nil {
		return nil
	}
	return &apiv1.OrganizationRef{Id: o.ID.String(), Slug: o.Slug, Name: o.Name}
}

func toOrganization(o *store.Organization, projects, members uint32) *apiv1.Organization {
	return &apiv1.Organization{
		Id: o.ID.String(), Slug: o.Slug, Name: o.Name, Plan: o.Plan, Settings: o.Settings, Limits: o.Limits,
		ProjectCount: projects, MemberCount: members, CreatedAt: ts(o.CreatedAt),
	}
}

func toInvitation(i *store.Invitation, org *store.Organization, invitedBy *store.User, url string) *apiv1.Invitation {
	return &apiv1.Invitation{
		Id: i.ID.String(), Organization: toOrgRef(org), Email: i.Email, Role: i.Role, InvitedBy: toUserRef(invitedBy),
		CreatedAt: ts(i.CreatedAt), ExpiresAt: ts(i.ExpiresAt), Url: url,
	}
}

func toAccessToken(t *store.PersonalToken, org *store.Organization) *apiv1.AccessToken {
	return &apiv1.AccessToken{
		Id: t.ID.String(), Name: t.Name, Prefix: t.Prefix, Organization: toOrgRef(org), Scopes: t.Scopes,
		CreatedAt: ts(t.CreatedAt), LastUsedAt: tsp(t.LastUsedAt), ExpiresAt: tsp(t.ExpiresAt),
	}
}

func toSessionInfo(s *store.Session, current bool) *apiv1.SessionInfo {
	ua := ingest.ParseUserAgent(s.UserAgent)
	client := strings.TrimSpace(ua.BrowserLabel())
	if os := ua.OSLabel(); os != "" {
		if client != "" {
			client += " · "
		}
		client += os
	}
	if client == "" {
		client = truncate(s.UserAgent, 80)
	}
	out := &apiv1.SessionInfo{Id: s.ID.String(), Current: current, Client: client,
		CreatedAt: ts(s.CreatedAt), LastSeenAt: ts(s.LastSeenAt), ExpiresAt: ts(s.ExpiresAt)}
	if s.IPAddress != nil {
		out.IpAddress = s.IPAddress.String()
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// cursor encodes keyset pagination positions.
func encodeCursor(t time.Time, id uuid.UUID) string {
	b := make([]byte, 0, 32)
	b = t.UTC().AppendFormat(b, time.RFC3339Nano)
	b = append(b, '|')
	b = append(b, id.String()...)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (time.Time, uuid.UUID, bool) {
	if s == "" {
		return time.Time{}, uuid.Nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	ts, idStr, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, false
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return t, id, true
}

func pageSize(n uint32) int {
	if n == 0 {
		return 50
	}
	return min(int(n), 100)
}
