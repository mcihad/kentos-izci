package api

import (
	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	c "github.com/mcihad/kentos-izci/gen/izci/api/v1/apiv1connect"
)

// rule is the authorization requirement of one procedure.
type rule struct {
	// public procedures need no credentials.
	public bool
	// optional procedures authenticate when credentials are present.
	optional bool
	// org procedures run inside the organization named by Izci-Org.
	org bool
	// role is the minimum role inside the organization.
	role apiv1.Role
	// superadmin procedures require an instance administrator.
	superadmin bool
	// scope is the personal access token scope required.
	scope apiv1.TokenScope
}

const (
	viewer = apiv1.Role_ROLE_VIEWER
	member = apiv1.Role_ROLE_MEMBER
	admin  = apiv1.Role_ROLE_ADMIN
	owner  = apiv1.Role_ROLE_OWNER

	read  = apiv1.TokenScope_TOKEN_SCOPE_READ
	write = apiv1.TokenScope_TOKEN_SCOPE_WRITE
	ai    = apiv1.TokenScope_TOKEN_SCOPE_AI
)

func orgRule(role apiv1.Role, scope apiv1.TokenScope) rule {
	return rule{org: true, role: role, scope: scope}
}

// rules maps every izci.api.v1 procedure to its requirement. A test fails
// when a procedure is missing, so new RPCs cannot ship unauthorized.
var rules = map[string]rule{
	// AuthService
	c.AuthServiceGetSetupStateProcedure:     {public: true},
	c.AuthServiceCompleteSetupProcedure:     {public: true},
	c.AuthServiceListAuthProvidersProcedure: {public: true},
	c.AuthServiceLoginProcedure:             {public: true},
	c.AuthServiceLogoutProcedure:            {optional: true},
	c.AuthServiceGetSessionProcedure:        {scope: read},
	c.AuthServiceChangePasswordProcedure:    {scope: write},
	c.AuthServiceSignUpProcedure:            {public: true},
	c.AuthServiceGetInvitationProcedure:     {public: true},
	c.AuthServiceAcceptInvitationProcedure:  {optional: true},

	// OrganizationService
	c.OrganizationServiceGetOrganizationProcedure:    orgRule(viewer, read),
	c.OrganizationServiceCreateOrganizationProcedure: {scope: write},
	c.OrganizationServiceUpdateOrganizationProcedure: orgRule(admin, write),
	c.OrganizationServiceListMembersProcedure:        orgRule(viewer, read),
	c.OrganizationServiceInviteMemberProcedure:       orgRule(admin, write),
	c.OrganizationServiceRevokeInvitationProcedure:   orgRule(admin, write),
	c.OrganizationServiceUpdateMemberProcedure:       orgRule(admin, write),
	c.OrganizationServiceRemoveMemberProcedure:       orgRule(viewer, write), // self-removal; others need admin (checked in handler)
	c.OrganizationServiceGetUsageProcedure:           orgRule(viewer, read),
	c.OrganizationServiceListAuditLogProcedure:       orgRule(admin, read),

	// ProjectService
	c.ProjectServiceListProjectsProcedure:       orgRule(viewer, read),
	c.ProjectServiceGetProjectProcedure:         orgRule(viewer, read),
	c.ProjectServiceCreateProjectProcedure:      orgRule(admin, write),
	c.ProjectServiceUpdateProjectProcedure:      orgRule(admin, write),
	c.ProjectServiceDeleteProjectProcedure:      orgRule(owner, write),
	c.ProjectServiceListApiKeysProcedure:        orgRule(viewer, read),
	c.ProjectServiceCreateApiKeyProcedure:       orgRule(admin, write),
	c.ProjectServiceUpdateApiKeyProcedure:       orgRule(admin, write),
	c.ProjectServiceRevokeApiKeyProcedure:       orgRule(admin, write),
	c.ProjectServiceGetIngestEndpointsProcedure: orgRule(viewer, read),
	c.ProjectServiceSendTestEventProcedure:      orgRule(member, write),
	c.ProjectServiceListEnvironmentsProcedure:   orgRule(viewer, read),
	c.ProjectServiceListReleasesProcedure:       orgRule(viewer, read),
	c.ProjectServiceLinkRepositoryProcedure:     orgRule(admin, write),
	c.ProjectServiceUnlinkRepositoryProcedure:   orgRule(admin, write),

	// IssueService
	c.IssueServiceListIssuesProcedure:        orgRule(viewer, read),
	c.IssueServiceGetIssueProcedure:          orgRule(viewer, read),
	c.IssueServiceUpdateIssuesProcedure:      orgRule(member, write),
	c.IssueServiceListIssueEventsProcedure:   orgRule(viewer, read),
	c.IssueServiceGetIssueStatsProcedure:     orgRule(viewer, read),
	c.IssueServiceGetIssueTagFacetsProcedure: orgRule(viewer, read),
	c.IssueServiceListIssueActivityProcedure: orgRule(viewer, read),
	c.IssueServiceAddIssueCommentProcedure:   orgRule(member, write),
	c.IssueServiceSuggestSearchProcedure:     orgRule(viewer, read),

	// EventService
	c.EventServiceGetEventProcedure:    orgRule(viewer, read),
	c.EventServiceGetRawEventProcedure: orgRule(viewer, read),

	// CustomerService
	c.CustomerServiceListCustomersProcedure:  orgRule(viewer, read),
	c.CustomerServiceGetCustomerProcedure:    orgRule(viewer, read),
	c.CustomerServiceUpdateCustomerProcedure: orgRule(member, write),

	// DashboardService
	c.DashboardServiceGetOverviewProcedure: orgRule(viewer, read),
	c.DashboardServiceWatchLiveProcedure:   orgRule(viewer, read),

	// AlertService
	c.AlertServiceListAlertRulesProcedure:         orgRule(viewer, read),
	c.AlertServiceCreateAlertRuleProcedure:        orgRule(admin, write),
	c.AlertServiceUpdateAlertRuleProcedure:        orgRule(admin, write),
	c.AlertServiceDeleteAlertRuleProcedure:        orgRule(admin, write),
	c.AlertServiceListChannelsProcedure:           orgRule(viewer, read),
	c.AlertServiceUpdateChannelProcedure:          orgRule(admin, write),
	c.AlertServiceDeleteChannelProcedure:          orgRule(admin, write),
	c.AlertServiceCreateTelegramLinkCodeProcedure: orgRule(admin, write),
	c.AlertServiceSendTestNotificationProcedure:   orgRule(admin, write),
	c.AlertServiceGetNotificationStatusProcedure:  orgRule(viewer, read),

	// IntegrationService
	c.IntegrationServiceListIntegrationsProcedure:           orgRule(viewer, read),
	c.IntegrationServiceConnectGitHubTokenProcedure:         orgRule(admin, write),
	c.IntegrationServiceStartGitHubAppInstallationProcedure: orgRule(admin, write),
	c.IntegrationServiceListRepositoriesProcedure:           orgRule(admin, read),
	c.IntegrationServiceDeleteIntegrationProcedure:          orgRule(admin, write),
	c.IntegrationServiceGetGitHubStatusProcedure:            orgRule(viewer, read),

	// AiService
	c.AiServiceStartRunProcedure:          orgRule(member, ai),
	c.AiServiceGetRunProcedure:            orgRule(viewer, read),
	c.AiServiceListRunsProcedure:          orgRule(viewer, read),
	c.AiServiceWatchRunProcedure:          orgRule(viewer, read),
	c.AiServiceCancelRunProcedure:         orgRule(member, ai),
	c.AiServiceCreateBranchProcedure:      orgRule(member, ai),
	c.AiServiceCreatePullRequestProcedure: orgRule(member, ai),
	c.AiServiceGetAiStatusProcedure:       orgRule(viewer, read),

	// UserService
	c.UserServiceUpdateProfileProcedure:                 {scope: write},
	c.UserServiceListAccessTokensProcedure:              {scope: read},
	c.UserServiceCreateAccessTokenProcedure:             {scope: write},
	c.UserServiceRevokeAccessTokenProcedure:             {scope: write},
	c.UserServiceListSessionsProcedure:                  {scope: read},
	c.UserServiceRevokeSessionProcedure:                 {scope: write},
	c.UserServiceCreateTelegramAccountLinkCodeProcedure: {scope: write},
	c.UserServiceUnlinkTelegramAccountProcedure:         {scope: write},

	// AdminService
	c.AdminServiceGetInstanceInfoProcedure:        {superadmin: true, scope: read},
	c.AdminServiceListOrganizationsProcedure:      {superadmin: true, scope: read},
	c.AdminServiceUpdateOrganizationPlanProcedure: {superadmin: true, scope: write},
	c.AdminServiceListUsersProcedure:              {superadmin: true, scope: read},
	c.AdminServiceUpdateUserProcedure:             {superadmin: true, scope: write},
}
