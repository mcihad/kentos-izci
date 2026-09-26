package api

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/ingest"
	"github.com/mcihad/kentos-izci/internal/realtime"
)

// Deps are the collaborators of the API services.
type Deps struct {
	DB     *db.DB
	Auth   *auth.Service
	Config *config.Config
	Log    *slog.Logger
	// Hub receives live updates (optional in tests).
	Hub *realtime.Hub
	// Pipeline writes test events (optional in tests).
	Pipeline *ingest.Pipeline
	// Keys is the ingest key cache, invalidated when keys or project
	// settings change (optional in tests).
	Keys *ingest.KeyResolver
	// StreamsDone is closed when the server shuts down, ending live streams
	// (optional in tests).
	StreamsDone <-chan struct{}
}

// Servers exposes the service implementations so that other subsystems
// (Telegram, AI) can plug hooks into them after construction.
type Servers struct {
	Auth         *AuthServer
	User         *UserServer
	Organization *OrganizationServer
	Project      *ProjectServer
	Issue        *IssueServer
	Event        *EventServer
	Customer     *CustomerServer
	Dashboard    *DashboardServer
	Admin        *AdminServer
	// Hub is the live update hub the services publish to.
	Hub *realtime.Hub
}

// Mount registers every izci.api.v1 service on handle. Services that are
// not implemented yet answer UNIMPLEMENTED.
func Mount(handle func(string, http.Handler), d Deps) (*Servers, error) {
	ic, err := NewInterceptor(d.Auth, d.Log, d.Config.HTTP.TrustedProxies)
	if err != nil {
		return nil, err
	}
	opts := []connect.HandlerOption{
		connect.WithInterceptors(ic),
		// Browsers can only send this header after a CORS preflight, which
		// izci never grants for the API: cookie-authenticated calls are
		// therefore same-origin only (CSRF protection).
		connect.WithRequireConnectProtocolHeader(),
		connect.WithCompressMinBytes(1024),
		connect.WithReadMaxBytes(4 << 20),
	}
	if d.Hub == nil {
		d.Hub = realtime.NewHub()
	}
	b := &base{db: d.DB, auth: d.Auth, cfg: d.Config, log: d.Log, hub: d.Hub, pipeline: d.Pipeline, keys: d.Keys, done: d.StreamsDone}
	s := &Servers{
		Auth:         &AuthServer{base: b},
		User:         &UserServer{base: b},
		Organization: &OrganizationServer{base: b},
		Project:      &ProjectServer{base: b},
		Issue:        &IssueServer{base: b},
		Event:        &EventServer{base: b},
		Customer:     &CustomerServer{base: b},
		Dashboard:    &DashboardServer{base: b},
		Admin:        &AdminServer{base: b},
		Hub:          d.Hub,
	}
	handle(apiv1connect.NewAuthServiceHandler(s.Auth, opts...))
	handle(apiv1connect.NewUserServiceHandler(s.User, opts...))
	handle(apiv1connect.NewOrganizationServiceHandler(s.Organization, opts...))
	handle(apiv1connect.NewProjectServiceHandler(s.Project, opts...))
	handle(apiv1connect.NewIssueServiceHandler(s.Issue, opts...))
	handle(apiv1connect.NewEventServiceHandler(s.Event, opts...))
	handle(apiv1connect.NewCustomerServiceHandler(s.Customer, opts...))
	handle(apiv1connect.NewDashboardServiceHandler(s.Dashboard, opts...))
	handle(apiv1connect.NewAlertServiceHandler(apiv1connect.UnimplementedAlertServiceHandler{}, opts...))
	handle(apiv1connect.NewIntegrationServiceHandler(apiv1connect.UnimplementedIntegrationServiceHandler{}, opts...))
	handle(apiv1connect.NewAiServiceHandler(apiv1connect.UnimplementedAiServiceHandler{}, opts...))
	handle(apiv1connect.NewAdminServiceHandler(s.Admin, opts...))
	return s, nil
}
