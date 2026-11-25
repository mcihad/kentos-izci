package api

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/github"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/ingest/sentry"
	"github.com/mcihad/kentos-izci/internal/sample"
	"github.com/mcihad/kentos-izci/internal/slug"
	"github.com/mcihad/kentos-izci/internal/store"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// ProjectServer implements izci.api.v1.ProjectService.
type ProjectServer struct {
	*base
	apiv1connect.UnimplementedProjectServiceHandler
}

var _ apiv1connect.ProjectServiceHandler = (*ProjectServer)(nil)

// project loads a project of the tenant by slug.
func (s *ProjectServer) project(ctx context.Context, tx pgx.Tx, t *auth.Tenant, projectSlug string) (*store.Project, error) {
	p, err := store.GetProjectBySlug(ctx, tx, t.OrgID(), strings.ToLower(projectSlug))
	if errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound("project")
	}
	return p, err
}

func toProject(p *store.Project) *apiv1.Project {
	return &apiv1.Project{
		Id: p.ID.String(), Slug: p.Slug, Name: p.Name, Platform: p.Platform, PublicId: uint64(p.PublicID),
		Settings: p.Settings, CreatedAt: ts(p.CreatedAt),
	}
}

// decorate attaches statistics and repositories to projects.
func (s *ProjectServer) decorate(ctx context.Context, tx pgx.Tx, t *auth.Tenant, list []*store.Project) ([]*apiv1.Project, error) {
	out := make([]*apiv1.Project, len(list))
	byID := make(map[uuid.UUID]*apiv1.Project, len(list))
	ids := make([]uuid.UUID, len(list))
	w := periodWindow(apiv1.StatsPeriod_STATS_PERIOD_LAST_24_HOURS, time.Now(), orgLocation(t.Org))
	counts := make(map[uuid.UUID][]uint64, len(list))
	for i, p := range list {
		out[i] = toProject(p)
		out[i].Stats = &apiv1.ProjectStats{LastEventAt: tsp(p.LastEventAt)}
		byID[p.ID], ids[i] = out[i], p.ID
		counts[p.ID] = make([]uint64, w.buckets)
	}
	if len(list) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT project_id, count(*) FROM issues WHERE project_id = ANY($1) AND status = 1 GROUP BY 1`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		byID[id].Stats.UnresolvedIssues = uint32(n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `SELECT project_id, hour, sum(count)::bigint FROM project_hourly
		WHERE project_id = ANY($1) AND hour >= $2 GROUP BY 1, 2`, ids, w.start)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var hour time.Time
		var n int64
		if err := rows.Scan(&id, &hour, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if i := w.bucketIndex(hour); i >= 0 {
			counts[id][i] += uint64(n)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, c := range counts {
		byID[id].Stats.Series_24H, byID[id].Stats.Events_24H = w.series(c), sum(c)
	}
	repos, err := loadRepositories(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for id, r := range repos {
		byID[id].Repository = r
	}
	return out, nil
}

func loadRepositories(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]*apiv1.ProjectRepository, error) {
	rows, err := tx.Query(ctx, `
		SELECT project_id, integration_id, provider, owner, name, url, default_branch, path_mappings, linked_at
		FROM project_repositories WHERE project_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]*apiv1.ProjectRepository{}
	for rows.Next() {
		var (
			id          uuid.UUID
			integration *uuid.UUID
			mappings    []byte
			linked      time.Time
			r           apiv1.ProjectRepository
		)
		if err := rows.Scan(&id, &integration, &r.Provider, &r.Owner, &r.Name, &r.Url, &r.DefaultBranch, &mappings, &linked); err != nil {
			return nil, err
		}
		if integration != nil {
			r.IntegrationId = integration.String()
		}
		r.LinkedAt = ts(linked)
		var pm []*apiv1.PathMapping
		_ = json.Unmarshal(mappings, &pm)
		r.PathMappings = pm
		out[id] = &r
	}
	return out, rows.Err()
}

// ListProjects implements ProjectService.ListProjects.
func (s *ProjectServer) ListProjects(ctx context.Context, _ *connect.Request[apiv1.ListProjectsRequest]) (*connect.Response[apiv1.ListProjectsResponse], error) {
	var out apiv1.ListProjectsResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		list, err := store.ListProjects(ctx, tx, t.OrgID())
		if err != nil {
			return err
		}
		out.Projects, err = s.decorate(ctx, tx, t, list)
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list projects", err)
	}
	return connect.NewResponse(&out), nil
}

// GetProject implements ProjectService.GetProject.
func (s *ProjectServer) GetProject(ctx context.Context, req *connect.Request[apiv1.GetProjectRequest]) (*connect.Response[apiv1.GetProjectResponse], error) {
	var out apiv1.GetProjectResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetSlug())
		if err != nil {
			return err
		}
		list, err := s.decorate(ctx, tx, t, []*store.Project{p})
		if err != nil {
			return err
		}
		out.Project = list[0]
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get project", err)
	}
	return connect.NewResponse(&out), nil
}

// CreateProject implements ProjectService.CreateProject.
func (s *ProjectServer) CreateProject(ctx context.Context, req *connect.Request[apiv1.CreateProjectRequest]) (*connect.Response[apiv1.CreateProjectResponse], error) {
	m := req.Msg
	p := auth.PrincipalFrom(ctx)
	projectSlug := m.GetSlug()
	if projectSlug == "" {
		if projectSlug = slug.Make(m.GetName(), 50); projectSlug == "" {
			return nil, fieldError("slug", "slug cannot be derived from the name")
		}
	}
	var out apiv1.CreateProjectResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		if max := t.Org.Limits.GetMaxProjects(); max > 0 {
			var n uint32
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM projects WHERE org_id = $1 AND deleted_at IS NULL`, t.OrgID()).Scan(&n); err != nil {
				return err
			}
			if n >= max {
				return apiError(connect.CodeResourceExhausted, apiv1.ErrorReason_ERROR_REASON_LIMIT_REACHED,
					"project limit of the organization reached", "limit", strconv.Itoa(int(max)))
			}
		}
		project, err := store.CreateProject(ctx, tx, t.OrgID(), projectSlug, strings.TrimSpace(m.GetName()), strings.ToLower(m.GetPlatform()))
		if store.UniqueViolation(err, "projects_org_slug_key") {
			return apiError(connect.CodeAlreadyExists, apiv1.ErrorReason_ERROR_REASON_SLUG_TAKEN, "project slug is taken")
		}
		if err != nil {
			return err
		}
		key, secret, err := store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{
			OrgID: t.OrgID(), ProjectID: project.ID, Name: "default", CreatedBy: &p.User.ID,
			// The first key ends up in applications, often in browsers:
			// it only sends events. Uploads need a separate CI key.
			Scopes: []apiv1.ApiKeyScope{apiv1.ApiKeyScope_API_KEY_SCOPE_INGEST},
		})
		if err != nil {
			return err
		}
		list, err := s.decorate(ctx, tx, t, []*store.Project{project})
		if err != nil {
			return err
		}
		out.Project, out.ApiKey, out.ApiKeySecret = list[0], toAPIKey(key, toUserRef(p.User)), secret
		return s.audit(ctx, tx, "project.create", "project", project.ID.String(), project.Slug, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "create project", err)
	}
	return connect.NewResponse(&out), nil
}

// UpdateProject implements ProjectService.UpdateProject.
func (s *ProjectServer) UpdateProject(ctx context.Context, req *connect.Request[apiv1.UpdateProjectRequest]) (*connect.Response[apiv1.UpdateProjectResponse], error) {
	m := req.Msg
	var out apiv1.UpdateProjectResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, m.GetSlug())
		if err != nil {
			return err
		}
		name, platform, settings := p.Name, p.Platform, p.Settings
		if v := strings.TrimSpace(m.GetName()); v != "" {
			name = v
		}
		if v := strings.ToLower(strings.TrimSpace(m.GetPlatform())); v != "" {
			platform = v
		}
		if m.GetSettings() != nil {
			settings = normalizeProjectSettings(m.GetSettings())
			if err := validateFilters(settings.GetFilters()); err != nil {
				return err
			}
		}
		p, err = store.UpdateProject(ctx, tx, p.ID, name, platform, settings)
		if err != nil {
			return err
		}
		list, err := s.decorate(ctx, tx, t, []*store.Project{p})
		if err != nil {
			return err
		}
		out.Project = list[0]
		return s.audit(ctx, tx, "project.update", "project", p.ID.String(), p.Slug, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update project", err)
	}
	s.invalidateKeys()
	return connect.NewResponse(&out), nil
}

// normalizeProjectSettings fills defaults for unset enums and lower-cases
// list entries that are matched case-insensitively.
func normalizeProjectSettings(in *apiv1.ProjectSettings) *apiv1.ProjectSettings {
	d := store.DefaultProjectSettings()
	if in.Privacy == nil {
		in.Privacy = d.Privacy
	}
	if in.Privacy.IpStorage == apiv1.IpStorage_IP_STORAGE_UNSPECIFIED {
		in.Privacy.IpStorage = apiv1.IpStorage_IP_STORAGE_FULL
	}
	if in.InApp == nil {
		in.InApp = d.InApp
	}
	if in.Ai == nil {
		in.Ai = d.Ai
	}
	if in.Ai.AutoAnalyze == apiv1.AutoAnalyze_AUTO_ANALYZE_UNSPECIFIED {
		in.Ai.AutoAnalyze = apiv1.AutoAnalyze_AUTO_ANALYZE_OFF
	}
	switch in.Ai.AutoAction {
	case apiv1.AutoAction_AUTO_ACTION_UNSPECIFIED:
		in.Ai.AutoAction = apiv1.AutoAction_AUTO_ACTION_NONE
	case apiv1.AutoAction_AUTO_ACTION_PULL_REQUEST:
		// Automatic pull requests are always drafts.
		in.Ai.AutoAction = apiv1.AutoAction_AUTO_ACTION_DRAFT_PULL_REQUEST
	}
	if in.Ai.MinLevel == ingestv1.Level_LEVEL_UNSPECIFIED {
		in.Ai.MinLevel = ingestv1.Level_LEVEL_ERROR
	}
	envs := in.IgnoredEnvironments[:0]
	for _, e := range in.IgnoredEnvironments {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			envs = append(envs, e)
		}
	}
	in.IgnoredEnvironments = envs
	if in.Filters == nil {
		in.Filters = &apiv1.InboundFilters{}
	}
	f := in.Filters
	f.ErrorMessages, f.IpAddresses, f.Releases = cleanList(f.ErrorMessages), cleanList(f.IpAddresses), cleanList(f.Releases)
	return in
}

// cleanList trims entries and drops empty and repeated ones.
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// validateFilters rejects IP entries that are not addresses or ranges.
func validateFilters(f *apiv1.InboundFilters) error {
	for _, s := range f.GetIpAddresses() {
		if _, err := ingest.ParseIPFilter(s); err != nil {
			return fieldError("settings.filters.ip_addresses", "not an IP address or CIDR range: "+s)
		}
	}
	for _, list := range [][]string{f.GetErrorMessages(), f.GetReleases()} {
		for _, s := range list {
			if _, err := ingest.Glob(s); err != nil {
				return fieldError("settings.filters", "invalid pattern: "+s)
			}
		}
	}
	return nil
}

func (s *ProjectServer) invalidateKeys() {
	if s.keys != nil {
		s.keys.InvalidateAll()
	}
}

// DeleteProject implements ProjectService.DeleteProject.
func (s *ProjectServer) DeleteProject(ctx context.Context, req *connect.Request[apiv1.DeleteProjectRequest]) (*connect.Response[apiv1.DeleteProjectResponse], error) {
	if req.Msg.GetConfirmSlug() != req.Msg.GetSlug() {
		return nil, fieldError("confirm_slug", "confirmation does not match the project slug")
	}
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetSlug())
		if err != nil {
			return err
		}
		if err := store.MarkProjectDeleted(ctx, tx, p.ID); err != nil {
			return err
		}
		return s.audit(ctx, tx, "project.delete", "project", p.ID.String(), p.Slug, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "delete project", err)
	}
	s.invalidateKeys()
	return connect.NewResponse(&apiv1.DeleteProjectResponse{}), nil
}

func toAPIKey(k *store.APIKey, createdBy *apiv1.UserRef) *apiv1.ApiKey {
	return &apiv1.ApiKey{
		Id: k.ID.String(), Name: k.Name, Prefix: k.Prefix, Scopes: k.Scopes, AllowedOrigins: k.AllowedOrigins,
		RateLimitPerMinute: uint32(max(k.RateLimitPerMinute, 0)), Enabled: k.Usable(), CreatedBy: createdBy,
		CreatedAt: ts(k.CreatedAt), LastUsedAt: tsp(k.LastUsedAt), BrowserSeenAt: tsp(k.BrowserSeenAt),
	}
}

// userRefs loads users by id.
func userRefs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]*apiv1.UserRef, error) {
	out := map[uuid.UUID]*apiv1.UserRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, name, email, avatar_url FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var u apiv1.UserRef
		if err := rows.Scan(&id, &u.Name, &u.Email, &u.AvatarUrl); err != nil {
			return nil, err
		}
		u.Id = id.String()
		out[id] = &u
	}
	return out, rows.Err()
}

// ListApiKeys implements ProjectService.ListApiKeys.
func (s *ProjectServer) ListApiKeys(ctx context.Context, req *connect.Request[apiv1.ListApiKeysRequest]) (*connect.Response[apiv1.ListApiKeysResponse], error) {
	var out apiv1.ListApiKeysResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		keys, err := store.ListAPIKeys(ctx, tx, p.ID, req.Msg.GetIncludeRevoked())
		if err != nil {
			return err
		}
		var creators []uuid.UUID
		for _, k := range keys {
			if k.CreatedBy != nil {
				creators = append(creators, *k.CreatedBy)
			}
		}
		users, err := userRefs(ctx, tx, creators)
		if err != nil {
			return err
		}
		for _, k := range keys {
			var by *apiv1.UserRef
			if k.CreatedBy != nil {
				by = users[*k.CreatedBy]
			}
			out.Keys = append(out.Keys, toAPIKey(k, by))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list api keys", err)
	}
	return connect.NewResponse(&out), nil
}

// CreateApiKey implements ProjectService.CreateApiKey.
func (s *ProjectServer) CreateApiKey(ctx context.Context, req *connect.Request[apiv1.CreateApiKeyRequest]) (*connect.Response[apiv1.CreateApiKeyResponse], error) {
	m := req.Msg
	user := auth.PrincipalFrom(ctx).User
	origins, err := normalizeOrigins(m.GetAllowedOrigins())
	if err != nil {
		return nil, err
	}
	var out apiv1.CreateApiKeyResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, m.GetProject())
		if err != nil {
			return err
		}
		k, secret, err := store.CreateAPIKey(ctx, tx, store.NewAPIKeyInput{
			OrgID: t.OrgID(), ProjectID: p.ID, Name: strings.TrimSpace(m.GetName()), Scopes: uniqueScopes(m.GetScopes()),
			AllowedOrigins: origins, RateLimitPerMinute: int32(min(m.GetRateLimitPerMinute(), 1<<30)), CreatedBy: &user.ID,
		})
		if err != nil {
			return err
		}
		out.Key, out.Secret, out.SentryDsn = toAPIKey(k, toUserRef(user)), secret, sentry.DSN(s.cfg.PublicURL, secret, p.PublicID)
		return s.audit(ctx, tx, "api_key.create", "api_key", k.ID.String(), k.Name, map[string]string{"project": p.Slug})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "create api key", err)
	}
	return connect.NewResponse(&out), nil
}

func uniqueScopes(in []apiv1.ApiKeyScope) []apiv1.ApiKeyScope {
	var out []apiv1.ApiKeyScope
	for _, s := range in {
		if s != apiv1.ApiKeyScope_API_KEY_SCOPE_UNSPECIFIED && !containsScope(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func containsScope(list []apiv1.ApiKeyScope, s apiv1.ApiKeyScope) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// normalizeOrigins validates browser origins ("https://host[:port]").
func normalizeOrigins(in []string) ([]string, error) {
	out := []string{}
	for i, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "*" {
			out = append(out, raw)
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return nil, fieldError("allowed_origins["+strconv.Itoa(i)+"]", "origin must look like https://app.example.com")
		}
		out = append(out, strings.ToLower(u.Scheme+"://"+u.Host))
	}
	return out, nil
}

// UpdateApiKey implements ProjectService.UpdateApiKey.
func (s *ProjectServer) UpdateApiKey(ctx context.Context, req *connect.Request[apiv1.UpdateApiKeyRequest]) (*connect.Response[apiv1.UpdateApiKeyResponse], error) {
	m := req.Msg
	origins, err := normalizeOrigins(m.GetAllowedOrigins())
	if err != nil {
		return nil, err
	}
	keyID, _ := uuid.Parse(m.GetKeyId())
	var out apiv1.UpdateApiKeyResponse
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, m.GetProject())
		if err != nil {
			return err
		}
		var createdBy *uuid.UUID
		var k store.APIKey
		var scopes []int16
		err = tx.QueryRow(ctx, `
			UPDATE api_keys SET name = CASE WHEN $3 = '' THEN name ELSE $3 END, allowed_origins = $4,
			  rate_limit_per_minute = $5, enabled = $6
			WHERE id = $1 AND project_id = $2 AND revoked_at IS NULL
			RETURNING id, name, prefix, scopes, allowed_origins, rate_limit_per_minute, enabled, created_by, created_at, last_used_at`,
			keyID, p.ID, strings.TrimSpace(m.GetName()), origins, int32(min(m.GetRateLimitPerMinute(), 1<<30)), m.GetEnabled()).Scan(
			&k.ID, &k.Name, &k.Prefix, &scopes, &k.AllowedOrigins, &k.RateLimitPerMinute, &k.Enabled, &createdBy, &k.CreatedAt, &k.LastUsedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound("api key")
		}
		if err != nil {
			return err
		}
		for _, sc := range scopes {
			k.Scopes = append(k.Scopes, apiv1.ApiKeyScope(sc))
		}
		var by *apiv1.UserRef
		if createdBy != nil {
			users, err := userRefs(ctx, tx, []uuid.UUID{*createdBy})
			if err != nil {
				return err
			}
			by = users[*createdBy]
		}
		out.Key = toAPIKey(&k, by)
		return s.audit(ctx, tx, "api_key.update", "api_key", k.ID.String(), k.Name, map[string]string{"project": p.Slug})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "update api key", err)
	}
	s.invalidateKeys()
	return connect.NewResponse(&out), nil
}

// RevokeApiKey implements ProjectService.RevokeApiKey.
func (s *ProjectServer) RevokeApiKey(ctx context.Context, req *connect.Request[apiv1.RevokeApiKeyRequest]) (*connect.Response[apiv1.RevokeApiKeyResponse], error) {
	keyID, _ := uuid.Parse(req.Msg.GetKeyId())
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		if err := store.RevokeAPIKey(ctx, tx, p.ID, keyID); errors.Is(err, store.ErrNotFound) {
			return errNotFound("api key")
		} else if err != nil {
			return err
		}
		return s.audit(ctx, tx, "api_key.revoke", "api_key", keyID.String(), "", map[string]string{"project": p.Slug})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "revoke api key", err)
	}
	s.invalidateKeys()
	return connect.NewResponse(&apiv1.RevokeApiKeyResponse{}), nil
}

// GetIngestEndpoints implements ProjectService.GetIngestEndpoints.
func (s *ProjectServer) GetIngestEndpoints(ctx context.Context, req *connect.Request[apiv1.GetIngestEndpointsRequest]) (*connect.Response[apiv1.GetIngestEndpointsResponse], error) {
	var out apiv1.GetIngestEndpointsResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		base := strings.TrimRight(s.cfg.PublicURL, "/")
		out.BaseUrl, out.PublicId = base, uint64(p.PublicID)
		out.SentryDsnTemplate = sentry.DSN(base, "<key>", p.PublicID)
		if u, err := url.Parse(base); err == nil {
			out.GrpcTls = u.Scheme == "https"
			port := u.Port()
			if port == "" {
				port = "80"
				if out.GrpcTls {
					port = "443"
				}
			}
			out.GrpcTarget = u.Hostname() + ":" + port
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "ingest endpoints", err)
	}
	return connect.NewResponse(&out), nil
}

// testEvent builds the event sent by the "send test event" button: a
// realistic stack trace, grouped into a single issue per project.
func testEvent() *ingestv1.Event {
	r := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	ev := sample.Event(r, sample.Options{Template: 0})
	ev.Environment = "production"
	ev.Fingerprint = []string{"izci-test-event"}
	ev.Tags["izci_test"] = "true"
	ev.Exceptions[0].Type = "IzciTestException"
	ev.Exceptions[0].Value = "Bu bir test olayıdır: İzci kurulumu çalışıyor."
	return ev
}

// SendTestEvent implements ProjectService.SendTestEvent.
func (s *ProjectServer) SendTestEvent(ctx context.Context, req *connect.Request[apiv1.SendTestEventRequest]) (*connect.Response[apiv1.SendTestEventResponse], error) {
	if s.pipeline == nil {
		return nil, apiError(connect.CodeUnavailable, apiv1.ErrorReason_ERROR_REASON_INTERNAL, "ingest pipeline is not running")
	}
	t := auth.TenantFrom(ctx)
	var p *store.Project
	err := s.db.InOrg(ctx, t.OrgID(), func(tx pgx.Tx) error {
		var err error
		p, err = s.project(ctx, tx, t, req.Msg.GetProject())
		return err
	})
	if err != nil {
		return nil, internal(ctx, s.log, "send test event", err)
	}
	processed, err := ingest.Normalize(testEvent(), ingest.NormalizeOptions{
		Project: ingest.ProjectContext{OrgID: t.OrgID(), OrgSlug: t.Org.Slug, ProjectID: p.ID, ProjectSlug: p.Slug,
			Platform: p.Platform, Settings: p.Settings},
		Retention: time.Duration(s.cfg.Retention.Days) * 24 * time.Hour,
	})
	if err != nil {
		return nil, internal(ctx, s.log, "send test event", err)
	}
	chans, err := s.pipeline.Submit(processed)
	if err != nil {
		return nil, apiError(connect.CodeUnavailable, apiv1.ErrorReason_ERROR_REASON_INTERNAL, "ingest is busy, try again")
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case r := <-chans[0]:
		if r.Err != nil {
			return nil, internal(ctx, s.log, "send test event", r.Err)
		}
		return connect.NewResponse(&apiv1.SendTestEventResponse{
			EventId: processed.ID.String(), IssueId: r.IssueID.String(), IssueShortId: r.IssueShortID,
		}), nil
	case <-timer.C:
		return connect.NewResponse(&apiv1.SendTestEventResponse{EventId: processed.ID.String()}), nil
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	}
}

// ListEnvironments implements ProjectService.ListEnvironments.
func (s *ProjectServer) ListEnvironments(ctx context.Context, req *connect.Request[apiv1.ListEnvironmentsRequest]) (*connect.Response[apiv1.ListEnvironmentsResponse], error) {
	var out apiv1.ListEnvironmentsResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		var rows pgx.Rows
		var err error
		if req.Msg.GetProject() != "" {
			p, err := s.project(ctx, tx, t, req.Msg.GetProject())
			if err != nil {
				return err
			}
			rows, err = tx.Query(ctx, `SELECT name, first_seen, last_seen FROM environments WHERE project_id = $1 ORDER BY name`, p.ID)
			if err != nil {
				return err
			}
		} else {
			rows, err = tx.Query(ctx, `SELECT e.name, min(e.first_seen), max(e.last_seen) FROM environments e
				JOIN projects p ON p.id = e.project_id AND p.deleted_at IS NULL
				WHERE e.org_id = $1 GROUP BY e.name ORDER BY e.name`, t.OrgID())
			if err != nil {
				return err
			}
		}
		defer rows.Close()
		for rows.Next() {
			var e apiv1.Environment
			var first, last time.Time
			if err := rows.Scan(&e.Name, &first, &last); err != nil {
				return err
			}
			e.FirstSeen, e.LastSeen = ts(first), ts(last)
			out.Environments = append(out.Environments, &e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list environments", err)
	}
	return connect.NewResponse(&out), nil
}

// ListReleases implements ProjectService.ListReleases.
func (s *ProjectServer) ListReleases(ctx context.Context, req *connect.Request[apiv1.ListReleasesRequest]) (*connect.Response[apiv1.ListReleasesResponse], error) {
	limit := pageSize(req.Msg.GetPageSize())
	before, beforeID, has := decodeCursor(req.Msg.GetPageToken())
	var out apiv1.ListReleasesResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT id, version, commit_sha, repository, ref, url, first_seen, last_seen, new_issues
			FROM releases WHERE project_id = $1 AND (NOT $2 OR (first_seen, id) < ($3, $4))
			ORDER BY first_seen DESC, id DESC LIMIT $5`, p.ID, has, before, beforeID, limit+1)
		if err != nil {
			return err
		}
		byID := map[uuid.UUID]*apiv1.Release{}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			var first, last time.Time
			var newIssues int32
			var r apiv1.Release
			if err := rows.Scan(&id, &r.Version, &r.CommitSha, &r.Repository, &r.Ref, &r.Url, &first, &last, &newIssues); err != nil {
				rows.Close()
				return err
			}
			r.Id, r.FirstSeen, r.LastSeen, r.NewIssues = id.String(), ts(first), ts(last), uint32(max(newIssues, 0))
			if len(out.Releases) == limit {
				prev := out.Releases[len(out.Releases)-1]
				pid, _ := uuid.Parse(prev.Id)
				out.NextPageToken = encodeCursor(prev.FirstSeen.AsTime(), pid)
				break
			}
			out.Releases = append(out.Releases, &r)
			byID[id] = &r
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := releaseHealth(ctx, tx, p.ID, out.Releases, req.Msg.GetHealthPeriod()); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT release_id, environment, deployed_at, deployed_by FROM (
			  SELECT d.*, row_number() OVER (PARTITION BY release_id ORDER BY deployed_at DESC) AS n
			  FROM deploys d WHERE release_id = ANY($1)
			) x WHERE n <= 10 ORDER BY deployed_at DESC`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var at time.Time
			var d apiv1.Deploy
			if err := rows.Scan(&id, &d.Environment, &at, &d.DeployedBy); err != nil {
				return err
			}
			d.DeployedAt = ts(at)
			byID[id].Deploys = append(byID[id].Deploys, &d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list releases", err)
	}
	return connect.NewResponse(&out), nil
}

// healthWindows are the lengths of release health periods.
var healthWindows = map[apiv1.StatsPeriod]time.Duration{
	apiv1.StatsPeriod_STATS_PERIOD_LAST_HOUR:     time.Hour,
	apiv1.StatsPeriod_STATS_PERIOD_LAST_24_HOURS: 24 * time.Hour,
	apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS:   7 * 24 * time.Hour,
	apiv1.StatsPeriod_STATS_PERIOD_LAST_14_DAYS:  14 * 24 * time.Hour,
	apiv1.StatsPeriod_STATS_PERIOD_LAST_30_DAYS:  30 * 24 * time.Hour,
	apiv1.StatsPeriod_STATS_PERIOD_LAST_90_DAYS:  90 * 24 * time.Hour,
}

// releaseHealth fills Release.health from session counters.
func releaseHealth(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, releases []*apiv1.Release, period apiv1.StatsPeriod) error {
	window, ok := healthWindows[period]
	if !ok {
		window = healthWindows[apiv1.StatsPeriod_STATS_PERIOD_LAST_14_DAYS]
	}
	versions := make([]string, len(releases))
	for i, r := range releases {
		versions[i] = r.Version
	}
	health, total, err := store.ReleaseHealth(ctx, tx, projectID, versions, time.Now().Add(-window).Truncate(time.Hour))
	if err != nil {
		return err
	}
	for _, r := range releases {
		h := health[r.Version]
		if h == nil || h.Sessions == 0 {
			continue
		}
		rh := &apiv1.ReleaseHealth{
			Sessions: uint64(h.Sessions), ErroredSessions: uint64(h.Errored), CrashedSessions: uint64(h.Crashed),
			AbnormalSessions: uint64(h.Abnormal), Users: uint64(h.Users),
			CrashFreeSessions: 100 * (1 - float64(min(h.Crashed, h.Sessions))/float64(h.Sessions)),
		}
		if h.Users > 0 {
			rh.CrashFreeUsers = 100 * (1 - float64(h.CrashedUsers)/float64(h.Users))
		}
		if total > 0 {
			rh.Adoption = 100 * float64(h.Sessions) / float64(total)
		}
		r.Health = rh
	}
	return nil
}

// parseRepository accepts "owner/name", "github.com/owner/name" or a URL.
func parseRepository(raw string) (owner, name string, ok bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	if strings.HasPrefix(s, "git@") {
		// git@github.com:owner/name
		if _, rest, found := strings.Cut(s, ":"); found {
			s = rest
		}
	} else if u, err := url.Parse(s); err == nil && u.Host != "" {
		s = strings.Trim(u.Path, "/")
	} else {
		s = strings.TrimPrefix(s, "github.com/")
	}
	parts := strings.Split(s, "/")
	if len(parts) != 2 || !validRepoPart(parts[0]) || !validRepoPart(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func validRepoPart(s string) bool {
	if s == "" || len(s) > 100 || s == "." || s == ".." {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// githubWebURL is the web address of GitHub (or GitHub Enterprise).
func (b *base) githubWebURL() string {
	if u := strings.TrimRight(b.cfg.GitHub.WebURL, "/"); u != "" {
		return u
	}
	return "https://github.com"
}

// LinkRepository implements ProjectService.LinkRepository.
func (s *ProjectServer) LinkRepository(ctx context.Context, req *connect.Request[apiv1.LinkRepositoryRequest]) (*connect.Response[apiv1.LinkRepositoryResponse], error) {
	m := req.Msg
	owner, name, ok := parseRepository(m.GetRepository())
	if !ok {
		return nil, fieldError("repository", `repository must be "owner/name" or a GitHub URL`)
	}
	branch := strings.TrimSpace(m.GetDefaultBranch())
	var integration *uuid.UUID
	if raw := m.GetIntegrationId(); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fieldError("integration_id", "invalid integration id")
		}
		// The integration must reach the repository; its default branch is
		// used unless one was given.
		var i *store.Integration
		if err := s.inOrg(ctx, func(tx pgx.Tx, _ *auth.Tenant) error {
			i, err = store.GetIntegration(ctx, tx, id)
			return err
		}); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, errNotFound("integration")
			}
			return nil, internal(ctx, s.log, "link repository", err)
		}
		client, err := s.github.Client(i)
		if err != nil {
			return nil, githubError(ctx, s.log, "link repository", err)
		}
		repo, err := client.GetRepository(ctx, owner, name)
		var gh *github.Error
		if errors.As(err, &gh) && gh.Status == http.StatusNotFound {
			return nil, fieldError("repository", "the integration cannot access "+owner+"/"+name)
		}
		if err != nil {
			return nil, githubError(ctx, s.log, "link repository", err)
		}
		if branch == "" {
			branch = repo.DefaultBranch
		}
		integration = &id
	}
	if branch == "" {
		branch = "main"
	}
	mappings := make([]map[string]string, 0, len(m.GetPathMappings()))
	for _, pm := range m.GetPathMappings() {
		mappings = append(mappings, map[string]string{"from_prefix": pm.GetFromPrefix(), "to_prefix": pm.GetToPrefix()})
	}
	mappingJSON, _ := json.Marshal(mappings)
	var out apiv1.LinkRepositoryResponse
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, m.GetProject())
		if err != nil {
			return err
		}
		repoURL := s.githubWebURL() + "/" + owner + "/" + name
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_repositories (project_id, org_id, integration_id, provider, owner, name, url, default_branch, path_mappings)
			VALUES ($1, $2, $3, 'github', $4, $5, $6, $7, $8)
			ON CONFLICT (project_id) DO UPDATE SET integration_id = EXCLUDED.integration_id, owner = EXCLUDED.owner,
			  name = EXCLUDED.name, url = EXCLUDED.url, default_branch = EXCLUDED.default_branch,
			  path_mappings = EXCLUDED.path_mappings, linked_at = now()`,
			p.ID, t.OrgID(), integration, owner, name, repoURL, branch, string(mappingJSON)); err != nil {
			return err
		}
		repos, err := loadRepositories(ctx, tx, []uuid.UUID{p.ID})
		if err != nil {
			return err
		}
		out.Repository = repos[p.ID]
		return s.audit(ctx, tx, "project.link_repository", "project", p.ID.String(), p.Slug, map[string]string{"repository": owner + "/" + name})
	})
	if err != nil {
		return nil, internal(ctx, s.log, "link repository", err)
	}
	return connect.NewResponse(&out), nil
}

// UnlinkRepository implements ProjectService.UnlinkRepository.
func (s *ProjectServer) UnlinkRepository(ctx context.Context, req *connect.Request[apiv1.UnlinkRepositoryRequest]) (*connect.Response[apiv1.UnlinkRepositoryResponse], error) {
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		p, err := s.project(ctx, tx, t, req.Msg.GetProject())
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM project_repositories WHERE project_id = $1`, p.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apiError(connect.CodeFailedPrecondition, apiv1.ErrorReason_ERROR_REASON_REPOSITORY_NOT_LINKED, "no repository is linked")
		}
		return s.audit(ctx, tx, "project.unlink_repository", "project", p.ID.String(), p.Slug, nil)
	})
	if err != nil {
		return nil, internal(ctx, s.log, "unlink repository", err)
	}
	return connect.NewResponse(&apiv1.UnlinkRepositoryResponse{}), nil
}
