package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/search"
	"github.com/mcihad/kentos-izci/internal/store"
)

// ViewServer implements izci.api.v1.ViewService.
type ViewServer struct {
	*base
	apiv1connect.UnimplementedViewServiceHandler
}

var _ apiv1connect.ViewServiceHandler = (*ViewServer)(nil)

var viewJSON = protojson.MarshalOptions{UseProtoNames: true}

// editable reports whether the caller may change a view: its owner, or an
// administrator for shared ones.
func editable(t *auth.Tenant, me uuid.UUID, v *store.View) bool {
	return (v.OwnerID != nil && *v.OwnerID == me) || (v.Shared && t.Role >= apiv1.Role_ROLE_ADMIN)
}

func checkQuery(field, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	q, err := search.Parse(text)
	if err != nil {
		var se *search.Error
		if errors.As(err, &se) {
			return queryError(se.Msg, se.Pos, se.Token)
		}
		return fieldError(field, err.Error())
	}
	// Values are checked where the search becomes SQL.
	return issueFilter(&sqlb{}, q, uuid.Nil, uuid.Nil, time.Now())
}

func (s *ViewServer) owners(ctx context.Context, tx pgx.Tx, views []store.View) (map[uuid.UUID]*apiv1.UserRef, error) {
	var ids []uuid.UUID
	for _, v := range views {
		if v.OwnerID != nil {
			ids = append(ids, *v.OwnerID)
		}
	}
	return userRefs(ctx, tx, ids)
}

func toSavedSearch(v *store.View, owner *apiv1.UserRef, canEdit bool) *apiv1.SavedSearch {
	out := &apiv1.SavedSearch{}
	_ = protojson.Unmarshal(v.Body, out)
	out.Id, out.Name, out.Shared, out.Owner, out.Editable, out.UpdatedAt = v.ID.String(), v.Name, v.Shared, owner, canEdit, ts(v.UpdatedAt)
	return out
}

func toDashboard(v *store.View, owner *apiv1.UserRef, canEdit, widgets bool) *apiv1.Dashboard {
	out := &apiv1.Dashboard{Id: v.ID.String(), Name: v.Name, Shared: v.Shared, Owner: owner, Editable: canEdit, UpdatedAt: ts(v.UpdatedAt)}
	if widgets {
		var body apiv1.Dashboard
		_ = protojson.Unmarshal(v.Body, &body)
		out.Widgets = body.GetWidgets()
	}
	return out
}

// ListSavedSearches implements ViewService.ListSavedSearches.
func (s *ViewServer) ListSavedSearches(ctx context.Context, _ *connect.Request[apiv1.ListSavedSearchesRequest]) (*connect.Response[apiv1.ListSavedSearchesResponse], error) {
	me := auth.PrincipalFrom(ctx).User.ID
	out := &apiv1.ListSavedSearchesResponse{}
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		views, err := store.ListViews(ctx, tx, "search", t.OrgID(), me)
		if err != nil {
			return err
		}
		refs, err := s.owners(ctx, tx, views)
		if err != nil {
			return err
		}
		for i := range views {
			v := &views[i]
			var owner *apiv1.UserRef
			if v.OwnerID != nil {
				owner = refs[*v.OwnerID]
			}
			out.Searches = append(out.Searches, toSavedSearch(v, owner, editable(t, me, v)))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list saved searches", err)
	}
	return connect.NewResponse(out), nil
}

// save creates or changes a view after checking the caller may.
func (s *ViewServer) save(ctx context.Context, tx pgx.Tx, t *auth.Tenant, kind, rawID, name string, body []byte, shared bool) (*store.View, error) {
	me := auth.PrincipalFrom(ctx).User.ID
	// Viewers keep views for themselves; sharing is for members.
	if shared && t.Role < apiv1.Role_ROLE_MEMBER {
		return nil, errPermission("viewers cannot share")
	}
	name = strings.TrimSpace(name)
	if rawID == "" {
		return store.CreateView(ctx, tx, kind, t.OrgID(), me, name, body, shared)
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, errNotFound(kind)
	}
	v, err := store.GetView(ctx, tx, kind, t.OrgID(), me, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errNotFound(kind)
	}
	if err != nil {
		return nil, err
	}
	if !editable(t, me, v) {
		return nil, errPermission("only its owner or an administrator can change it")
	}
	return store.UpdateView(ctx, tx, kind, id, name, body, shared)
}

func (s *ViewServer) remove(ctx context.Context, kind, rawID string) error {
	me := auth.PrincipalFrom(ctx).User.ID
	id, _ := uuid.Parse(rawID)
	return s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		v, err := store.GetView(ctx, tx, kind, t.OrgID(), me, id)
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound(kind)
		}
		if err != nil {
			return err
		}
		if !editable(t, me, v) {
			return errPermission("only its owner or an administrator can delete it")
		}
		return store.DeleteView(ctx, tx, kind, id)
	})
}

// SaveSearch implements ViewService.SaveSearch.
func (s *ViewServer) SaveSearch(ctx context.Context, req *connect.Request[apiv1.SaveSearchRequest]) (*connect.Response[apiv1.SaveSearchResponse], error) {
	in := req.Msg.GetSearch()
	if err := checkQuery("search.query", in.GetQuery()); err != nil {
		return nil, err
	}
	body, err := viewJSON.Marshal(&apiv1.SavedSearch{Query: strings.TrimSpace(in.GetQuery()), Sort: in.GetSort(),
		Projects: lower(in.GetProjects()), StatsPeriod: in.GetStatsPeriod()})
	if err != nil {
		return nil, err
	}
	var out *apiv1.SavedSearch
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		v, err := s.save(ctx, tx, t, "search", in.GetId(), in.GetName(), body, in.GetShared())
		if err != nil {
			return err
		}
		refs, err := s.owners(ctx, tx, []store.View{*v})
		if err != nil {
			return err
		}
		out = toSavedSearch(v, refs[*v.OwnerID], true)
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "save search", err)
	}
	return connect.NewResponse(&apiv1.SaveSearchResponse{Search: out}), nil
}

// DeleteSavedSearch implements ViewService.DeleteSavedSearch.
func (s *ViewServer) DeleteSavedSearch(ctx context.Context, req *connect.Request[apiv1.DeleteSavedSearchRequest]) (*connect.Response[apiv1.DeleteSavedSearchResponse], error) {
	if err := s.remove(ctx, "search", req.Msg.GetId()); err != nil {
		return nil, internal(ctx, s.log, "delete saved search", err)
	}
	return connect.NewResponse(&apiv1.DeleteSavedSearchResponse{}), nil
}

// ListDashboards implements ViewService.ListDashboards.
func (s *ViewServer) ListDashboards(ctx context.Context, _ *connect.Request[apiv1.ListDashboardsRequest]) (*connect.Response[apiv1.ListDashboardsResponse], error) {
	me := auth.PrincipalFrom(ctx).User.ID
	out := &apiv1.ListDashboardsResponse{}
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		views, err := store.ListViews(ctx, tx, "dashboard", t.OrgID(), me)
		if err != nil {
			return err
		}
		refs, err := s.owners(ctx, tx, views)
		if err != nil {
			return err
		}
		for i := range views {
			v := &views[i]
			var owner *apiv1.UserRef
			if v.OwnerID != nil {
				owner = refs[*v.OwnerID]
			}
			out.Dashboards = append(out.Dashboards, toDashboard(v, owner, editable(t, me, v), false))
		}
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "list dashboards", err)
	}
	return connect.NewResponse(out), nil
}

// GetDashboard implements ViewService.GetDashboard.
func (s *ViewServer) GetDashboard(ctx context.Context, req *connect.Request[apiv1.GetDashboardRequest]) (*connect.Response[apiv1.GetDashboardResponse], error) {
	me := auth.PrincipalFrom(ctx).User.ID
	id, _ := uuid.Parse(req.Msg.GetId())
	var out *apiv1.Dashboard
	err := s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		v, err := store.GetView(ctx, tx, "dashboard", t.OrgID(), me, id)
		if errors.Is(err, store.ErrNotFound) {
			return errNotFound("dashboard")
		}
		if err != nil {
			return err
		}
		refs, err := s.owners(ctx, tx, []store.View{*v})
		if err != nil {
			return err
		}
		var owner *apiv1.UserRef
		if v.OwnerID != nil {
			owner = refs[*v.OwnerID]
		}
		out = toDashboard(v, owner, editable(t, me, v), true)
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "get dashboard", err)
	}
	return connect.NewResponse(&apiv1.GetDashboardResponse{Dashboard: out}), nil
}

// SaveDashboard implements ViewService.SaveDashboard.
func (s *ViewServer) SaveDashboard(ctx context.Context, req *connect.Request[apiv1.SaveDashboardRequest]) (*connect.Response[apiv1.SaveDashboardResponse], error) {
	in := req.Msg.GetDashboard()
	widgets := make([]*apiv1.DashboardWidget, 0, len(in.GetWidgets()))
	for _, w := range in.GetWidgets() {
		if err := checkQuery("dashboard.widgets.query", w.GetQuery()); err != nil {
			return nil, err
		}
		w = proto.CloneOf(w)
		w.Query, w.Projects = strings.TrimSpace(w.GetQuery()), lower(w.GetProjects())
		widgets = append(widgets, w)
	}
	body, err := viewJSON.Marshal(&apiv1.Dashboard{Widgets: widgets})
	if err != nil {
		return nil, err
	}
	var out *apiv1.Dashboard
	err = s.inOrg(ctx, func(tx pgx.Tx, t *auth.Tenant) error {
		v, err := s.save(ctx, tx, t, "dashboard", in.GetId(), in.GetName(), body, in.GetShared())
		if err != nil {
			return err
		}
		refs, err := s.owners(ctx, tx, []store.View{*v})
		if err != nil {
			return err
		}
		out = toDashboard(v, refs[*v.OwnerID], true, true)
		return nil
	})
	if err != nil {
		return nil, internal(ctx, s.log, "save dashboard", err)
	}
	return connect.NewResponse(&apiv1.SaveDashboardResponse{Dashboard: out}), nil
}

// DeleteDashboard implements ViewService.DeleteDashboard.
func (s *ViewServer) DeleteDashboard(ctx context.Context, req *connect.Request[apiv1.DeleteDashboardRequest]) (*connect.Response[apiv1.DeleteDashboardResponse], error) {
	if err := s.remove(ctx, "dashboard", req.Msg.GetId()); err != nil {
		return nil, internal(ctx, s.log, "delete dashboard", err)
	}
	return connect.NewResponse(&apiv1.DeleteDashboardResponse{}), nil
}
