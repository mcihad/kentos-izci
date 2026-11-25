package api

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/store"
	"github.com/mcihad/kentos-izci/internal/version"
)

// AdminServer implements izci.api.v1.AdminService (superadmins only; the
// interceptor enforces it).
type AdminServer struct {
	*base
	apiv1connect.UnimplementedAdminServiceHandler

	// Features reports optional subsystems; other packages extend it as they
	// are wired (Telegram, AI, GitHub App, MCP).
	Features func() *apiv1.FeatureFlags
}

var _ apiv1connect.AdminServiceHandler = (*AdminServer)(nil)

// processStart approximates when the server process started.
var processStart = time.Now()

func (s *AdminServer) features() *apiv1.FeatureFlags {
	if s.Features != nil {
		return s.Features()
	}
	f := &apiv1.FeatureFlags{}
	for _, p := range s.cfg.OIDC {
		f.OidcProviders = append(f.OidcProviders, p.ID)
	}
	return f
}

// GetInstanceInfo implements AdminService.GetInstanceInfo.
func (s *AdminServer) GetInstanceInfo(ctx context.Context, _ *connect.Request[apiv1.GetInstanceInfoRequest]) (*connect.Response[apiv1.GetInstanceInfoResponse], error) {
	v := version.Get()
	out := &apiv1.GetInstanceInfoResponse{
		Version: v.Version, Commit: v.Commit, GoVersion: v.GoVersion, PublicUrl: s.cfg.PublicURL,
		Features: s.features(), StartedAt: ts(processStart),
	}
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		var events int64
		err := tx.QueryRow(ctx, `SELECT current_setting('server_version'),
			(SELECT count(*) FROM organizations)::int, (SELECT count(*) FROM users)::int,
			(SELECT coalesce(sum(count), 0) FROM project_hourly WHERE hour >= now() - interval '24 hours')::bigint`).Scan(
			&out.PostgresVersion, &out.Organizations, &out.Users, &events)
		out.Events_24H = uint64(events)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "instance info", err)
	}
	return connect.NewResponse(out), nil
}

func likePattern(q string) string {
	if q = strings.TrimSpace(q); q == "" {
		return ""
	}
	return "%" + likeEscape(q) + "%"
}

func offsetToken(token string) int {
	n, err := strconv.Atoi(token)
	if err != nil || n < 0 || n > 1_000_000 {
		return 0
	}
	return n
}

// ListOrganizations implements AdminService.ListOrganizations.
func (s *AdminServer) ListOrganizations(ctx context.Context, req *connect.Request[apiv1.ListOrganizationsRequest]) (*connect.Response[apiv1.ListOrganizationsResponse], error) {
	limit, offset := pageSize(req.Msg.GetPageSize()), offsetToken(req.Msg.GetPageToken())
	out := &apiv1.ListOrganizationsResponse{}
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		list, err := store.ListOrganizationStats(ctx, tx, likePattern(req.Msg.GetQuery()), limit+1, offset)
		if err != nil {
			return err
		}
		if len(list) > limit {
			list, out.NextPageToken = list[:limit], strconv.Itoa(offset+limit)
		}
		for _, o := range list {
			out.Organizations = append(out.Organizations, &apiv1.AdminOrganization{
				Organization: toOrganization(o.Org, uint32(o.Projects), uint32(o.Members)),
				MonthEvents:  uint64(o.MonthEvents), LastEventAt: tsp(o.LastEventAt),
			})
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "admin list organizations", err)
	}
	return connect.NewResponse(out), nil
}

// adminAudit records an instance-level action in the audit log of the
// affected organization (nil for user-level actions).
func (s *AdminServer) adminAudit(ctx context.Context, tx pgx.Tx, org *uuid.UUID, action, targetType, targetID, targetName string, meta map[string]string) error {
	p := auth.PrincipalFrom(ctx)
	e := store.AuditEntry{OrgID: org, Action: action, TargetType: targetType, TargetID: targetID, TargetName: targetName,
		Metadata: meta, IP: infoFrom(ctx).IP}
	if p != nil {
		e.ActorID = &p.User.ID
	}
	return store.InsertAudit(ctx, tx, e)
}

// UpdateOrganizationPlan implements AdminService.UpdateOrganizationPlan.
func (s *AdminServer) UpdateOrganizationPlan(ctx context.Context, req *connect.Request[apiv1.UpdateOrganizationPlanRequest]) (*connect.Response[apiv1.UpdateOrganizationPlanResponse], error) {
	id, _ := uuid.Parse(req.Msg.GetOrganizationId())
	var out apiv1.UpdateOrganizationPlanResponse
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		org, err := store.UpdateOrganizationPlan(ctx, tx, id, strings.TrimSpace(req.Msg.GetPlan()), req.Msg.GetLimits())
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound("organization")
		}
		if err != nil {
			return err
		}
		var projects, members uint32
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM projects WHERE org_id = $1 AND deleted_at IS NULL)::int,
			(SELECT count(*) FROM org_members WHERE org_id = $1)::int`, id).Scan(&projects, &members); err != nil {
			return err
		}
		out.Organization = toOrganization(org, projects, members)
		return s.adminAudit(ctx, tx, &id, "organization.plan_update", "organization", id.String(), org.Slug,
			map[string]string{"plan": org.Plan})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "admin update plan", err)
	}
	// Resolved ingest keys carry the organization limits.
	if s.keys != nil {
		s.keys.InvalidateAll()
	}
	return connect.NewResponse(&out), nil
}

func toAdminUser(u *store.UserStats) *apiv1.AdminUser {
	return &apiv1.AdminUser{
		User: toUser(u.User, u.Providers), Disabled: u.User.Disabled, LastLoginAt: tsp(u.User.LastLoginAt),
		OrganizationCount: uint32(max(u.Organizations, 0)),
	}
}

// ListUsers implements AdminService.ListUsers.
func (s *AdminServer) ListUsers(ctx context.Context, req *connect.Request[apiv1.ListUsersRequest]) (*connect.Response[apiv1.ListUsersResponse], error) {
	limit, offset := pageSize(req.Msg.GetPageSize()), offsetToken(req.Msg.GetPageToken())
	out := &apiv1.ListUsersResponse{}
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		list, err := store.ListUserStats(ctx, tx, likePattern(req.Msg.GetQuery()), limit+1, offset)
		if err != nil {
			return err
		}
		if len(list) > limit {
			list, out.NextPageToken = list[:limit], strconv.Itoa(offset+limit)
		}
		for _, u := range list {
			out.Users = append(out.Users, toAdminUser(u))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "admin list users", err)
	}
	return connect.NewResponse(out), nil
}

// UpdateUser implements AdminService.UpdateUser.
func (s *AdminServer) UpdateUser(ctx context.Context, req *connect.Request[apiv1.UpdateUserRequest]) (*connect.Response[apiv1.UpdateUserResponse], error) {
	m := req.Msg
	id, _ := uuid.Parse(m.GetUserId())
	self := auth.PrincipalFrom(ctx).User.ID == id
	if self && ((m.Superadmin != nil && !m.GetSuperadmin()) || m.GetDisabled()) {
		return nil, apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_CONFLICT,
			"you cannot remove your own superadmin rights or disable yourself")
	}
	var out apiv1.UpdateUserResponse
	resetEmail := ""
	err := s.db.InSystem(ctx, func(tx pgx.Tx) error {
		// Serialize superadmin changes so two admins cannot demote each
		// other concurrently and leave the instance without one.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('izci.superadmins'))`); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE users SET superadmin = coalesce($2, superadmin), disabled = coalesce($3, disabled), updated_at = now()
			WHERE id = $1`, id, m.Superadmin, m.Disabled)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound("user")
		}
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE superadmin AND NOT disabled`).Scan(&admins); err != nil {
			return err
		}
		if admins == 0 {
			return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_CONFLICT, "at least one active superadmin is required")
		}
		if m.GetDisabled() {
			// Disabled accounts are rejected at authentication; dropping
			// their sessions also ends open browser tabs immediately.
			if _, err := store.DeleteUserSessions(ctx, tx, id); err != nil {
				return err
			}
		}
		if m.GetResetTwoFactor() {
			if err := store.DisableTwoFactor(ctx, tx, id); err != nil {
				return err
			}
			// Sessions that passed the second step end with it; an
			// administrator resetting their own keeps the current one.
			if p := auth.PrincipalFrom(ctx); self && p.Session != nil {
				_, err = store.DeleteOtherSessions(ctx, tx, id, p.Session.ID)
			} else {
				_, err = store.DeleteUserSessions(ctx, tx, id)
			}
			if err != nil {
				return err
			}
		}
		u, err := store.UserStatsByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if m.GetResetTwoFactor() {
			resetEmail = u.User.Email
		}
		out.User = toAdminUser(u)
		meta := map[string]string{}
		if m.Superadmin != nil {
			meta["superadmin"] = strconv.FormatBool(m.GetSuperadmin())
		}
		if m.Disabled != nil {
			meta["disabled"] = strconv.FormatBool(m.GetDisabled())
		}
		if m.GetResetTwoFactor() {
			meta["reset_two_factor"] = "true"
		}
		return s.adminAudit(ctx, tx, nil, "user.admin_update", "user", id.String(), u.User.Email, meta)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "admin update user", err)
	}
	if resetEmail != "" {
		s.account.TwoFactorDisabled(ctx, resetEmail, true, time.Now())
	}
	return connect.NewResponse(&out), nil
}
