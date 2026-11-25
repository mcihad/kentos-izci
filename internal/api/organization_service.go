package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/store"
)

// OrganizationServer implements izci.api.v1.OrganizationService.
type OrganizationServer struct {
	*base
	apiv1connect.UnimplementedOrganizationServiceHandler
}

var _ apiv1connect.OrganizationServiceHandler = (*OrganizationServer)(nil)

// inOrg runs fn in the request tenant's scope.
func (b *base) inOrg(ctx context.Context, fn func(tx pgx.Tx, t *auth.Tenant) error) error {
	t := auth.TenantFrom(ctx)
	return b.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error { return fn(tx, t) })
}

func (b *base) audit(ctx context.Context, tx pgx.Tx, action, targetType, targetID, targetName string, meta map[string]string) error {
	t := auth.TenantFrom(ctx)
	p := auth.PrincipalFrom(ctx)
	e := store.AuditEntry{Action: action, TargetType: targetType, TargetID: targetID, TargetName: targetName, Metadata: meta, IP: infoFrom(ctx).IP}
	if t != nil {
		id := t.OrgID()
		e.OrgID = &id
	}
	if p != nil {
		id := p.User.ID
		e.ActorID = &id
	}
	return store.InsertAudit(ctx, tx, e)
}

func (s *OrganizationServer) counts(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (uint32, uint32, error) {
	var projects, members uint32
	err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM projects WHERE org_id = $1 AND deleted_at IS NULL),
		(SELECT count(*) FROM org_members WHERE org_id = $1)`, orgID).Scan(&projects, &members)
	return projects, members, err
}

// GetOrganization implements OrganizationService.GetOrganization.
func (s *OrganizationServer) GetOrganization(ctx context.Context, _ *connect.Request[apiv1.GetOrganizationRequest]) (*connect.Response[apiv1.GetOrganizationResponse], error) {
	var out apiv1.GetOrganizationResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, m, err := s.counts(ctx, tx, t.OrgID())
		out.Organization, out.Role = toOrganization(t.Org, p, m), t.Role
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get organization", err)
	}
	return connect.NewResponse(&out), nil
}

// CreateOrganization implements OrganizationService.CreateOrganization.
func (s *OrganizationServer) CreateOrganization(ctx context.Context, req *connect.Request[apiv1.CreateOrganizationRequest]) (*connect.Response[apiv1.CreateOrganizationResponse], error) {
	p := auth.PrincipalFrom(ctx)
	if !p.User.Superadmin && s.cfg.Auth.Signup != "open" {
		return nil, errPermission("only instance administrators can create organizations")
	}
	if s.auth.SuperadminNeedsTwoFactor(p.User) && s.cfg.Auth.Signup != "open" {
		return nil, errSuperadminTwoFactor()
	}
	// With open sign-up anyone may create organizations, up to a limit.
	if limit := s.cfg.Auth.SignupOrgLimit; limit > 0 && (!p.User.Superadmin || s.auth.SuperadminNeedsTwoFactor(p.User)) {
		var owned int
		err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
			var err error
			owned, err = store.CountOwnedOrganizations(ctx, tx, p.User.ID)
			return err
		})
		if err != nil {
			return nil, internal(ctx, s.log, "count organizations", err)
		}
		if owned >= limit {
			return nil, apiError(connect.CodeResourceExhausted, apiv1.ErrorReason_ERROR_REASON_LIMIT_REACHED,
				fmt.Sprintf("you can own at most %d organizations on this server", limit),
				"resource", "organizations", "limit", strconv.Itoa(limit))
		}
	}
	var org *store.Organization
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		var err error
		if org, err = store.CreateOrganization(ctx, tx, req.Msg.GetSlug(), req.Msg.GetName()); err != nil {
			return err
		}
		return store.AddMember(ctx, tx, org.ID, p.User.ID, apiv1.Role_ROLE_OWNER)
	})
	if store.UniqueViolation(err, "organizations_slug_key") {
		return nil, apiError(connect.CodeAlreadyExists, apiv1.ErrorReason_ERROR_REASON_SLUG_TAKEN, "organization slug is taken")
	}
	if err != nil {
		return nil, internal(ctx, s.log, "create organization", err)
	}
	return connect.NewResponse(&apiv1.CreateOrganizationResponse{Organization: toOrganization(org, 0, 1)}), nil
}

// UpdateOrganization implements OrganizationService.UpdateOrganization.
func (s *OrganizationServer) UpdateOrganization(ctx context.Context, req *connect.Request[apiv1.UpdateOrganizationRequest]) (*connect.Response[apiv1.UpdateOrganizationResponse], error) {
	m := req.Msg
	if tz := m.GetSettings().GetTimezone(); tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return nil, fieldError("settings.timezone", "unknown timezone")
		}
	}
	var out apiv1.UpdateOrganizationResponse
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		t := auth.TenantFrom(ctx)
		name := t.Org.Name
		if strings.TrimSpace(m.GetName()) != "" {
			name = strings.TrimSpace(m.GetName())
		}
		settings := t.Org.Settings
		if m.GetSettings() != nil {
			settings = proto.CloneOf(m.GetSettings())
			for i, d := range settings.AutoJoinDomains {
				settings.AutoJoinDomains[i] = strings.ToLower(strings.TrimSpace(d))
			}
		}
		// Requiring two-factor authentication without using it would lock
		// the caller out.
		before, after := t.Org.Settings.GetRequireTwoFactor(), settings.GetRequireTwoFactor()
		if after && !before && !auth.PrincipalFrom(ctx).User.TwoFactor() {
			return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_TWO_FACTOR_REQUIRED,
				"turn on two-factor authentication for your own account first")
		}
		if before && !after {
			// Lifting the requirement weakens every member's account.
			if err := requireRecentAuth(ctx); err != nil {
				return err
			}
		}
		org, err := store.UpdateOrganization(ctx, tx, t.OrgID(), name, settings)
		if err != nil {
			return err
		}
		p, mem, err := s.counts(ctx, tx, org.ID)
		out.Organization = toOrganization(org, p, mem)
		if err != nil {
			return err
		}
		meta := map[string]string{}
		if before != after {
			meta["require_two_factor"] = strconv.FormatBool(after)
		}
		// Who allowed sending the organization's data to the AI provider,
		// and when (KVKK).
		if was, is := t.Org.Settings.GetAiEnabled(), settings.GetAiEnabled(); was != is {
			meta["ai_enabled"] = strconv.FormatBool(is)
		}
		if len(meta) == 0 {
			meta = nil
		}
		return s.audit(ctx, tx, "organization.update", "organization", org.ID.String(), org.Name, meta)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update organization", err)
	}
	return connect.NewResponse(&out), nil
}

// ListMembers implements OrganizationService.ListMembers.
func (s *OrganizationServer) ListMembers(ctx context.Context, _ *connect.Request[apiv1.ListMembersRequest]) (*connect.Response[apiv1.ListMembersResponse], error) {
	var out apiv1.ListMembersResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		members, err := store.ListMembers(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		for _, m := range members {
			out.Members = append(out.Members, &apiv1.Member{User: toUserRef(m.User), Role: m.Role, JoinedAt: ts(m.JoinedAt),
				LastSeenAt: tsp(m.User.LastSeenAt), TwoFactorEnabled: m.User.TwoFactor()})
		}
		if t.Role < apiv1.Role_ROLE_ADMIN {
			return nil
		}
		invs, err := store.PendingInvitations(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		for _, inv := range invs {
			var inviter *store.User
			if inv.InvitedBy != nil {
				inviter, _ = store.GetUser(ctx, tx, *inv.InvitedBy)
			}
			out.Invitations = append(out.Invitations, toInvitation(inv, t.Org, inviter, ""))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list members", err)
	}
	return connect.NewResponse(&out), nil
}

// InviteMember implements OrganizationService.InviteMember.
func (s *OrganizationServer) InviteMember(ctx context.Context, req *connect.Request[apiv1.InviteMemberRequest]) (*connect.Response[apiv1.InviteMemberResponse], error) {
	t := auth.TenantFrom(ctx)
	if req.Msg.GetRole() == apiv1.Role_ROLE_OWNER && t.Role < apiv1.Role_ROLE_OWNER {
		return nil, errPermission("only owners can invite owners")
	}
	email := strings.ToLower(strings.TrimSpace(req.Msg.GetEmail()))
	var out apiv1.InviteMemberResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		if u, err := store.GetUserByEmail(ctx, tx, email); err == nil {
			if _, err := store.MemberRole(ctx, tx, t.OrgID(), u.ID); err == nil {
				return errAlreadyMember
			}
		}
		if limit := t.Org.Limits.GetMaxMembers(); limit > 0 {
			var n uint32
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1`, t.OrgID()).Scan(&n); err != nil {
				return err
			}
			if n >= limit {
				return errLimit
			}
		}
		p := auth.PrincipalFrom(ctx)
		inv, token, err := store.CreateInvitation(ctx, tx, t.OrgID(), email, req.Msg.GetRole(), p.User.ID, s.cfg.Auth.InvitationTTL)
		if err != nil {
			return err
		}
		out.Invitation = toInvitation(inv, t.Org, p.User, s.cfg.PublicURL+"/invite/"+token)
		return s.audit(ctx, tx, "invitation.create", "invitation", inv.ID.String(), email, map[string]string{"role": inv.Role.String()})
	})
	switch {
	case errors.Is(err, errAlreadyMember):
		return nil, apiError(connect.CodeAlreadyExists, apiv1.ErrorReason_ERROR_REASON_CONFLICT, "already a member")
	case errors.Is(err, errLimit):
		return nil, apiError(connect.CodeResourceExhausted, apiv1.ErrorReason_ERROR_REASON_LIMIT_REACHED, "member limit reached", "resource", "members")
	case err != nil:
		return nil, internal(ctx, s.log, "invite member", err)
	}
	return connect.NewResponse(&out), nil
}

var (
	errAlreadyMember = errors.New("already a member")
	errLimit         = errors.New("limit reached")
	errLastOwner     = errors.New("last owner")
)

// RevokeInvitation implements OrganizationService.RevokeInvitation.
func (s *OrganizationServer) RevokeInvitation(ctx context.Context, req *connect.Request[apiv1.RevokeInvitationRequest]) (*connect.Response[apiv1.RevokeInvitationResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetInvitationId())
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		if err := store.RevokeInvitation(ctx, tx, t.OrgID(), id); err != nil {
			return err
		}
		return s.audit(ctx, tx, "invitation.revoke", "invitation", id.String(), "", nil)
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound("invitation")
	}
	if err != nil {
		return nil, internal(ctx, s.log, "revoke invitation", err)
	}
	return connect.NewResponse(&apiv1.RevokeInvitationResponse{}), nil
}

// UpdateMember implements OrganizationService.UpdateMember.
func (s *OrganizationServer) UpdateMember(ctx context.Context, req *connect.Request[apiv1.UpdateMemberRequest]) (*connect.Response[apiv1.UpdateMemberResponse], error) {
	userID, _ := uuid.Parse(req.Msg.GetUserId())
	newRole := req.Msg.GetRole()
	var out apiv1.UpdateMemberResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		current, err := store.MemberRole(ctx, tx, t.OrgID(), userID)
		if err != nil {
			return err
		}
		if (current == apiv1.Role_ROLE_OWNER || newRole == apiv1.Role_ROLE_OWNER) && t.Role < apiv1.Role_ROLE_OWNER {
			return errPermission("only owners can change owners")
		}
		if current == apiv1.Role_ROLE_OWNER && newRole != apiv1.Role_ROLE_OWNER {
			if n, err := store.CountOwners(ctx, tx, t.OrgID()); err != nil {
				return err
			} else if n <= 1 {
				return errLastOwner
			}
		}
		if err := store.AddMember(ctx, tx, t.OrgID(), userID, newRole); err != nil {
			return err
		}
		u, err := store.GetUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		out.Member = &apiv1.Member{User: toUserRef(u), Role: newRole, LastSeenAt: tsp(u.LastSeenAt)}
		return s.audit(ctx, tx, "member.update", "user", userID.String(), u.Email, map[string]string{"role": newRole.String()})
	})
	return memberResult(ctx, s.base, "update member", err, &out)
}

func memberResult[T any](ctx context.Context, b *base, op string, err error, out *T) (*connect.Response[T], error) {
	var cerr *connect.Error
	switch {
	case errors.As(err, &cerr):
		return nil, cerr
	case errors.Is(err, store.ErrNotFound):
		return nil, errNotFound("member")
	case errors.Is(err, errLastOwner):
		return nil, apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_LAST_OWNER, "the organization needs at least one owner")
	case err != nil:
		return nil, internal(ctx, b.log, op, err)
	}
	return connect.NewResponse(out), nil
}

// RemoveMember implements OrganizationService.RemoveMember.
func (s *OrganizationServer) RemoveMember(ctx context.Context, req *connect.Request[apiv1.RemoveMemberRequest]) (*connect.Response[apiv1.RemoveMemberResponse], error) {
	userID, _ := uuid.Parse(req.Msg.GetUserId())
	p := auth.PrincipalFrom(ctx)
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		self := userID == p.User.ID
		if !self && t.Role < apiv1.Role_ROLE_ADMIN {
			return errPermission("requires the admin role")
		}
		current, err := store.MemberRole(ctx, tx, t.OrgID(), userID)
		if err != nil {
			return err
		}
		if current == apiv1.Role_ROLE_OWNER {
			if !self && t.Role < apiv1.Role_ROLE_OWNER {
				return errPermission("only owners can remove owners")
			}
			if n, err := store.CountOwners(ctx, tx, t.OrgID()); err != nil {
				return err
			} else if n <= 1 {
				return errLastOwner
			}
		}
		if err := store.RemoveMember(ctx, tx, t.OrgID(), userID); err != nil {
			return err
		}
		return s.audit(ctx, tx, "member.remove", "user", userID.String(), "", nil)
	})
	return memberResult(ctx, s.base, "remove member", err, &apiv1.RemoveMemberResponse{})
}

// GetUsage implements OrganizationService.GetUsage.
func (s *OrganizationServer) GetUsage(ctx context.Context, req *connect.Request[apiv1.GetUsageRequest]) (*connect.Response[apiv1.GetUsageResponse], error) {
	month := req.Msg.GetMonth()
	if month == "" {
		month = time.Now().UTC().Format("2006-01")
	}
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fieldError("month", "month must be YYYY-MM")
	}
	out := &apiv1.GetUsageResponse{Month: month}
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		out.Limits = t.Org.Limits
		rows, err := tx.Query(ctx, `
			SELECT day, accepted, rate_limited, filtered, ai_runs FROM usage_daily
			WHERE org_id = $1 AND day >= $2 AND day < $3 ORDER BY day`, t.OrgID(), start, start.AddDate(0, 1, 0))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var day time.Time
			var d apiv1.DailyUsage
			var ai int32
			if err := rows.Scan(&day, &d.Accepted, &d.RateLimited, &d.Filtered, &ai); err != nil {
				return err
			}
			d.Date = day.Format("2006-01-02")
			out.Days = append(out.Days, &d)
			out.EventsAccepted += d.Accepted
			out.EventsRateLimited += d.RateLimited
			out.EventsFiltered += d.Filtered
			out.AiRuns += uint32(ai)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get usage", err)
	}
	return connect.NewResponse(out), nil
}

// ListAuditLog implements OrganizationService.ListAuditLog.
func (s *OrganizationServer) ListAuditLog(ctx context.Context, req *connect.Request[apiv1.ListAuditLogRequest]) (*connect.Response[apiv1.ListAuditLogResponse], error) {
	limit := pageSize(req.Msg.GetPageSize())
	before, beforeID, hasCursor := decodeCursor(req.Msg.GetPageToken())
	out := &apiv1.ListAuditLogResponse{}
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		rows, err := tx.Query(ctx, `
			SELECT a.id, a.action, a.target_type, a.target_id, a.target_name, a.metadata, coalesce(host(a.ip_address), ''), a.created_at,
			       u.id, u.name, u.email, u.avatar_url
			FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
			WHERE a.org_id = $1 AND (NOT $2 OR (a.created_at, a.id) < ($3, $4))
			ORDER BY a.created_at DESC, a.id DESC LIMIT $5`, t.OrgID(), hasCursor, before, beforeID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e apiv1.AuditLogEntry
			var id uuid.UUID
			var created time.Time
			var meta map[string]string
			var uid *uuid.UUID
			var uname, uemail, uavatar *string
			if err := rows.Scan(&id, &e.Action, &e.TargetType, &e.TargetId, &e.TargetName, &meta, &e.IpAddress, &created,
				&uid, &uname, &uemail, &uavatar); err != nil {
				return err
			}
			e.Id, e.Metadata, e.CreatedAt = id.String(), meta, ts(created)
			if uid != nil {
				e.Actor = &apiv1.UserRef{Id: uid.String(), Name: deref(uname), Email: deref(uemail), AvatarUrl: deref(uavatar)}
			}
			if len(out.Entries) == limit {
				last := out.Entries[len(out.Entries)-1]
				lastID, _ := uuid.Parse(last.Id)
				out.NextPageToken = encodeCursor(last.CreatedAt.AsTime(), lastID)
				break
			}
			out.Entries = append(out.Entries, &e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list audit log", err)
	}
	return connect.NewResponse(out), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
