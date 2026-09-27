package api

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
	"github.com/mcihad/kentos-izci/internal/ai"
	"github.com/mcihad/kentos-izci/internal/alert"
	"github.com/mcihad/kentos-izci/internal/auth"
	"github.com/mcihad/kentos-izci/internal/config"
	"github.com/mcihad/kentos-izci/internal/db"
	"github.com/mcihad/kentos-izci/internal/github"
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
	// Notifier is the Telegram bot; nil when none is configured.
	Notifier Notifier
	// Alerts caches alert rules for ingest; invalidated on rule changes.
	Alerts *alert.Engine
	// AI runs analyses; nil disables AI features.
	AI *ai.Runner
	// GitHub gives access to repositories through integrations.
	GitHub *github.Access
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
	Alert        *AlertServer
	Integration  *IntegrationServer
	Ai           *AiServer
	Feedback     *FeedbackServer
	// Hub is the live update hub the services publish to.
	Hub *realtime.Hub
}

// ServiceNames lists the fully qualified names of the izci.api.v1 services.
var ServiceNames = []string{
	apiv1connect.AuthServiceName, apiv1connect.UserServiceName, apiv1connect.OrganizationServiceName,
	apiv1connect.ProjectServiceName, apiv1connect.IssueServiceName, apiv1connect.EventServiceName,
	apiv1connect.CustomerServiceName, apiv1connect.DashboardServiceName, apiv1connect.AlertServiceName,
	apiv1connect.IntegrationServiceName, apiv1connect.AiServiceName, apiv1connect.AdminServiceName,
	apiv1connect.FeedbackServiceName,
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
	b := &base{db: d.DB, auth: d.Auth, cfg: d.Config, log: d.Log, hub: d.Hub, pipeline: d.Pipeline, keys: d.Keys, done: d.StreamsDone,
		notifier: d.Notifier, alerts: d.Alerts, ai: d.AI, github: d.GitHub}
	if b.github == nil {
		b.github = github.NewAccess(d.Config.GitHub.APIURL, d.Config.GitHub.WebURL, nil, d.Auth.Box(), nil)
	}
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
		Alert:        &AlertServer{base: b},
		Integration:  &IntegrationServer{base: b},
		Ai:           &AiServer{base: b},
		Feedback:     &FeedbackServer{base: b},
		Hub:          d.Hub,
	}
	if d.Notifier != nil {
		s.User.TelegramLinkCode = d.Notifier.AccountLinkCode
	}
	handle(apiv1connect.NewAuthServiceHandler(s.Auth, opts...))
	handle(apiv1connect.NewUserServiceHandler(s.User, opts...))
	handle(apiv1connect.NewOrganizationServiceHandler(s.Organization, opts...))
	handle(apiv1connect.NewProjectServiceHandler(s.Project, opts...))
	handle(apiv1connect.NewIssueServiceHandler(s.Issue, opts...))
	handle(apiv1connect.NewEventServiceHandler(s.Event, opts...))
	handle(apiv1connect.NewCustomerServiceHandler(s.Customer, opts...))
	handle(apiv1connect.NewDashboardServiceHandler(s.Dashboard, opts...))
	handle(apiv1connect.NewAlertServiceHandler(s.Alert, opts...))
	handle(apiv1connect.NewIntegrationServiceHandler(s.Integration, opts...))
	handle(apiv1connect.NewAiServiceHandler(s.Ai, opts...))
	handle(apiv1connect.NewFeedbackServiceHandler(s.Feedback, opts...))
	handle("GET /attachments/{org}/{id}", http.HandlerFunc(s.Event.ServeAttachment))
	handle("GET /integrations/github/callback", http.HandlerFunc(s.Integration.GitHubCallback))
	handle(apiv1connect.NewAdminServiceHandler(s.Admin, opts...))
	return s, nil
}
