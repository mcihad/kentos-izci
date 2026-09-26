package api

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
)

// Deps are the collaborators of the API services.
type Deps struct {
	DB     *db.DB
	Auth   *auth.Service
	Config *config.Config
	Log    *slog.Logger
}

// Servers exposes the service implementations so that other subsystems
// (Telegram, AI) can plug hooks into them after construction.
type Servers struct {
	Auth         *AuthServer
	User         *UserServer
	Organization *OrganizationServer
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
	b := &base{db: d.DB, auth: d.Auth, cfg: d.Config, log: d.Log}
	s := &Servers{
		Auth:         &AuthServer{base: b},
		User:         &UserServer{base: b},
		Organization: &OrganizationServer{base: b},
	}
	handle(apiv1connect.NewAuthServiceHandler(s.Auth, opts...))
	handle(apiv1connect.NewUserServiceHandler(s.User, opts...))
	handle(apiv1connect.NewOrganizationServiceHandler(s.Organization, opts...))
	handle(apiv1connect.NewProjectServiceHandler(apiv1connect.UnimplementedProjectServiceHandler{}, opts...))
	handle(apiv1connect.NewIssueServiceHandler(apiv1connect.UnimplementedIssueServiceHandler{}, opts...))
	handle(apiv1connect.NewEventServiceHandler(apiv1connect.UnimplementedEventServiceHandler{}, opts...))
	handle(apiv1connect.NewCustomerServiceHandler(apiv1connect.UnimplementedCustomerServiceHandler{}, opts...))
	handle(apiv1connect.NewDashboardServiceHandler(apiv1connect.UnimplementedDashboardServiceHandler{}, opts...))
	handle(apiv1connect.NewAlertServiceHandler(apiv1connect.UnimplementedAlertServiceHandler{}, opts...))
	handle(apiv1connect.NewIntegrationServiceHandler(apiv1connect.UnimplementedIntegrationServiceHandler{}, opts...))
	handle(apiv1connect.NewAiServiceHandler(apiv1connect.UnimplementedAiServiceHandler{}, opts...))
	handle(apiv1connect.NewAdminServiceHandler(apiv1connect.UnimplementedAdminServiceHandler{}, opts...))
	return s, nil
}
