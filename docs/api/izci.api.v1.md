# izci.api.v1

> Generated from `izci/api/v1/common.proto`, `izci/api/v1/auth.proto`, `izci/api/v1/organization.proto`, `izci/api/v1/admin.proto`, `izci/api/v1/feedback.proto`, `izci/api/v1/event.proto`, `izci/api/v1/issue.proto`, `izci/api/v1/ai.proto`, `izci/api/v1/alert.proto`, `izci/api/v1/customer.proto`, `izci/api/v1/dashboard.proto`, `izci/api/v1/integration.proto`, `izci/api/v1/monitor.proto`, `izci/api/v1/ownership.proto`, `izci/api/v1/project.proto`, `izci/api/v1/symbol.proto`, `izci/api/v1/user.proto` — do not edit by hand.

Package izci.api.v1 is the API behind the izci web UI. It is also the
public management API for automation (CLI scripts, CI, MCP tools).

**Authentication**

Calls are authenticated either by the browser session cookie set by
AuthService.Login (web UI) or by a personal access token (prefix "izp_")
sent as "Authorization: Bearer izp_…" (automation). Session-based calls must
use the Connect protocol and carry the "Connect-Protocol-Version: 1" header,
which protects them against cross-site request forgery.

**Organization scope**

Most services operate inside one organization, selected with the
"Izci-Org" request header carrying the organization slug (e.g.
"Izci-Org: kentos"). Tokens bound to one organization imply it. Calls that
need an organization and have none fail with PERMISSION_DENIED and reason
ERROR_REASON_ORGANIZATION_REQUIRED.

**Errors**

Errors use standard Connect/gRPC codes and carry an ErrorDetail with a
machine-readable ErrorReason. Clients should branch on the reason, not on
the (English) message.

**Pagination**

List methods take page_size (default 50, max 100 unless documented
otherwise) and an opaque page_token, and return next_page_token, which is
empty on the last page.

## Contents

**Services:** [AuthService](#izci-api-v1-authservice) · [OrganizationService](#izci-api-v1-organizationservice) · [AdminService](#izci-api-v1-adminservice) · [FeedbackService](#izci-api-v1-feedbackservice) · [EventService](#izci-api-v1-eventservice) · [IssueService](#izci-api-v1-issueservice) · [AiService](#izci-api-v1-aiservice) · [AlertService](#izci-api-v1-alertservice) · [CustomerService](#izci-api-v1-customerservice) · [DashboardService](#izci-api-v1-dashboardservice) · [IntegrationService](#izci-api-v1-integrationservice) · [MonitorService](#izci-api-v1-monitorservice) · [OwnershipService](#izci-api-v1-ownershipservice) · [ProjectService](#izci-api-v1-projectservice) · [SymbolService](#izci-api-v1-symbolservice) · [UserService](#izci-api-v1-userservice)

**Messages:** [AcceptInvitationRequest](#izci-api-v1-acceptinvitationrequest) · [AcceptInvitationResponse](#izci-api-v1-acceptinvitationresponse) · [AccessToken](#izci-api-v1-accesstoken) · [AddIssueCommentRequest](#izci-api-v1-addissuecommentrequest) · [AddIssueCommentResponse](#izci-api-v1-addissuecommentresponse) · [AdminOrganization](#izci-api-v1-adminorganization) · [AdminUser](#izci-api-v1-adminuser) · [AiLogEntry](#izci-api-v1-ailogentry) · [AiResult](#izci-api-v1-airesult) · [AiRun](#izci-api-v1-airun) · [AiSettings](#izci-api-v1-aisettings) · [AiUsage](#izci-api-v1-aiusage) · [AlertFilters](#izci-api-v1-alertfilters) · [AlertRule](#izci-api-v1-alertrule) · [AlertRuleInput](#izci-api-v1-alertruleinput) · [AlertTrigger](#izci-api-v1-alerttrigger) · [ApiKey](#izci-api-v1-apikey) · [ArtifactBundle](#izci-api-v1-artifactbundle) · [ArtifactFile](#izci-api-v1-artifactfile) · [AuditLogEntry](#izci-api-v1-auditlogentry) · [AuthProvider](#izci-api-v1-authprovider) · [BeginTwoFactorSetupRequest](#izci-api-v1-begintwofactorsetuprequest) · [BeginTwoFactorSetupResponse](#izci-api-v1-begintwofactorsetupresponse) · [CancelRunRequest](#izci-api-v1-cancelrunrequest) · [CancelRunResponse](#izci-api-v1-cancelrunresponse) · [ChangePasswordRequest](#izci-api-v1-changepasswordrequest) · [ChangePasswordResponse](#izci-api-v1-changepasswordresponse) · [ChangedFile](#izci-api-v1-changedfile) · [Channel](#izci-api-v1-channel) · [ChannelSettings](#izci-api-v1-channelsettings) · [CheckIn](#izci-api-v1-checkin) · [Codeowners](#izci-api-v1-codeowners) · [CompleteSetupRequest](#izci-api-v1-completesetuprequest) · [CompleteSetupResponse](#izci-api-v1-completesetupresponse) · [ConnectGitHubTokenRequest](#izci-api-v1-connectgithubtokenrequest) · [ConnectGitHubTokenResponse](#izci-api-v1-connectgithubtokenresponse) · [CreateAccessTokenRequest](#izci-api-v1-createaccesstokenrequest) · [CreateAccessTokenResponse](#izci-api-v1-createaccesstokenresponse) · [CreateAlertRuleRequest](#izci-api-v1-createalertrulerequest) · [CreateAlertRuleResponse](#izci-api-v1-createalertruleresponse) · [CreateApiKeyRequest](#izci-api-v1-createapikeyrequest) · [CreateApiKeyResponse](#izci-api-v1-createapikeyresponse) · [CreateBranchRequest](#izci-api-v1-createbranchrequest) · [CreateBranchResponse](#izci-api-v1-createbranchresponse) · [CreateChannelRequest](#izci-api-v1-createchannelrequest) · [CreateChannelResponse](#izci-api-v1-createchannelresponse) · [CreateMonitorRequest](#izci-api-v1-createmonitorrequest) · [CreateMonitorResponse](#izci-api-v1-createmonitorresponse) · [CreateOrganizationRequest](#izci-api-v1-createorganizationrequest) · [CreateOrganizationResponse](#izci-api-v1-createorganizationresponse) · [CreateProjectRequest](#izci-api-v1-createprojectrequest) · [CreateProjectResponse](#izci-api-v1-createprojectresponse) · [CreatePullRequestRequest](#izci-api-v1-createpullrequestrequest) · [CreatePullRequestResponse](#izci-api-v1-createpullrequestresponse) · [CreateTelegramAccountLinkCodeRequest](#izci-api-v1-createtelegramaccountlinkcoderequest) · [CreateTelegramAccountLinkCodeResponse](#izci-api-v1-createtelegramaccountlinkcoderesponse) · [CreateTelegramLinkCodeRequest](#izci-api-v1-createtelegramlinkcoderequest) · [CreateTelegramLinkCodeResponse](#izci-api-v1-createtelegramlinkcoderesponse) · [CreateTicketRequest](#izci-api-v1-createticketrequest) · [CreateTicketResponse](#izci-api-v1-createticketresponse) · [Customer](#izci-api-v1-customer) · [CustomerProjectBreakdown](#izci-api-v1-customerprojectbreakdown) · [CustomerRef](#izci-api-v1-customerref) · [CustomerSummary](#izci-api-v1-customersummary) · [DailyUsage](#izci-api-v1-dailyusage) · [DebugFile](#izci-api-v1-debugfile) · [DeleteAlertRuleRequest](#izci-api-v1-deletealertrulerequest) · [DeleteAlertRuleResponse](#izci-api-v1-deletealertruleresponse) · [DeleteArtifactBundleRequest](#izci-api-v1-deleteartifactbundlerequest) · [DeleteArtifactBundleResponse](#izci-api-v1-deleteartifactbundleresponse) · [DeleteChannelRequest](#izci-api-v1-deletechannelrequest) · [DeleteChannelResponse](#izci-api-v1-deletechannelresponse) · [DeleteDebugFileRequest](#izci-api-v1-deletedebugfilerequest) · [DeleteDebugFileResponse](#izci-api-v1-deletedebugfileresponse) · [DeleteIntegrationRequest](#izci-api-v1-deleteintegrationrequest) · [DeleteIntegrationResponse](#izci-api-v1-deleteintegrationresponse) · [DeleteMonitorRequest](#izci-api-v1-deletemonitorrequest) · [DeleteMonitorResponse](#izci-api-v1-deletemonitorresponse) · [DeleteProjectRequest](#izci-api-v1-deleteprojectrequest) · [DeleteProjectResponse](#izci-api-v1-deleteprojectresponse) · [Deploy](#izci-api-v1-deploy) · [DisableTwoFactorRequest](#izci-api-v1-disabletwofactorrequest) · [DisableTwoFactorResponse](#izci-api-v1-disabletwofactorresponse) · [EmailSettings](#izci-api-v1-emailsettings) · [EnableTwoFactorRequest](#izci-api-v1-enabletwofactorrequest) · [EnableTwoFactorResponse](#izci-api-v1-enabletwofactorresponse) · [Environment](#izci-api-v1-environment) · [ErrorDetail](#izci-api-v1-errordetail) · [EventAttachment](#izci-api-v1-eventattachment) · [EventDetail](#izci-api-v1-eventdetail) · [EventSummary](#izci-api-v1-eventsummary) · [FeatureFlags](#izci-api-v1-featureflags) · [FilterDay](#izci-api-v1-filterday) · [FilterStat](#izci-api-v1-filterstat) · [FindSuspectCommitRequest](#izci-api-v1-findsuspectcommitrequest) · [FindSuspectCommitResponse](#izci-api-v1-findsuspectcommitresponse) · [GetAiStatusRequest](#izci-api-v1-getaistatusrequest) · [GetAiStatusResponse](#izci-api-v1-getaistatusresponse) · [GetCustomerRequest](#izci-api-v1-getcustomerrequest) · [GetCustomerResponse](#izci-api-v1-getcustomerresponse) · [GetEventRequest](#izci-api-v1-geteventrequest) · [GetEventResponse](#izci-api-v1-geteventresponse) · [GetFilterStatsRequest](#izci-api-v1-getfilterstatsrequest) · [GetFilterStatsResponse](#izci-api-v1-getfilterstatsresponse) · [GetGitHubStatusRequest](#izci-api-v1-getgithubstatusrequest) · [GetGitHubStatusResponse](#izci-api-v1-getgithubstatusresponse) · [GetIngestEndpointsRequest](#izci-api-v1-getingestendpointsrequest) · [GetIngestEndpointsResponse](#izci-api-v1-getingestendpointsresponse) · [GetInstanceInfoRequest](#izci-api-v1-getinstanceinforequest) · [GetInstanceInfoResponse](#izci-api-v1-getinstanceinforesponse) · [GetInvitationRequest](#izci-api-v1-getinvitationrequest) · [GetInvitationResponse](#izci-api-v1-getinvitationresponse) · [GetIssueOwnersRequest](#izci-api-v1-getissueownersrequest) · [GetIssueOwnersResponse](#izci-api-v1-getissueownersresponse) · [GetIssueRequest](#izci-api-v1-getissuerequest) · [GetIssueResponse](#izci-api-v1-getissueresponse) · [GetIssueStatsRequest](#izci-api-v1-getissuestatsrequest) · [GetIssueStatsResponse](#izci-api-v1-getissuestatsresponse) · [GetIssueTagFacetsRequest](#izci-api-v1-getissuetagfacetsrequest) · [GetIssueTagFacetsResponse](#izci-api-v1-getissuetagfacetsresponse) · [GetMonitorRequest](#izci-api-v1-getmonitorrequest) · [GetMonitorResponse](#izci-api-v1-getmonitorresponse) · [GetNotificationStatusRequest](#izci-api-v1-getnotificationstatusrequest) · [GetNotificationStatusResponse](#izci-api-v1-getnotificationstatusresponse) · [GetOrganizationRequest](#izci-api-v1-getorganizationrequest) · [GetOrganizationResponse](#izci-api-v1-getorganizationresponse) · [GetOverviewRequest](#izci-api-v1-getoverviewrequest) · [GetOverviewResponse](#izci-api-v1-getoverviewresponse) · [GetOwnershipRequest](#izci-api-v1-getownershiprequest) · [GetOwnershipResponse](#izci-api-v1-getownershipresponse) · [GetProjectRequest](#izci-api-v1-getprojectrequest) · [GetProjectResponse](#izci-api-v1-getprojectresponse) · [GetRawEventRequest](#izci-api-v1-getraweventrequest) · [GetRawEventResponse](#izci-api-v1-getraweventresponse) · [GetRunRequest](#izci-api-v1-getrunrequest) · [GetRunResponse](#izci-api-v1-getrunresponse) · [GetSessionRequest](#izci-api-v1-getsessionrequest) · [GetSessionResponse](#izci-api-v1-getsessionresponse) · [GetSetupStateRequest](#izci-api-v1-getsetupstaterequest) · [GetSetupStateResponse](#izci-api-v1-getsetupstateresponse) · [GetUsageRequest](#izci-api-v1-getusagerequest) · [GetUsageResponse](#izci-api-v1-getusageresponse) · [Grouping](#izci-api-v1-grouping) · [IgnoreOptions](#izci-api-v1-ignoreoptions) · [ImportCodeownersRequest](#izci-api-v1-importcodeownersrequest) · [ImportCodeownersResponse](#izci-api-v1-importcodeownersresponse) · [InAppRules](#izci-api-v1-inapprules) · [InboundFilters](#izci-api-v1-inboundfilters) · [Integration](#izci-api-v1-integration) · [Invitation](#izci-api-v1-invitation) · [InviteMemberRequest](#izci-api-v1-invitememberrequest) · [InviteMemberResponse](#izci-api-v1-invitememberresponse) · [Issue](#izci-api-v1-issue) · [IssueActivity](#izci-api-v1-issueactivity) · [IssueLocation](#izci-api-v1-issuelocation) · [IssueOwner](#izci-api-v1-issueowner) · [IssueRef](#izci-api-v1-issueref) · [IssueSummary](#izci-api-v1-issuesummary) · [JiraInput](#izci-api-v1-jirainput) · [JiraSettings](#izci-api-v1-jirasettings) · [LevelSeries](#izci-api-v1-levelseries) · [LinkRepositoryRequest](#izci-api-v1-linkrepositoryrequest) · [LinkRepositoryResponse](#izci-api-v1-linkrepositoryresponse) · [ListAccessTokensRequest](#izci-api-v1-listaccesstokensrequest) · [ListAccessTokensResponse](#izci-api-v1-listaccesstokensresponse) · [ListAlertRulesRequest](#izci-api-v1-listalertrulesrequest) · [ListAlertRulesResponse](#izci-api-v1-listalertrulesresponse) · [ListApiKeysRequest](#izci-api-v1-listapikeysrequest) · [ListApiKeysResponse](#izci-api-v1-listapikeysresponse) · [ListArtifactBundlesRequest](#izci-api-v1-listartifactbundlesrequest) · [ListArtifactBundlesResponse](#izci-api-v1-listartifactbundlesresponse) · [ListAuditLogRequest](#izci-api-v1-listauditlogrequest) · [ListAuditLogResponse](#izci-api-v1-listauditlogresponse) · [ListAuthProvidersRequest](#izci-api-v1-listauthprovidersrequest) · [ListAuthProvidersResponse](#izci-api-v1-listauthprovidersresponse) · [ListChannelsRequest](#izci-api-v1-listchannelsrequest) · [ListChannelsResponse](#izci-api-v1-listchannelsresponse) · [ListCheckInsRequest](#izci-api-v1-listcheckinsrequest) · [ListCheckInsResponse](#izci-api-v1-listcheckinsresponse) · [ListCustomersRequest](#izci-api-v1-listcustomersrequest) · [ListCustomersResponse](#izci-api-v1-listcustomersresponse) · [ListDebugFilesRequest](#izci-api-v1-listdebugfilesrequest) · [ListDebugFilesResponse](#izci-api-v1-listdebugfilesresponse) · [ListEnvironmentsRequest](#izci-api-v1-listenvironmentsrequest) · [ListEnvironmentsResponse](#izci-api-v1-listenvironmentsresponse) · [ListFeedbackRequest](#izci-api-v1-listfeedbackrequest) · [ListFeedbackResponse](#izci-api-v1-listfeedbackresponse) · [ListIntegrationsRequest](#izci-api-v1-listintegrationsrequest) · [ListIntegrationsResponse](#izci-api-v1-listintegrationsresponse) · [ListIssueActivityRequest](#izci-api-v1-listissueactivityrequest) · [ListIssueActivityResponse](#izci-api-v1-listissueactivityresponse) · [ListIssueEventsRequest](#izci-api-v1-listissueeventsrequest) · [ListIssueEventsResponse](#izci-api-v1-listissueeventsresponse) · [ListIssuesRequest](#izci-api-v1-listissuesrequest) · [ListIssuesResponse](#izci-api-v1-listissuesresponse) · [ListMembersRequest](#izci-api-v1-listmembersrequest) · [ListMembersResponse](#izci-api-v1-listmembersresponse) · [ListMonitorsRequest](#izci-api-v1-listmonitorsrequest) · [ListMonitorsResponse](#izci-api-v1-listmonitorsresponse) · [ListOrganizationsRequest](#izci-api-v1-listorganizationsrequest) · [ListOrganizationsResponse](#izci-api-v1-listorganizationsresponse) · [ListOwnerAliasesRequest](#izci-api-v1-listowneraliasesrequest) · [ListOwnerAliasesResponse](#izci-api-v1-listowneraliasesresponse) · [ListProjectsRequest](#izci-api-v1-listprojectsrequest) · [ListProjectsResponse](#izci-api-v1-listprojectsresponse) · [ListReleasesRequest](#izci-api-v1-listreleasesrequest) · [ListReleasesResponse](#izci-api-v1-listreleasesresponse) · [ListRepositoriesRequest](#izci-api-v1-listrepositoriesrequest) · [ListRepositoriesResponse](#izci-api-v1-listrepositoriesresponse) · [ListRunsRequest](#izci-api-v1-listrunsrequest) · [ListRunsResponse](#izci-api-v1-listrunsresponse) · [ListSessionsRequest](#izci-api-v1-listsessionsrequest) · [ListSessionsResponse](#izci-api-v1-listsessionsresponse) · [ListUsersRequest](#izci-api-v1-listusersrequest) · [ListUsersResponse](#izci-api-v1-listusersresponse) · [LoginRequest](#izci-api-v1-loginrequest) · [LoginResponse](#izci-api-v1-loginresponse) · [LogoutRequest](#izci-api-v1-logoutrequest) · [LogoutResponse](#izci-api-v1-logoutresponse) · [Member](#izci-api-v1-member) · [Membership](#izci-api-v1-membership) · [Monitor](#izci-api-v1-monitor) · [MonitorEnvironment](#izci-api-v1-monitorenvironment) · [MonitorInterval](#izci-api-v1-monitorinterval) · [MonitorSchedule](#izci-api-v1-monitorschedule) · [Organization](#izci-api-v1-organization) · [OrganizationLimits](#izci-api-v1-organizationlimits) · [OrganizationRef](#izci-api-v1-organizationref) · [OrganizationSettings](#izci-api-v1-organizationsettings) · [OverviewTotals](#izci-api-v1-overviewtotals) · [OwnerAlias](#izci-api-v1-owneralias) · [Ownership](#izci-api-v1-ownership) · [PathMapping](#izci-api-v1-pathmapping) · [PathRewrite](#izci-api-v1-pathrewrite) · [PrivacySettings](#izci-api-v1-privacysettings) · [Project](#izci-api-v1-project) · [ProjectHealth](#izci-api-v1-projecthealth) · [ProjectRef](#izci-api-v1-projectref) · [ProjectRepository](#izci-api-v1-projectrepository) · [ProjectSettings](#izci-api-v1-projectsettings) · [ProjectStats](#izci-api-v1-projectstats) · [PullRequestRef](#izci-api-v1-pullrequestref) · [RegenerateRecoveryCodesRequest](#izci-api-v1-regeneraterecoverycodesrequest) · [RegenerateRecoveryCodesResponse](#izci-api-v1-regeneraterecoverycodesresponse) · [Release](#izci-api-v1-release) · [ReleaseHealth](#izci-api-v1-releasehealth) · [RemoveMemberRequest](#izci-api-v1-removememberrequest) · [RemoveMemberResponse](#izci-api-v1-removememberresponse) · [Repository](#izci-api-v1-repository) · [ResolveOptions](#izci-api-v1-resolveoptions) · [RevokeAccessTokenRequest](#izci-api-v1-revokeaccesstokenrequest) · [RevokeAccessTokenResponse](#izci-api-v1-revokeaccesstokenresponse) · [RevokeApiKeyRequest](#izci-api-v1-revokeapikeyrequest) · [RevokeApiKeyResponse](#izci-api-v1-revokeapikeyresponse) · [RevokeInvitationRequest](#izci-api-v1-revokeinvitationrequest) · [RevokeInvitationResponse](#izci-api-v1-revokeinvitationresponse) · [RevokeSessionRequest](#izci-api-v1-revokesessionrequest) · [RevokeSessionResponse](#izci-api-v1-revokesessionresponse) · [RuleProblem](#izci-api-v1-ruleproblem) · [SearchSuggestion](#izci-api-v1-searchsuggestion) · [SendTestEventRequest](#izci-api-v1-sendtesteventrequest) · [SendTestEventResponse](#izci-api-v1-sendtesteventresponse) · [SendTestNotificationRequest](#izci-api-v1-sendtestnotificationrequest) · [SendTestNotificationResponse](#izci-api-v1-sendtestnotificationresponse) · [Series](#izci-api-v1-series) · [Session](#izci-api-v1-session) · [SessionInfo](#izci-api-v1-sessioninfo) · [SetOwnerAliasRequest](#izci-api-v1-setowneraliasrequest) · [SetOwnerAliasResponse](#izci-api-v1-setowneraliasresponse) · [SignUpRequest](#izci-api-v1-signuprequest) · [SignUpResponse](#izci-api-v1-signupresponse) · [Snooze](#izci-api-v1-snooze) · [StartGitHubAppInstallationRequest](#izci-api-v1-startgithubappinstallationrequest) · [StartGitHubAppInstallationResponse](#izci-api-v1-startgithubappinstallationresponse) · [StartRunRequest](#izci-api-v1-startrunrequest) · [StartRunResponse](#izci-api-v1-startrunresponse) · [SuggestSearchRequest](#izci-api-v1-suggestsearchrequest) · [SuggestSearchResponse](#izci-api-v1-suggestsearchresponse) · [SuspectCommit](#izci-api-v1-suspectcommit) · [TagFacet](#izci-api-v1-tagfacet) · [TagValueCount](#izci-api-v1-tagvaluecount) · [TelegramAccount](#izci-api-v1-telegramaccount) · [TelegramChat](#izci-api-v1-telegramchat) · [TicketRef](#izci-api-v1-ticketref) · [UnlinkRepositoryRequest](#izci-api-v1-unlinkrepositoryrequest) · [UnlinkRepositoryResponse](#izci-api-v1-unlinkrepositoryresponse) · [UnlinkTelegramAccountRequest](#izci-api-v1-unlinktelegramaccountrequest) · [UnlinkTelegramAccountResponse](#izci-api-v1-unlinktelegramaccountresponse) · [UpdateAlertRuleRequest](#izci-api-v1-updatealertrulerequest) · [UpdateAlertRuleResponse](#izci-api-v1-updatealertruleresponse) · [UpdateApiKeyRequest](#izci-api-v1-updateapikeyrequest) · [UpdateApiKeyResponse](#izci-api-v1-updateapikeyresponse) · [UpdateChannelRequest](#izci-api-v1-updatechannelrequest) · [UpdateChannelResponse](#izci-api-v1-updatechannelresponse) · [UpdateCustomerRequest](#izci-api-v1-updatecustomerrequest) · [UpdateCustomerResponse](#izci-api-v1-updatecustomerresponse) · [UpdateIssuesRequest](#izci-api-v1-updateissuesrequest) · [UpdateIssuesResponse](#izci-api-v1-updateissuesresponse) · [UpdateMemberRequest](#izci-api-v1-updatememberrequest) · [UpdateMemberResponse](#izci-api-v1-updatememberresponse) · [UpdateMonitorRequest](#izci-api-v1-updatemonitorrequest) · [UpdateMonitorResponse](#izci-api-v1-updatemonitorresponse) · [UpdateOrganizationPlanRequest](#izci-api-v1-updateorganizationplanrequest) · [UpdateOrganizationPlanResponse](#izci-api-v1-updateorganizationplanresponse) · [UpdateOrganizationRequest](#izci-api-v1-updateorganizationrequest) · [UpdateOrganizationResponse](#izci-api-v1-updateorganizationresponse) · [UpdateOwnershipRequest](#izci-api-v1-updateownershiprequest) · [UpdateOwnershipResponse](#izci-api-v1-updateownershipresponse) · [UpdateProfileRequest](#izci-api-v1-updateprofilerequest) · [UpdateProfileResponse](#izci-api-v1-updateprofileresponse) · [UpdateProjectRequest](#izci-api-v1-updateprojectrequest) · [UpdateProjectResponse](#izci-api-v1-updateprojectresponse) · [UpdateUserRequest](#izci-api-v1-updateuserrequest) · [UpdateUserResponse](#izci-api-v1-updateuserresponse) · [User](#izci-api-v1-user) · [UserAgentInfo](#izci-api-v1-useragentinfo) · [UserFeedback](#izci-api-v1-userfeedback) · [UserRef](#izci-api-v1-userref) · [VerifyTwoFactorRequest](#izci-api-v1-verifytwofactorrequest) · [VerifyTwoFactorResponse](#izci-api-v1-verifytwofactorresponse) · [WatchLiveRequest](#izci-api-v1-watchliverequest) · [WatchLiveResponse](#izci-api-v1-watchliveresponse) · [WatchRunRequest](#izci-api-v1-watchrunrequest) · [WatchRunResponse](#izci-api-v1-watchrunresponse) · [WebhookSettings](#izci-api-v1-webhooksettings)

**Enums:** [ActivityKind](#izci-api-v1-activitykind) · [AiLogKind](#izci-api-v1-ailogkind) · [AiRunMode](#izci-api-v1-airunmode) · [AiRunStatus](#izci-api-v1-airunstatus) · [AiRunTrigger](#izci-api-v1-airuntrigger) · [AiState](#izci-api-v1-aistate) · [ApiKeyScope](#izci-api-v1-apikeyscope) · [ArtifactFileKind](#izci-api-v1-artifactfilekind) · [AutoAction](#izci-api-v1-autoaction) · [AutoAnalyze](#izci-api-v1-autoanalyze) · [AutoAssign](#izci-api-v1-autoassign) · [ChannelKind](#izci-api-v1-channelkind) · [CheckInStatus](#izci-api-v1-checkinstatus) · [CustomerSort](#izci-api-v1-customersort) · [DebugFileKind](#izci-api-v1-debugfilekind) · [ErrorReason](#izci-api-v1-errorreason) · [EventPick](#izci-api-v1-eventpick) · [FilterReason](#izci-api-v1-filterreason) · [Health](#izci-api-v1-health) · [IntegrationAuth](#izci-api-v1-integrationauth) · [IpStorage](#izci-api-v1-ipstorage) · [IssueSort](#izci-api-v1-issuesort) · [IssueStatus](#izci-api-v1-issuestatus) · [IssueSubstatus](#izci-api-v1-issuesubstatus) · [LiveEventKind](#izci-api-v1-liveeventkind) · [MonitorIntervalUnit](#izci-api-v1-monitorintervalunit) · [MonitorStatus](#izci-api-v1-monitorstatus) · [Priority](#izci-api-v1-priority) · [Risk](#izci-api-v1-risk) · [Role](#izci-api-v1-role) · [RuleProblemKind](#izci-api-v1-ruleproblemkind) · [SignupMode](#izci-api-v1-signupmode) · [StatsPeriod](#izci-api-v1-statsperiod) · [SuggestionKind](#izci-api-v1-suggestionkind) · [SuspectCommitState](#izci-api-v1-suspectcommitstate) · [TokenScope](#izci-api-v1-tokenscope) · [TriggerKind](#izci-api-v1-triggerkind)

## Services

<a id="izci-api-v1-authservice"></a>

### AuthService

AuthService handles first-run setup, sign-in and sessions.

Methods of this service do not require the Izci-Org header. Login,
VerifyTwoFactor, CompleteSetup, SignUp and AcceptInvitation set the
"izci_session" cookie (HttpOnly, Secure, SameSite=Lax); Logout clears it.
OpenID Connect sign-in is a browser redirect flow outside of this service:
open AuthProvider.login_url. For accounts with two-factor authentication
it ends on "/login?two_factor=1", where VerifyTwoFactor completes it.

| Method | Request | Response | Summary |
|---|---|---|---|
| [GetSetupState](#izci-api-v1-authservice-getsetupstate) | [GetSetupStateRequest](#izci-api-v1-getsetupstaterequest) | [GetSetupStateResponse](#izci-api-v1-getsetupstateresponse) | GetSetupState tells whether the instance still needs its initial setup (no user exists yet). |
| [CompleteSetup](#izci-api-v1-authservice-completesetup) | [CompleteSetupRequest](#izci-api-v1-completesetuprequest) | [CompleteSetupResponse](#izci-api-v1-completesetupresponse) | CompleteSetup creates the first user (an instance superadmin and owner), the first organization and the first project, then signs the user in. |
| [ListAuthProviders](#izci-api-v1-authservice-listauthproviders) | [ListAuthProvidersRequest](#izci-api-v1-listauthprovidersrequest) | [ListAuthProvidersResponse](#izci-api-v1-listauthprovidersresponse) | ListAuthProviders lists the sign-in options shown on the login page. |
| [Login](#izci-api-v1-authservice-login) | [LoginRequest](#izci-api-v1-loginrequest) | [LoginResponse](#izci-api-v1-loginresponse) | Login signs in with email and password. |
| [VerifyTwoFactor](#izci-api-v1-authservice-verifytwofactor) | [VerifyTwoFactorRequest](#izci-api-v1-verifytwofactorrequest) | [VerifyTwoFactorResponse](#izci-api-v1-verifytwofactorresponse) | VerifyTwoFactor completes a sign-in waiting for its second step (the "izci_2fa" cookie) with a code from the authenticator app or a recovery code, and starts the session. |
| [Logout](#izci-api-v1-authservice-logout) | [LogoutRequest](#izci-api-v1-logoutrequest) | [LogoutResponse](#izci-api-v1-logoutresponse) | Logout ends the current session. |
| [GetSession](#izci-api-v1-authservice-getsession) | [GetSessionRequest](#izci-api-v1-getsessionrequest) | [GetSessionResponse](#izci-api-v1-getsessionresponse) | GetSession returns the signed-in user and their organizations. |
| [ChangePassword](#izci-api-v1-authservice-changepassword) | [ChangePasswordRequest](#izci-api-v1-changepasswordrequest) | [ChangePasswordResponse](#izci-api-v1-changepasswordresponse) | ChangePassword changes (or, for accounts created through OpenID Connect, sets) the password of the signed-in user and ends their other sessions. |
| [SignUp](#izci-api-v1-authservice-signup) | [SignUpRequest](#izci-api-v1-signuprequest) | [SignUpResponse](#izci-api-v1-signupresponse) | SignUp creates an account together with a new organization when self sign-up is open (AUTH_SIGNUP=open), then signs the user in. |
| [GetInvitation](#izci-api-v1-authservice-getinvitation) | [GetInvitationRequest](#izci-api-v1-getinvitationrequest) | [GetInvitationResponse](#izci-api-v1-getinvitationresponse) | GetInvitation describes a pending invitation by its token. |
| [AcceptInvitation](#izci-api-v1-authservice-acceptinvitation) | [AcceptInvitationRequest](#izci-api-v1-acceptinvitationrequest) | [AcceptInvitationResponse](#izci-api-v1-acceptinvitationresponse) | AcceptInvitation joins the invited organization. |

<a id="izci-api-v1-authservice-getsetupstate"></a>

#### AuthService.GetSetupState

`rpc GetSetupState(GetSetupStateRequest) returns (GetSetupStateResponse)`

GetSetupState tells whether the instance still needs its initial setup
(no user exists yet). Callable without authentication.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-authservice-completesetup"></a>

#### AuthService.CompleteSetup

`rpc CompleteSetup(CompleteSetupRequest) returns (CompleteSetupResponse)`

CompleteSetup creates the first user (an instance superadmin and owner),
the first organization and the first project, then signs the user in.
Fails with ERROR_REASON_SETUP_COMPLETED once any user exists and with
ERROR_REASON_SETUP_TOKEN_INVALID without the setup code, which izci
prints in its log while no user exists (or IZCI_SETUP_TOKEN).

<a id="izci-api-v1-authservice-listauthproviders"></a>

#### AuthService.ListAuthProviders

`rpc ListAuthProviders(ListAuthProvidersRequest) returns (ListAuthProvidersResponse)`

ListAuthProviders lists the sign-in options shown on the login page.
Callable without authentication.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-authservice-login"></a>

#### AuthService.Login

`rpc Login(LoginRequest) returns (LoginResponse)`

Login signs in with email and password. Repeated failures are rate
limited per account and per client address
(ERROR_REASON_TOO_MANY_ATTEMPTS). For accounts with two-factor
authentication the response has two_factor_required instead of a
session, and sets the "izci_2fa" cookie (HttpOnly, 10 minutes) that
VerifyTwoFactor needs.

<a id="izci-api-v1-authservice-verifytwofactor"></a>

#### AuthService.VerifyTwoFactor

`rpc VerifyTwoFactor(VerifyTwoFactorRequest) returns (VerifyTwoFactorResponse)`

VerifyTwoFactor completes a sign-in waiting for its second step (the
"izci_2fa" cookie) with a code from the authenticator app or a recovery
code, and starts the session. Fails with
ERROR_REASON_INVALID_TWO_FACTOR_CODE for a wrong code,
ERROR_REASON_TWO_FACTOR_EXPIRED when the sign-in is older than 10
minutes, and ERROR_REASON_TOO_MANY_ATTEMPTS after repeated wrong codes
(the lockout grows from 5 minutes to a day).

<a id="izci-api-v1-authservice-logout"></a>

#### AuthService.Logout

`rpc Logout(LogoutRequest) returns (LogoutResponse)`

Logout ends the current session.

<a id="izci-api-v1-authservice-getsession"></a>

#### AuthService.GetSession

`rpc GetSession(GetSessionRequest) returns (GetSessionResponse)`

GetSession returns the signed-in user and their organizations. Fails
with UNAUTHENTICATED when there is no valid session.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-authservice-changepassword"></a>

#### AuthService.ChangePassword

`rpc ChangePassword(ChangePasswordRequest) returns (ChangePasswordResponse)`

ChangePassword changes (or, for accounts created through OpenID Connect,
sets) the password of the signed-in user and ends their other sessions.

<a id="izci-api-v1-authservice-signup"></a>

#### AuthService.SignUp

`rpc SignUp(SignUpRequest) returns (SignUpResponse)`

SignUp creates an account together with a new organization when self
sign-up is open (AUTH_SIGNUP=open), then signs the user in.

<a id="izci-api-v1-authservice-getinvitation"></a>

#### AuthService.GetInvitation

`rpc GetInvitation(GetInvitationRequest) returns (GetInvitationResponse)`

GetInvitation describes a pending invitation by its token. Callable
without authentication so the invitation page can render.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-authservice-acceptinvitation"></a>

#### AuthService.AcceptInvitation

`rpc AcceptInvitation(AcceptInvitationRequest) returns (AcceptInvitationResponse)`

AcceptInvitation joins the invited organization. A signed-in user joins
with their account; otherwise an account is created from name and
password for the invited email address.

<a id="izci-api-v1-organizationservice"></a>

### OrganizationService

OrganizationService manages the organization selected by the Izci-Org
header: settings, members, invitations, usage and the audit log.

An organization is the tenant boundary of izci: projects, customers,
issues, alert rules and integrations all belong to exactly one
organization, and no data is shared between organizations.

| Method | Request | Response | Summary |
|---|---|---|---|
| [GetOrganization](#izci-api-v1-organizationservice-getorganization) | [GetOrganizationRequest](#izci-api-v1-getorganizationrequest) | [GetOrganizationResponse](#izci-api-v1-getorganizationresponse) | GetOrganization returns the current organization. |
| [CreateOrganization](#izci-api-v1-organizationservice-createorganization) | [CreateOrganizationRequest](#izci-api-v1-createorganizationrequest) | [CreateOrganizationResponse](#izci-api-v1-createorganizationresponse) | CreateOrganization creates a new organization owned by the caller. |
| [UpdateOrganization](#izci-api-v1-organizationservice-updateorganization) | [UpdateOrganizationRequest](#izci-api-v1-updateorganizationrequest) | [UpdateOrganizationResponse](#izci-api-v1-updateorganizationresponse) | UpdateOrganization changes the name and settings. |
| [ListMembers](#izci-api-v1-organizationservice-listmembers) | [ListMembersRequest](#izci-api-v1-listmembersrequest) | [ListMembersResponse](#izci-api-v1-listmembersresponse) | ListMembers lists members and pending invitations. |
| [InviteMember](#izci-api-v1-organizationservice-invitemember) | [InviteMemberRequest](#izci-api-v1-invitememberrequest) | [InviteMemberResponse](#izci-api-v1-invitememberresponse) | InviteMember invites an email address. |
| [RevokeInvitation](#izci-api-v1-organizationservice-revokeinvitation) | [RevokeInvitationRequest](#izci-api-v1-revokeinvitationrequest) | [RevokeInvitationResponse](#izci-api-v1-revokeinvitationresponse) | RevokeInvitation cancels a pending invitation. |
| [UpdateMember](#izci-api-v1-organizationservice-updatemember) | [UpdateMemberRequest](#izci-api-v1-updatememberrequest) | [UpdateMemberResponse](#izci-api-v1-updatememberresponse) | UpdateMember changes a member's role. |
| [RemoveMember](#izci-api-v1-organizationservice-removemember) | [RemoveMemberRequest](#izci-api-v1-removememberrequest) | [RemoveMemberResponse](#izci-api-v1-removememberresponse) | RemoveMember removes a member (or the caller, to leave). |
| [GetUsage](#izci-api-v1-organizationservice-getusage) | [GetUsageRequest](#izci-api-v1-getusagerequest) | [GetUsageResponse](#izci-api-v1-getusageresponse) | GetUsage returns event and AI usage of the current month against the organization's limits. |
| [ListAuditLog](#izci-api-v1-organizationservice-listauditlog) | [ListAuditLogRequest](#izci-api-v1-listauditlogrequest) | [ListAuditLogResponse](#izci-api-v1-listauditlogresponse) | ListAuditLog lists security-relevant changes (keys, members, integrations, settings), newest first. |

<a id="izci-api-v1-organizationservice-getorganization"></a>

#### OrganizationService.GetOrganization

`rpc GetOrganization(GetOrganizationRequest) returns (GetOrganizationResponse)`

GetOrganization returns the current organization. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-organizationservice-createorganization"></a>

#### OrganizationService.CreateOrganization

`rpc CreateOrganization(CreateOrganizationRequest) returns (CreateOrganizationResponse)`

CreateOrganization creates a new organization owned by the caller.
Allowed for superadmins, and for everyone when self sign-up is open.
Does not use the Izci-Org header.

<a id="izci-api-v1-organizationservice-updateorganization"></a>

#### OrganizationService.UpdateOrganization

`rpc UpdateOrganization(UpdateOrganizationRequest) returns (UpdateOrganizationResponse)`

UpdateOrganization changes the name and settings. Requires ROLE_ADMIN.

<a id="izci-api-v1-organizationservice-listmembers"></a>

#### OrganizationService.ListMembers

`rpc ListMembers(ListMembersRequest) returns (ListMembersResponse)`

ListMembers lists members and pending invitations. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-organizationservice-invitemember"></a>

#### OrganizationService.InviteMember

`rpc InviteMember(InviteMemberRequest) returns (InviteMemberResponse)`

InviteMember invites an email address. Requires ROLE_ADMIN; inviting an
owner requires ROLE_OWNER. Returns the invitation link to share (izci
does not send email).

<a id="izci-api-v1-organizationservice-revokeinvitation"></a>

#### OrganizationService.RevokeInvitation

`rpc RevokeInvitation(RevokeInvitationRequest) returns (RevokeInvitationResponse)`

RevokeInvitation cancels a pending invitation. Requires ROLE_ADMIN.

<a id="izci-api-v1-organizationservice-updatemember"></a>

#### OrganizationService.UpdateMember

`rpc UpdateMember(UpdateMemberRequest) returns (UpdateMemberResponse)`

UpdateMember changes a member's role. Requires ROLE_ADMIN; changing an
owner or granting ROLE_OWNER requires ROLE_OWNER. The last owner cannot be
demoted (ERROR_REASON_LAST_OWNER).

<a id="izci-api-v1-organizationservice-removemember"></a>

#### OrganizationService.RemoveMember

`rpc RemoveMember(RemoveMemberRequest) returns (RemoveMemberResponse)`

RemoveMember removes a member (or the caller, to leave). Requires
ROLE_ADMIN to remove others. The last owner cannot be removed.

<a id="izci-api-v1-organizationservice-getusage"></a>

#### OrganizationService.GetUsage

`rpc GetUsage(GetUsageRequest) returns (GetUsageResponse)`

GetUsage returns event and AI usage of the current month against the
organization's limits. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-organizationservice-listauditlog"></a>

#### OrganizationService.ListAuditLog

`rpc ListAuditLog(ListAuditLogRequest) returns (ListAuditLogResponse)`

ListAuditLog lists security-relevant changes (keys, members,
integrations, settings), newest first. Requires ROLE_ADMIN.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-adminservice"></a>

### AdminService

AdminService administers the whole instance. Every method requires a
superadmin; none uses the Izci-Org header.

| Method | Request | Response | Summary |
|---|---|---|---|
| [GetInstanceInfo](#izci-api-v1-adminservice-getinstanceinfo) | [GetInstanceInfoRequest](#izci-api-v1-getinstanceinforequest) | [GetInstanceInfoResponse](#izci-api-v1-getinstanceinforesponse) | GetInstanceInfo returns version, database and feature information. |
| [ListOrganizations](#izci-api-v1-adminservice-listorganizations) | [ListOrganizationsRequest](#izci-api-v1-listorganizationsrequest) | [ListOrganizationsResponse](#izci-api-v1-listorganizationsresponse) | ListOrganizations lists all organizations with their usage. |
| [UpdateOrganizationPlan](#izci-api-v1-adminservice-updateorganizationplan) | [UpdateOrganizationPlanRequest](#izci-api-v1-updateorganizationplanrequest) | [UpdateOrganizationPlanResponse](#izci-api-v1-updateorganizationplanresponse) | UpdateOrganizationPlan changes the plan and limits of an organization. |
| [ListUsers](#izci-api-v1-adminservice-listusers) | [ListUsersRequest](#izci-api-v1-listusersrequest) | [ListUsersResponse](#izci-api-v1-listusersresponse) | ListUsers lists all users. |
| [UpdateUser](#izci-api-v1-adminservice-updateuser) | [UpdateUserRequest](#izci-api-v1-updateuserrequest) | [UpdateUserResponse](#izci-api-v1-updateuserresponse) | UpdateUser grants or revokes superadmin and disables or enables an account. |

<a id="izci-api-v1-adminservice-getinstanceinfo"></a>

#### AdminService.GetInstanceInfo

`rpc GetInstanceInfo(GetInstanceInfoRequest) returns (GetInstanceInfoResponse)`

GetInstanceInfo returns version, database and feature information.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-adminservice-listorganizations"></a>

#### AdminService.ListOrganizations

`rpc ListOrganizations(ListOrganizationsRequest) returns (ListOrganizationsResponse)`

ListOrganizations lists all organizations with their usage.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-adminservice-updateorganizationplan"></a>

#### AdminService.UpdateOrganizationPlan

`rpc UpdateOrganizationPlan(UpdateOrganizationPlanRequest) returns (UpdateOrganizationPlanResponse)`

UpdateOrganizationPlan changes the plan and limits of an organization.

<a id="izci-api-v1-adminservice-listusers"></a>

#### AdminService.ListUsers

`rpc ListUsers(ListUsersRequest) returns (ListUsersResponse)`

ListUsers lists all users.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-adminservice-updateuser"></a>

#### AdminService.UpdateUser

`rpc UpdateUser(UpdateUserRequest) returns (UpdateUserResponse)`

UpdateUser grants or revokes superadmin and disables or enables an
account. A superadmin cannot demote or disable themselves.

<a id="izci-api-v1-feedbackservice"></a>

### FeedbackService

FeedbackService reads what end users wrote about problems (user feedback).
Feedback arrives from Sentry SDKs: the feedback widget, captureFeedback
calls and the legacy user report API.

| Method | Request | Response | Summary |
|---|---|---|---|
| [ListFeedback](#izci-api-v1-feedbackservice-listfeedback) | [ListFeedbackRequest](#izci-api-v1-listfeedbackrequest) | [ListFeedbackResponse](#izci-api-v1-listfeedbackresponse) | ListFeedback returns feedback of the current organization, newest first, optionally limited to a project or an issue. |

<a id="izci-api-v1-feedbackservice-listfeedback"></a>

#### FeedbackService.ListFeedback

`rpc ListFeedback(ListFeedbackRequest) returns (ListFeedbackResponse)`

ListFeedback returns feedback of the current organization, newest first,
optionally limited to a project or an issue. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-eventservice"></a>

### EventService

EventService reads individual events of the current organization.

| Method | Request | Response | Summary |
|---|---|---|---|
| [GetEvent](#izci-api-v1-eventservice-getevent) | [GetEventRequest](#izci-api-v1-geteventrequest) | [GetEventResponse](#izci-api-v1-geteventresponse) | GetEvent returns one event with everything needed to render it, plus the ids of its neighbours within the same issue. |
| [GetRawEvent](#izci-api-v1-eventservice-getrawevent) | [GetRawEventRequest](#izci-api-v1-getraweventrequest) | [GetRawEventResponse](#izci-api-v1-getraweventresponse) | GetRawEvent returns the stored event as JSON, for download and debugging. |

<a id="izci-api-v1-eventservice-getevent"></a>

#### EventService.GetEvent

`rpc GetEvent(GetEventRequest) returns (GetEventResponse)`

GetEvent returns one event with everything needed to render it, plus the
ids of its neighbours within the same issue. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-eventservice-getrawevent"></a>

#### EventService.GetRawEvent

`rpc GetRawEvent(GetRawEventRequest) returns (GetRawEventResponse)`

GetRawEvent returns the stored event as JSON, for download and
debugging. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-issueservice"></a>

### IssueService

IssueService lists, inspects and triages issues of the current
organization.

An issue is a group of events that share a fingerprint (see
docs/gruplama.md). Search queries use the izci search language, e.g.

  is:unresolved level:error customer:sivas-belediyesi release:1.4.2
  browser:Chrome !environment:staging "timeout"

Supported keys: is (unresolved, resolved, ignored, new, regressed,
escalating, assigned, unassigned, unhandled), level, project, environment
(env), release, customer, tenant, user, browser, os, device, server,
assigned (me, none, an email), type, file, has (ai, user, customer),
first_seen / last_seen (-24h, -7d, >2026-09-01), times_seen (>100),
priority, and any custom tag key. Prefix a term with "!" to negate it;
quoted or bare words search titles, messages and culprits.

| Method | Request | Response | Summary |
|---|---|---|---|
| [ListIssues](#izci-api-v1-issueservice-listissues) | [ListIssuesRequest](#izci-api-v1-listissuesrequest) | [ListIssuesResponse](#izci-api-v1-listissuesresponse) | ListIssues searches issues. |
| [GetIssue](#izci-api-v1-issueservice-getissue) | [GetIssueRequest](#izci-api-v1-getissuerequest) | [GetIssueResponse](#izci-api-v1-getissueresponse) | GetIssue returns one issue by id or short id. |
| [UpdateIssues](#izci-api-v1-issueservice-updateissues) | [UpdateIssuesRequest](#izci-api-v1-updateissuesrequest) | [UpdateIssuesResponse](#izci-api-v1-updateissuesresponse) | UpdateIssues changes status, assignee or priority of up to 100 issues at once. |
| [ListIssueEvents](#izci-api-v1-issueservice-listissueevents) | [ListIssueEventsRequest](#izci-api-v1-listissueeventsrequest) | [ListIssueEventsResponse](#izci-api-v1-listissueeventsresponse) | ListIssueEvents pages through the events of an issue, newest first. |
| [GetIssueStats](#izci-api-v1-issueservice-getissuestats) | [GetIssueStatsRequest](#izci-api-v1-getissuestatsrequest) | [GetIssueStatsResponse](#izci-api-v1-getissuestatsresponse) | GetIssueStats returns the event count series of an issue. |
| [GetIssueTagFacets](#izci-api-v1-issueservice-getissuetagfacets) | [GetIssueTagFacetsRequest](#izci-api-v1-getissuetagfacetsrequest) | [GetIssueTagFacetsResponse](#izci-api-v1-getissuetagfacetsresponse) | GetIssueTagFacets returns the distribution of tag values (customer, tenant, release, browser, …) across the events of an issue. |
| [ListIssueActivity](#izci-api-v1-issueservice-listissueactivity) | [ListIssueActivityRequest](#izci-api-v1-listissueactivityrequest) | [ListIssueActivityResponse](#izci-api-v1-listissueactivityresponse) | ListIssueActivity lists comments and changes of an issue, newest first. |
| [AddIssueComment](#izci-api-v1-issueservice-addissuecomment) | [AddIssueCommentRequest](#izci-api-v1-addissuecommentrequest) | [AddIssueCommentResponse](#izci-api-v1-addissuecommentresponse) | AddIssueComment adds a Markdown comment to an issue. |
| [CreateTicket](#izci-api-v1-issueservice-createticket) | [CreateTicketRequest](#izci-api-v1-createticketrequest) | [CreateTicketResponse](#izci-api-v1-createticketresponse) | CreateTicket opens a ticket for an issue in the Jira project of a channel; an issue gets one ticket per channel, so a second call returns the existing one. |
| [SuggestSearch](#izci-api-v1-issueservice-suggestsearch) | [SuggestSearchRequest](#izci-api-v1-suggestsearchrequest) | [SuggestSearchResponse](#izci-api-v1-suggestsearchresponse) | SuggestSearch completes the search term under the cursor: keys, values seen in the data and operators. |

<a id="izci-api-v1-issueservice-listissues"></a>

#### IssueService.ListIssues

`rpc ListIssues(ListIssuesRequest) returns (ListIssuesResponse)`

ListIssues searches issues. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-issueservice-getissue"></a>

#### IssueService.GetIssue

`rpc GetIssue(GetIssueRequest) returns (GetIssueResponse)`

GetIssue returns one issue by id or short id. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-issueservice-updateissues"></a>

#### IssueService.UpdateIssues

`rpc UpdateIssues(UpdateIssuesRequest) returns (UpdateIssuesResponse)`

UpdateIssues changes status, assignee or priority of up to 100 issues at
once. Requires ROLE_MEMBER.

<a id="izci-api-v1-issueservice-listissueevents"></a>

#### IssueService.ListIssueEvents

`rpc ListIssueEvents(ListIssueEventsRequest) returns (ListIssueEventsResponse)`

ListIssueEvents pages through the events of an issue, newest first.
Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-issueservice-getissuestats"></a>

#### IssueService.GetIssueStats

`rpc GetIssueStats(GetIssueStatsRequest) returns (GetIssueStatsResponse)`

GetIssueStats returns the event count series of an issue. Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-issueservice-getissuetagfacets"></a>

#### IssueService.GetIssueTagFacets

`rpc GetIssueTagFacets(GetIssueTagFacetsRequest) returns (GetIssueTagFacetsResponse)`

GetIssueTagFacets returns the distribution of tag values (customer,
tenant, release, browser, …) across the events of an issue. Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-issueservice-listissueactivity"></a>

#### IssueService.ListIssueActivity

`rpc ListIssueActivity(ListIssueActivityRequest) returns (ListIssueActivityResponse)`

ListIssueActivity lists comments and changes of an issue, newest first.
Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-issueservice-addissuecomment"></a>

#### IssueService.AddIssueComment

`rpc AddIssueComment(AddIssueCommentRequest) returns (AddIssueCommentResponse)`

AddIssueComment adds a Markdown comment to an issue. Requires
ROLE_MEMBER.

<a id="izci-api-v1-issueservice-createticket"></a>

#### IssueService.CreateTicket

`rpc CreateTicket(CreateTicketRequest) returns (CreateTicketResponse)`

CreateTicket opens a ticket for an issue in the Jira project of a
channel; an issue gets one ticket per channel, so a second call returns
the existing one. Requires ROLE_MEMBER.

<a id="izci-api-v1-issueservice-suggestsearch"></a>

#### IssueService.SuggestSearch

`rpc SuggestSearch(SuggestSearchRequest) returns (SuggestSearchResponse)`

SuggestSearch completes the search term under the cursor: keys, values
seen in the data and operators. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-aiservice"></a>

### AiService

AiService runs AI analyses of issues with opencode and turns proposed
fixes into branches and pull requests.

A run checks out the project's repository at the release of the analyzed
event, gives the agent the issue context (stack traces mapped to repository
paths, breadcrumbs, request, runtime) and restricted permissions, and
records the analysis and the resulting diff. Nothing is pushed until
CreateBranch or CreatePullRequest is called (or the project's automation
does it).

| Method | Request | Response | Summary |
|---|---|---|---|
| [StartRun](#izci-api-v1-aiservice-startrun) | [StartRunRequest](#izci-api-v1-startrunrequest) | [StartRunResponse](#izci-api-v1-startrunresponse) | StartRun queues an analysis of an issue. |
| [GetRun](#izci-api-v1-aiservice-getrun) | [GetRunRequest](#izci-api-v1-getrunrequest) | [GetRunResponse](#izci-api-v1-getrunresponse) | GetRun returns a run with its analysis and diff. |
| [ListRuns](#izci-api-v1-aiservice-listruns) | [ListRunsRequest](#izci-api-v1-listrunsrequest) | [ListRunsResponse](#izci-api-v1-listrunsresponse) | ListRuns lists runs, newest first. |
| [WatchRun](#izci-api-v1-aiservice-watchrun) | [WatchRunRequest](#izci-api-v1-watchrunrequest) | stream [WatchRunResponse](#izci-api-v1-watchrunresponse) | WatchRun streams the log and status changes of a run until it finishes. |
| [CancelRun](#izci-api-v1-aiservice-cancelrun) | [CancelRunRequest](#izci-api-v1-cancelrunrequest) | [CancelRunResponse](#izci-api-v1-cancelrunresponse) | CancelRun stops a queued or running run. |
| [CreateBranch](#izci-api-v1-aiservice-createbranch) | [CreateBranchRequest](#izci-api-v1-createbranchrequest) | [CreateBranchResponse](#izci-api-v1-createbranchresponse) | CreateBranch commits the proposed changes and pushes them to a new branch. |
| [CreatePullRequest](#izci-api-v1-aiservice-createpullrequest) | [CreatePullRequestRequest](#izci-api-v1-createpullrequestrequest) | [CreatePullRequestResponse](#izci-api-v1-createpullrequestresponse) | CreatePullRequest pushes the branch (if needed) and opens a pull request. |
| [GetAiStatus](#izci-api-v1-aiservice-getaistatus) | [GetAiStatusRequest](#izci-api-v1-getaistatusrequest) | [GetAiStatusResponse](#izci-api-v1-getaistatusresponse) | GetAiStatus reports whether AI analysis is available and the current queue. |

<a id="izci-api-v1-aiservice-startrun"></a>

#### AiService.StartRun

`rpc StartRun(StartRunRequest) returns (StartRunResponse)`

StartRun queues an analysis of an issue. Requires ROLE_MEMBER. Fails with
ERROR_REASON_AI_RUN_ACTIVE when a run for the issue is already queued or
running, and ERROR_REASON_AI_BUDGET_EXCEEDED at the monthly limit.

<a id="izci-api-v1-aiservice-getrun"></a>

#### AiService.GetRun

`rpc GetRun(GetRunRequest) returns (GetRunResponse)`

GetRun returns a run with its analysis and diff. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-aiservice-listruns"></a>

#### AiService.ListRuns

`rpc ListRuns(ListRunsRequest) returns (ListRunsResponse)`

ListRuns lists runs, newest first. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-aiservice-watchrun"></a>

#### AiService.WatchRun

`rpc WatchRun(WatchRunRequest) returns (stream WatchRunResponse)`

WatchRun streams the log and status changes of a run until it finishes.
Log entries already recorded are replayed first. Requires ROLE_VIEWER.

<a id="izci-api-v1-aiservice-cancelrun"></a>

#### AiService.CancelRun

`rpc CancelRun(CancelRunRequest) returns (CancelRunResponse)`

CancelRun stops a queued or running run. Requires ROLE_MEMBER.

<a id="izci-api-v1-aiservice-createbranch"></a>

#### AiService.CreateBranch

`rpc CreateBranch(CreateBranchRequest) returns (CreateBranchResponse)`

CreateBranch commits the proposed changes and pushes them to a new
branch. Requires ROLE_MEMBER. Fails with ERROR_REASON_AI_RUN_NOT_READY
when the run has no changes.

<a id="izci-api-v1-aiservice-createpullrequest"></a>

#### AiService.CreatePullRequest

`rpc CreatePullRequest(CreatePullRequestRequest) returns (CreatePullRequestResponse)`

CreatePullRequest pushes the branch (if needed) and opens a pull
request. Requires ROLE_MEMBER.

<a id="izci-api-v1-aiservice-getaistatus"></a>

#### AiService.GetAiStatus

`rpc GetAiStatus(GetAiStatusRequest) returns (GetAiStatusResponse)`

GetAiStatus reports whether AI analysis is available and the current
queue. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-alertservice"></a>

### AlertService

AlertService manages alert rules and the notification channels they
deliver to: Telegram chats, e-mail recipients, Slack and Microsoft Teams
channels, webhooks and Jira projects.

Linking a Telegram chat: call CreateTelegramLinkCode, add the izci bot to
the group (or open a private chat with it) and send "/baglan <code>". The
chat then appears in ListChannels. Other channels are created with
CreateChannel.

| Method | Request | Response | Summary |
|---|---|---|---|
| [ListAlertRules](#izci-api-v1-alertservice-listalertrules) | [ListAlertRulesRequest](#izci-api-v1-listalertrulesrequest) | [ListAlertRulesResponse](#izci-api-v1-listalertrulesresponse) | ListAlertRules lists rules, optionally for one project. |
| [CreateAlertRule](#izci-api-v1-alertservice-createalertrule) | [CreateAlertRuleRequest](#izci-api-v1-createalertrulerequest) | [CreateAlertRuleResponse](#izci-api-v1-createalertruleresponse) | CreateAlertRule creates a rule. |
| [UpdateAlertRule](#izci-api-v1-alertservice-updatealertrule) | [UpdateAlertRuleRequest](#izci-api-v1-updatealertrulerequest) | [UpdateAlertRuleResponse](#izci-api-v1-updatealertruleresponse) | UpdateAlertRule replaces a rule. |
| [DeleteAlertRule](#izci-api-v1-alertservice-deletealertrule) | [DeleteAlertRuleRequest](#izci-api-v1-deletealertrulerequest) | [DeleteAlertRuleResponse](#izci-api-v1-deletealertruleresponse) | DeleteAlertRule deletes a rule. |
| [ListChannels](#izci-api-v1-alertservice-listchannels) | [ListChannelsRequest](#izci-api-v1-listchannelsrequest) | [ListChannelsResponse](#izci-api-v1-listchannelsresponse) | ListChannels lists notification channels. |
| [CreateChannel](#izci-api-v1-alertservice-createchannel) | [CreateChannelRequest](#izci-api-v1-createchannelrequest) | [CreateChannelResponse](#izci-api-v1-createchannelresponse) | CreateChannel adds an e-mail, Slack, Teams, webhook or Jira channel. |
| [UpdateChannel](#izci-api-v1-alertservice-updatechannel) | [UpdateChannelRequest](#izci-api-v1-updatechannelrequest) | [UpdateChannelResponse](#izci-api-v1-updatechannelresponse) | UpdateChannel renames, mutes or reconfigures a channel. |
| [DeleteChannel](#izci-api-v1-alertservice-deletechannel) | [DeleteChannelRequest](#izci-api-v1-deletechannelrequest) | [DeleteChannelResponse](#izci-api-v1-deletechannelresponse) | DeleteChannel removes a channel and detaches it from rules. |
| [CreateTelegramLinkCode](#izci-api-v1-alertservice-createtelegramlinkcode) | [CreateTelegramLinkCodeRequest](#izci-api-v1-createtelegramlinkcoderequest) | [CreateTelegramLinkCodeResponse](#izci-api-v1-createtelegramlinkcoderesponse) | CreateTelegramLinkCode creates a one-time code (valid 15 minutes) that links a Telegram chat to the organization. |
| [SendTestNotification](#izci-api-v1-alertservice-sendtestnotification) | [SendTestNotificationRequest](#izci-api-v1-sendtestnotificationrequest) | [SendTestNotificationResponse](#izci-api-v1-sendtestnotificationresponse) | SendTestNotification sends a sample notification to a channel. |
| [GetNotificationStatus](#izci-api-v1-alertservice-getnotificationstatus) | [GetNotificationStatusRequest](#izci-api-v1-getnotificationstatusrequest) | [GetNotificationStatusResponse](#izci-api-v1-getnotificationstatusresponse) | GetNotificationStatus reports which notification transports are configured on the instance. |

<a id="izci-api-v1-alertservice-listalertrules"></a>

#### AlertService.ListAlertRules

`rpc ListAlertRules(ListAlertRulesRequest) returns (ListAlertRulesResponse)`

ListAlertRules lists rules, optionally for one project. Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-alertservice-createalertrule"></a>

#### AlertService.CreateAlertRule

`rpc CreateAlertRule(CreateAlertRuleRequest) returns (CreateAlertRuleResponse)`

CreateAlertRule creates a rule. Requires ROLE_ADMIN.

<a id="izci-api-v1-alertservice-updatealertrule"></a>

#### AlertService.UpdateAlertRule

`rpc UpdateAlertRule(UpdateAlertRuleRequest) returns (UpdateAlertRuleResponse)`

UpdateAlertRule replaces a rule. Requires ROLE_ADMIN.

<a id="izci-api-v1-alertservice-deletealertrule"></a>

#### AlertService.DeleteAlertRule

`rpc DeleteAlertRule(DeleteAlertRuleRequest) returns (DeleteAlertRuleResponse)`

DeleteAlertRule deletes a rule. Requires ROLE_ADMIN.

<a id="izci-api-v1-alertservice-listchannels"></a>

#### AlertService.ListChannels

`rpc ListChannels(ListChannelsRequest) returns (ListChannelsResponse)`

ListChannels lists notification channels. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-alertservice-createchannel"></a>

#### AlertService.CreateChannel

`rpc CreateChannel(CreateChannelRequest) returns (CreateChannelResponse)`

CreateChannel adds an e-mail, Slack, Teams, webhook or Jira channel.
Jira settings are checked against Jira first. Requires ROLE_ADMIN. Fails
with ERROR_REASON_CHANNEL_UNAVAILABLE for e-mail when no SMTP server is
configured.

<a id="izci-api-v1-alertservice-updatechannel"></a>

#### AlertService.UpdateChannel

`rpc UpdateChannel(UpdateChannelRequest) returns (UpdateChannelResponse)`

UpdateChannel renames, mutes or reconfigures a channel. Requires
ROLE_ADMIN.

<a id="izci-api-v1-alertservice-deletechannel"></a>

#### AlertService.DeleteChannel

`rpc DeleteChannel(DeleteChannelRequest) returns (DeleteChannelResponse)`

DeleteChannel removes a channel and detaches it from rules. Requires
ROLE_ADMIN.

<a id="izci-api-v1-alertservice-createtelegramlinkcode"></a>

#### AlertService.CreateTelegramLinkCode

`rpc CreateTelegramLinkCode(CreateTelegramLinkCodeRequest) returns (CreateTelegramLinkCodeResponse)`

CreateTelegramLinkCode creates a one-time code (valid 15 minutes) that
links a Telegram chat to the organization. Requires ROLE_ADMIN. Fails
with ERROR_REASON_TELEGRAM_NOT_CONFIGURED when no bot is configured.

<a id="izci-api-v1-alertservice-sendtestnotification"></a>

#### AlertService.SendTestNotification

`rpc SendTestNotification(SendTestNotificationRequest) returns (SendTestNotificationResponse)`

SendTestNotification sends a sample notification to a channel. Requires
ROLE_ADMIN.

<a id="izci-api-v1-alertservice-getnotificationstatus"></a>

#### AlertService.GetNotificationStatus

`rpc GetNotificationStatus(GetNotificationStatusRequest) returns (GetNotificationStatusResponse)`

GetNotificationStatus reports which notification transports are
configured on the instance. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-customerservice"></a>

### CustomerService

CustomerService reports on customers (the organizations that use the
monitored applications, e.g. municipalities).

Customers are created automatically the first time an event reports
Event.customer.id and are shared by all projects of the organization.

| Method | Request | Response | Summary |
|---|---|---|---|
| [ListCustomers](#izci-api-v1-customerservice-listcustomers) | [ListCustomersRequest](#izci-api-v1-listcustomersrequest) | [ListCustomersResponse](#izci-api-v1-listcustomersresponse) | ListCustomers lists customers with their health over a period. |
| [GetCustomer](#izci-api-v1-customerservice-getcustomer) | [GetCustomerRequest](#izci-api-v1-getcustomerrequest) | [GetCustomerResponse](#izci-api-v1-getcustomerresponse) | GetCustomer returns one customer with a per-project breakdown and its most impactful issues. |
| [UpdateCustomer](#izci-api-v1-customerservice-updatecustomer) | [UpdateCustomerRequest](#izci-api-v1-updatecustomerrequest) | [UpdateCustomerResponse](#izci-api-v1-updatecustomerresponse) | UpdateCustomer changes the display name or notes. |

<a id="izci-api-v1-customerservice-listcustomers"></a>

#### CustomerService.ListCustomers

`rpc ListCustomers(ListCustomersRequest) returns (ListCustomersResponse)`

ListCustomers lists customers with their health over a period.
Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-customerservice-getcustomer"></a>

#### CustomerService.GetCustomer

`rpc GetCustomer(GetCustomerRequest) returns (GetCustomerResponse)`

GetCustomer returns one customer with a per-project breakdown and its
most impactful issues. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-customerservice-updatecustomer"></a>

#### CustomerService.UpdateCustomer

`rpc UpdateCustomer(UpdateCustomerRequest) returns (UpdateCustomerResponse)`

UpdateCustomer changes the display name or notes. Requires ROLE_MEMBER.

<a id="izci-api-v1-dashboardservice"></a>

### DashboardService

DashboardService powers the overview page and the live feed.

| Method | Request | Response | Summary |
|---|---|---|---|
| [GetOverview](#izci-api-v1-dashboardservice-getoverview) | [GetOverviewRequest](#izci-api-v1-getoverviewrequest) | [GetOverviewResponse](#izci-api-v1-getoverviewresponse) | GetOverview returns totals, charts and top lists for a period. |
| [WatchLive](#izci-api-v1-dashboardservice-watchlive) | [WatchLiveRequest](#izci-api-v1-watchliverequest) | stream [WatchLiveResponse](#izci-api-v1-watchliveresponse) | WatchLive streams events and issue changes of the organization as they happen. |

<a id="izci-api-v1-dashboardservice-getoverview"></a>

#### DashboardService.GetOverview

`rpc GetOverview(GetOverviewRequest) returns (GetOverviewResponse)`

GetOverview returns totals, charts and top lists for a period. Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-dashboardservice-watchlive"></a>

#### DashboardService.WatchLive

`rpc WatchLive(WatchLiveRequest) returns (stream WatchLiveResponse)`

WatchLive streams events and issue changes of the organization as they
happen. The server sends a heartbeat every 20 seconds; clients should
reconnect with backoff when the stream ends. Requires ROLE_VIEWER.

<a id="izci-api-v1-integrationservice"></a>

### IntegrationService

IntegrationService connects the organization to source-code hosts
(GitHub), which izci uses to link stack frames to source, to let the AI
agent read the code and to push fix branches and open pull requests.

Two connection types exist: a fine-grained personal access token (stored
encrypted) and the izci GitHub App (short-lived installation tokens;
requires GITHUB_APP_* configuration on the instance).

| Method | Request | Response | Summary |
|---|---|---|---|
| [ListIntegrations](#izci-api-v1-integrationservice-listintegrations) | [ListIntegrationsRequest](#izci-api-v1-listintegrationsrequest) | [ListIntegrationsResponse](#izci-api-v1-listintegrationsresponse) | ListIntegrations lists connected accounts. |
| [ConnectGitHubToken](#izci-api-v1-integrationservice-connectgithubtoken) | [ConnectGitHubTokenRequest](#izci-api-v1-connectgithubtokenrequest) | [ConnectGitHubTokenResponse](#izci-api-v1-connectgithubtokenresponse) | ConnectGitHubToken validates and stores a GitHub token. |
| [StartGitHubAppInstallation](#izci-api-v1-integrationservice-startgithubappinstallation) | [StartGitHubAppInstallationRequest](#izci-api-v1-startgithubappinstallationrequest) | [StartGitHubAppInstallationResponse](#izci-api-v1-startgithubappinstallationresponse) | StartGitHubAppInstallation returns the URL that installs the izci GitHub App; GitHub redirects back to izci, which stores the installation. |
| [ListRepositories](#izci-api-v1-integrationservice-listrepositories) | [ListRepositoriesRequest](#izci-api-v1-listrepositoriesrequest) | [ListRepositoriesResponse](#izci-api-v1-listrepositoriesresponse) | ListRepositories lists repositories reachable through an integration. |
| [DeleteIntegration](#izci-api-v1-integrationservice-deleteintegration) | [DeleteIntegrationRequest](#izci-api-v1-deleteintegrationrequest) | [DeleteIntegrationResponse](#izci-api-v1-deleteintegrationresponse) | DeleteIntegration disconnects an account; linked projects lose AI fix features. |
| [GetGitHubStatus](#izci-api-v1-integrationservice-getgithubstatus) | [GetGitHubStatusRequest](#izci-api-v1-getgithubstatusrequest) | [GetGitHubStatusResponse](#izci-api-v1-getgithubstatusresponse) | GetGitHubStatus reports whether the GitHub App is configured on the instance. |

<a id="izci-api-v1-integrationservice-listintegrations"></a>

#### IntegrationService.ListIntegrations

`rpc ListIntegrations(ListIntegrationsRequest) returns (ListIntegrationsResponse)`

ListIntegrations lists connected accounts. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-integrationservice-connectgithubtoken"></a>

#### IntegrationService.ConnectGitHubToken

`rpc ConnectGitHubToken(ConnectGitHubTokenRequest) returns (ConnectGitHubTokenResponse)`

ConnectGitHubToken validates and stores a GitHub token. The token needs
"Contents: read and write", "Pull requests: read and write" and
"Metadata: read" on the repositories izci should use. Requires
ROLE_ADMIN.

<a id="izci-api-v1-integrationservice-startgithubappinstallation"></a>

#### IntegrationService.StartGitHubAppInstallation

`rpc StartGitHubAppInstallation(StartGitHubAppInstallationRequest) returns (StartGitHubAppInstallationResponse)`

StartGitHubAppInstallation returns the URL that installs the izci GitHub
App; GitHub redirects back to izci, which stores the installation.
Requires ROLE_ADMIN.

<a id="izci-api-v1-integrationservice-listrepositories"></a>

#### IntegrationService.ListRepositories

`rpc ListRepositories(ListRepositoriesRequest) returns (ListRepositoriesResponse)`

ListRepositories lists repositories reachable through an integration.
Requires ROLE_ADMIN.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-integrationservice-deleteintegration"></a>

#### IntegrationService.DeleteIntegration

`rpc DeleteIntegration(DeleteIntegrationRequest) returns (DeleteIntegrationResponse)`

DeleteIntegration disconnects an account; linked projects lose AI fix
features. Requires ROLE_ADMIN.

<a id="izci-api-v1-integrationservice-getgithubstatus"></a>

#### IntegrationService.GetGitHubStatus

`rpc GetGitHubStatus(GetGitHubStatusRequest) returns (GetGitHubStatusResponse)`

GetGitHubStatus reports whether the GitHub App is configured on the
instance. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-monitorservice"></a>

### MonitorService

MonitorService manages cron monitors: scheduled jobs (reports, billing
runs, imports) that report when they run. Monitors are usually created by
the jobs themselves with their first check-in (Sentry SDK monitor_config
or izci.ingest.v1.IngestService.SendCheckIn); they can also be created and
edited here. A monitor that misses runs, fails or times out raises an
issue, which alert rules notify about like any other.

| Method | Request | Response | Summary |
|---|---|---|---|
| [ListMonitors](#izci-api-v1-monitorservice-listmonitors) | [ListMonitorsRequest](#izci-api-v1-listmonitorsrequest) | [ListMonitorsResponse](#izci-api-v1-listmonitorsresponse) | ListMonitors returns the monitors of the current organization with the state of each environment and their latest runs. |
| [GetMonitor](#izci-api-v1-monitorservice-getmonitor) | [GetMonitorRequest](#izci-api-v1-getmonitorrequest) | [GetMonitorResponse](#izci-api-v1-getmonitorresponse) | GetMonitor returns one monitor. |
| [CreateMonitor](#izci-api-v1-monitorservice-createmonitor) | [CreateMonitorRequest](#izci-api-v1-createmonitorrequest) | [CreateMonitorResponse](#izci-api-v1-createmonitorresponse) | CreateMonitor creates a monitor in a project. |
| [UpdateMonitor](#izci-api-v1-monitorservice-updatemonitor) | [UpdateMonitorRequest](#izci-api-v1-updatemonitorrequest) | [UpdateMonitorResponse](#izci-api-v1-updatemonitorresponse) | UpdateMonitor changes the name, schedule, failure policy or paused state of a monitor. |
| [DeleteMonitor](#izci-api-v1-monitorservice-deletemonitor) | [DeleteMonitorRequest](#izci-api-v1-deletemonitorrequest) | [DeleteMonitorResponse](#izci-api-v1-deletemonitorresponse) | DeleteMonitor deletes a monitor with its runs. |
| [ListCheckIns](#izci-api-v1-monitorservice-listcheckins) | [ListCheckInsRequest](#izci-api-v1-listcheckinsrequest) | [ListCheckInsResponse](#izci-api-v1-listcheckinsresponse) | ListCheckIns returns the runs of a monitor, newest first. |

<a id="izci-api-v1-monitorservice-listmonitors"></a>

#### MonitorService.ListMonitors

`rpc ListMonitors(ListMonitorsRequest) returns (ListMonitorsResponse)`

ListMonitors returns the monitors of the current organization with the
state of each environment and their latest runs. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-monitorservice-getmonitor"></a>

#### MonitorService.GetMonitor

`rpc GetMonitor(GetMonitorRequest) returns (GetMonitorResponse)`

GetMonitor returns one monitor. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-monitorservice-createmonitor"></a>

#### MonitorService.CreateMonitor

`rpc CreateMonitor(CreateMonitorRequest) returns (CreateMonitorResponse)`

CreateMonitor creates a monitor in a project. Requires ROLE_ADMIN.

<a id="izci-api-v1-monitorservice-updatemonitor"></a>

#### MonitorService.UpdateMonitor

`rpc UpdateMonitor(UpdateMonitorRequest) returns (UpdateMonitorResponse)`

UpdateMonitor changes the name, schedule, failure policy or paused
state of a monitor. Requires ROLE_ADMIN.

<a id="izci-api-v1-monitorservice-deletemonitor"></a>

#### MonitorService.DeleteMonitor

`rpc DeleteMonitor(DeleteMonitorRequest) returns (DeleteMonitorResponse)`

DeleteMonitor deletes a monitor with its runs. Its issues stay. Requires
ROLE_ADMIN.

<a id="izci-api-v1-monitorservice-listcheckins"></a>

#### MonitorService.ListCheckIns

`rpc ListCheckIns(ListCheckInsRequest) returns (ListCheckInsResponse)`

ListCheckIns returns the runs of a monitor, newest first. Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-ownershipservice"></a>

### OwnershipService

OwnershipService manages who owns a project's issues: ownership rules in
Sentry's syntax, an imported CODEOWNERS file, aliases that map "@login",
"@org/team" or "#team" to members, and automatic assignment of new
issues. It also names an issue's owners and its suspect commit, the
commit that last changed the failing lines.

Rules are evaluated in order, CODEOWNERS first and the project's own
rules after it; the last matching rule decides automatic assignment.

| Method | Request | Response | Summary |
|---|---|---|---|
| [GetOwnership](#izci-api-v1-ownershipservice-getownership) | [GetOwnershipRequest](#izci-api-v1-getownershiprequest) | [GetOwnershipResponse](#izci-api-v1-getownershipresponse) | GetOwnership returns a project's ownership configuration. |
| [UpdateOwnership](#izci-api-v1-ownershipservice-updateownership) | [UpdateOwnershipRequest](#izci-api-v1-updateownershiprequest) | [UpdateOwnershipResponse](#izci-api-v1-updateownershipresponse) | UpdateOwnership changes the rules, the CODEOWNERS text or automatic assignment; unset fields stay. |
| [ImportCodeowners](#izci-api-v1-ownershipservice-importcodeowners) | [ImportCodeownersRequest](#izci-api-v1-importcodeownersrequest) | [ImportCodeownersResponse](#izci-api-v1-importcodeownersresponse) | ImportCodeowners reads CODEOWNERS from the project's linked GitHub repository (".github/CODEOWNERS", "CODEOWNERS" or "docs/CODEOWNERS" on the default branch) and stores it. |
| [ListOwnerAliases](#izci-api-v1-ownershipservice-listowneraliases) | [ListOwnerAliasesRequest](#izci-api-v1-listowneraliasesrequest) | [ListOwnerAliasesResponse](#izci-api-v1-listowneraliasesresponse) | ListOwnerAliases lists the organization's owner aliases. |
| [SetOwnerAlias](#izci-api-v1-ownershipservice-setowneralias) | [SetOwnerAliasRequest](#izci-api-v1-setowneraliasrequest) | [SetOwnerAliasResponse](#izci-api-v1-setowneraliasresponse) | SetOwnerAlias maps an alias to members; an empty member list removes it. |
| [GetIssueOwners](#izci-api-v1-ownershipservice-getissueowners) | [GetIssueOwnersRequest](#izci-api-v1-getissueownersrequest) | [GetIssueOwnersResponse](#izci-api-v1-getissueownersresponse) | GetIssueOwners returns an issue's owners by its latest event and the current rules, and its stored suspect commit. |
| [FindSuspectCommit](#izci-api-v1-ownershipservice-findsuspectcommit) | [FindSuspectCommitRequest](#izci-api-v1-findsuspectcommitrequest) | [FindSuspectCommitResponse](#izci-api-v1-findsuspectcommitresponse) | FindSuspectCommit searches, or searches again, for an issue's suspect commit: git blame, through GitHub, of the innermost in-app frames of its latest event at the release's commit (the default branch when the release has none). |

<a id="izci-api-v1-ownershipservice-getownership"></a>

#### OwnershipService.GetOwnership

`rpc GetOwnership(GetOwnershipRequest) returns (GetOwnershipResponse)`

GetOwnership returns a project's ownership configuration. Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-ownershipservice-updateownership"></a>

#### OwnershipService.UpdateOwnership

`rpc UpdateOwnership(UpdateOwnershipRequest) returns (UpdateOwnershipResponse)`

UpdateOwnership changes the rules, the CODEOWNERS text or automatic
assignment; unset fields stay. An invalid rule line fails the call with
ERROR_REASON_INVALID_ARGUMENT on field "rules" and metadata "line",
"kind" (a RuleProblemKind name) and "value". CODEOWNERS text is stored
even with invalid lines, which Ownership.problems lists. Requires
ROLE_ADMIN.

<a id="izci-api-v1-ownershipservice-importcodeowners"></a>

#### OwnershipService.ImportCodeowners

`rpc ImportCodeowners(ImportCodeownersRequest) returns (ImportCodeownersResponse)`

ImportCodeowners reads CODEOWNERS from the project's linked GitHub
repository (".github/CODEOWNERS", "CODEOWNERS" or "docs/CODEOWNERS" on
the default branch) and stores it. Fails with
ERROR_REASON_REPOSITORY_NOT_LINKED without a repository linked through
a GitHub connection, ERROR_REASON_NOT_FOUND when the repository has no
CODEOWNERS file and ERROR_REASON_INTEGRATION_FAILED when GitHub
refuses. Requires ROLE_ADMIN.

<a id="izci-api-v1-ownershipservice-listowneraliases"></a>

#### OwnershipService.ListOwnerAliases

`rpc ListOwnerAliases(ListOwnerAliasesRequest) returns (ListOwnerAliasesResponse)`

ListOwnerAliases lists the organization's owner aliases. Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-ownershipservice-setowneralias"></a>

#### OwnershipService.SetOwnerAlias

`rpc SetOwnerAlias(SetOwnerAliasRequest) returns (SetOwnerAliasResponse)`

SetOwnerAlias maps an alias to members; an empty member list removes
it. Requires ROLE_ADMIN.

<a id="izci-api-v1-ownershipservice-getissueowners"></a>

#### OwnershipService.GetIssueOwners

`rpc GetIssueOwners(GetIssueOwnersRequest) returns (GetIssueOwnersResponse)`

GetIssueOwners returns an issue's owners by its latest event and the
current rules, and its stored suspect commit. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-ownershipservice-findsuspectcommit"></a>

#### OwnershipService.FindSuspectCommit

`rpc FindSuspectCommit(FindSuspectCommitRequest) returns (FindSuspectCommitResponse)`

FindSuspectCommit searches, or searches again, for an issue's suspect
commit: git blame, through GitHub, of the innermost in-app frames of
its latest event at the release's commit (the default branch when the
release has none). The result is stored. Requires ROLE_VIEWER.

<a id="izci-api-v1-projectservice"></a>

### ProjectService

ProjectService manages projects of the current organization, their ingest
API keys, settings, environments, releases and linked repositories.

A project corresponds to one application (e.g. "KentOS Kalem"). Every
event belongs to exactly one project, determined by the API key it was sent
with.

| Method | Request | Response | Summary |
|---|---|---|---|
| [ListProjects](#izci-api-v1-projectservice-listprojects) | [ListProjectsRequest](#izci-api-v1-listprojectsrequest) | [ListProjectsResponse](#izci-api-v1-listprojectsresponse) | ListProjects lists the projects of the organization with 24-hour stats. |
| [GetProject](#izci-api-v1-projectservice-getproject) | [GetProjectRequest](#izci-api-v1-getprojectrequest) | [GetProjectResponse](#izci-api-v1-getprojectresponse) | GetProject returns one project. |
| [CreateProject](#izci-api-v1-projectservice-createproject) | [CreateProjectRequest](#izci-api-v1-createprojectrequest) | [CreateProjectResponse](#izci-api-v1-createprojectresponse) | CreateProject creates a project and its first API key. |
| [UpdateProject](#izci-api-v1-projectservice-updateproject) | [UpdateProjectRequest](#izci-api-v1-updateprojectrequest) | [UpdateProjectResponse](#izci-api-v1-updateprojectresponse) | UpdateProject changes name, platform and settings. |
| [DeleteProject](#izci-api-v1-projectservice-deleteproject) | [DeleteProjectRequest](#izci-api-v1-deleteprojectrequest) | [DeleteProjectResponse](#izci-api-v1-deleteprojectresponse) | DeleteProject deletes a project with all its issues and events. |
| [ListApiKeys](#izci-api-v1-projectservice-listapikeys) | [ListApiKeysRequest](#izci-api-v1-listapikeysrequest) | [ListApiKeysResponse](#izci-api-v1-listapikeysresponse) | ListApiKeys lists the project's API keys (without secrets). |
| [CreateApiKey](#izci-api-v1-projectservice-createapikey) | [CreateApiKeyRequest](#izci-api-v1-createapikeyrequest) | [CreateApiKeyResponse](#izci-api-v1-createapikeyresponse) | CreateApiKey creates an API key. |
| [UpdateApiKey](#izci-api-v1-projectservice-updateapikey) | [UpdateApiKeyRequest](#izci-api-v1-updateapikeyrequest) | [UpdateApiKeyResponse](#izci-api-v1-updateapikeyresponse) | UpdateApiKey changes name, allowed origins, rate limit or enabled state. |
| [RevokeApiKey](#izci-api-v1-projectservice-revokeapikey) | [RevokeApiKeyRequest](#izci-api-v1-revokeapikeyrequest) | [RevokeApiKeyResponse](#izci-api-v1-revokeapikeyresponse) | RevokeApiKey permanently revokes an API key; clients using it are rejected within seconds. |
| [GetIngestEndpoints](#izci-api-v1-projectservice-getingestendpoints) | [GetIngestEndpointsRequest](#izci-api-v1-getingestendpointsrequest) | [GetIngestEndpointsResponse](#izci-api-v1-getingestendpointsresponse) | GetIngestEndpoints returns the addresses applications use to send events (gRPC target, Connect URL, Sentry DSN template). |
| [GetFilterStats](#izci-api-v1-projectservice-getfilterstats) | [GetFilterStatsRequest](#izci-api-v1-getfilterstatsrequest) | [GetFilterStatsResponse](#izci-api-v1-getfilterstatsresponse) | GetFilterStats counts the events the project's inbound filters dropped per reason and day, for the last 30 days. |
| [SendTestEvent](#izci-api-v1-projectservice-sendtestevent) | [SendTestEventRequest](#izci-api-v1-sendtesteventrequest) | [SendTestEventResponse](#izci-api-v1-sendtesteventresponse) | SendTestEvent sends a sample event through the full ingest pipeline so the setup can be verified end to end. |
| [ListEnvironments](#izci-api-v1-projectservice-listenvironments) | [ListEnvironmentsRequest](#izci-api-v1-listenvironmentsrequest) | [ListEnvironmentsResponse](#izci-api-v1-listenvironmentsresponse) | ListEnvironments lists the environments seen in the project. |
| [ListReleases](#izci-api-v1-projectservice-listreleases) | [ListReleasesRequest](#izci-api-v1-listreleasesrequest) | [ListReleasesResponse](#izci-api-v1-listreleasesresponse) | ListReleases lists releases, newest first. |
| [LinkRepository](#izci-api-v1-projectservice-linkrepository) | [LinkRepositoryRequest](#izci-api-v1-linkrepositoryrequest) | [LinkRepositoryResponse](#izci-api-v1-linkrepositoryresponse) | LinkRepository links the project to a source repository, used for source links, AI analysis and fix pull requests. |
| [UnlinkRepository](#izci-api-v1-projectservice-unlinkrepository) | [UnlinkRepositoryRequest](#izci-api-v1-unlinkrepositoryrequest) | [UnlinkRepositoryResponse](#izci-api-v1-unlinkrepositoryresponse) | UnlinkRepository removes the repository link. |

<a id="izci-api-v1-projectservice-listprojects"></a>

#### ProjectService.ListProjects

`rpc ListProjects(ListProjectsRequest) returns (ListProjectsResponse)`

ListProjects lists the projects of the organization with 24-hour stats.
Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-projectservice-getproject"></a>

#### ProjectService.GetProject

`rpc GetProject(GetProjectRequest) returns (GetProjectResponse)`

GetProject returns one project. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-projectservice-createproject"></a>

#### ProjectService.CreateProject

`rpc CreateProject(CreateProjectRequest) returns (CreateProjectResponse)`

CreateProject creates a project and its first API key. Requires
ROLE_ADMIN. Fails with ERROR_REASON_LIMIT_REACHED at the plan limit.

<a id="izci-api-v1-projectservice-updateproject"></a>

#### ProjectService.UpdateProject

`rpc UpdateProject(UpdateProjectRequest) returns (UpdateProjectResponse)`

UpdateProject changes name, platform and settings. Requires ROLE_ADMIN.

<a id="izci-api-v1-projectservice-deleteproject"></a>

#### ProjectService.DeleteProject

`rpc DeleteProject(DeleteProjectRequest) returns (DeleteProjectResponse)`

DeleteProject deletes a project with all its issues and events. Requires
ROLE_OWNER. Irreversible.

<a id="izci-api-v1-projectservice-listapikeys"></a>

#### ProjectService.ListApiKeys

`rpc ListApiKeys(ListApiKeysRequest) returns (ListApiKeysResponse)`

ListApiKeys lists the project's API keys (without secrets). Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-projectservice-createapikey"></a>

#### ProjectService.CreateApiKey

`rpc CreateApiKey(CreateApiKeyRequest) returns (CreateApiKeyResponse)`

CreateApiKey creates an API key. The secret is returned only once.
Requires ROLE_ADMIN.

<a id="izci-api-v1-projectservice-updateapikey"></a>

#### ProjectService.UpdateApiKey

`rpc UpdateApiKey(UpdateApiKeyRequest) returns (UpdateApiKeyResponse)`

UpdateApiKey changes name, allowed origins, rate limit or enabled state.
Requires ROLE_ADMIN.

<a id="izci-api-v1-projectservice-revokeapikey"></a>

#### ProjectService.RevokeApiKey

`rpc RevokeApiKey(RevokeApiKeyRequest) returns (RevokeApiKeyResponse)`

RevokeApiKey permanently revokes an API key; clients using it are
rejected within seconds. Requires ROLE_ADMIN.

<a id="izci-api-v1-projectservice-getingestendpoints"></a>

#### ProjectService.GetIngestEndpoints

`rpc GetIngestEndpoints(GetIngestEndpointsRequest) returns (GetIngestEndpointsResponse)`

GetIngestEndpoints returns the addresses applications use to send events
(gRPC target, Connect URL, Sentry DSN template). Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-projectservice-getfilterstats"></a>

#### ProjectService.GetFilterStats

`rpc GetFilterStats(GetFilterStatsRequest) returns (GetFilterStatsResponse)`

GetFilterStats counts the events the project's inbound filters dropped
per reason and day, for the last 30 days. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-projectservice-sendtestevent"></a>

#### ProjectService.SendTestEvent

`rpc SendTestEvent(SendTestEventRequest) returns (SendTestEventResponse)`

SendTestEvent sends a sample event through the full ingest pipeline so
the setup can be verified end to end. Requires ROLE_MEMBER.

<a id="izci-api-v1-projectservice-listenvironments"></a>

#### ProjectService.ListEnvironments

`rpc ListEnvironments(ListEnvironmentsRequest) returns (ListEnvironmentsResponse)`

ListEnvironments lists the environments seen in the project. Requires
ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-projectservice-listreleases"></a>

#### ProjectService.ListReleases

`rpc ListReleases(ListReleasesRequest) returns (ListReleasesResponse)`

ListReleases lists releases, newest first. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-projectservice-linkrepository"></a>

#### ProjectService.LinkRepository

`rpc LinkRepository(LinkRepositoryRequest) returns (LinkRepositoryResponse)`

LinkRepository links the project to a source repository, used for
source links, AI analysis and fix pull requests. Requires ROLE_ADMIN.

<a id="izci-api-v1-projectservice-unlinkrepository"></a>

#### ProjectService.UnlinkRepository

`rpc UnlinkRepository(UnlinkRepositoryRequest) returns (UnlinkRepositoryResponse)`

UnlinkRepository removes the repository link. Requires ROLE_ADMIN.

<a id="izci-api-v1-symbolservice"></a>

### SymbolService

SymbolService lists and removes the files izci resolves stack traces
with: source map bundles of web applications and the debug information
files and obfuscation maps of Flutter and Dart builds. They are uploaded
with sentry-cli, the Sentry bundler plugins (Vite, webpack, Rollup,
esbuild) or sentry_dart_plugin through izci's Sentry-compatible upload
endpoints (/api/0/...).

| Method | Request | Response | Summary |
|---|---|---|---|
| [ListArtifactBundles](#izci-api-v1-symbolservice-listartifactbundles) | [ListArtifactBundlesRequest](#izci-api-v1-listartifactbundlesrequest) | [ListArtifactBundlesResponse](#izci-api-v1-listartifactbundlesresponse) | ListArtifactBundles returns a project's source map bundles, newest first. |
| [DeleteArtifactBundle](#izci-api-v1-symbolservice-deleteartifactbundle) | [DeleteArtifactBundleRequest](#izci-api-v1-deleteartifactbundlerequest) | [DeleteArtifactBundleResponse](#izci-api-v1-deleteartifactbundleresponse) | DeleteArtifactBundle removes a bundle with its files; events that arrive later are no longer resolved with it. |
| [ListDebugFiles](#izci-api-v1-symbolservice-listdebugfiles) | [ListDebugFilesRequest](#izci-api-v1-listdebugfilesrequest) | [ListDebugFilesResponse](#izci-api-v1-listdebugfilesresponse) | ListDebugFiles returns a project's debug information files and obfuscation maps, newest first. |
| [DeleteDebugFile](#izci-api-v1-symbolservice-deletedebugfile) | [DeleteDebugFileRequest](#izci-api-v1-deletedebugfilerequest) | [DeleteDebugFileResponse](#izci-api-v1-deletedebugfileresponse) | DeleteDebugFile removes a debug file. |

<a id="izci-api-v1-symbolservice-listartifactbundles"></a>

#### SymbolService.ListArtifactBundles

`rpc ListArtifactBundles(ListArtifactBundlesRequest) returns (ListArtifactBundlesResponse)`

ListArtifactBundles returns a project's source map bundles, newest
first. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-symbolservice-deleteartifactbundle"></a>

#### SymbolService.DeleteArtifactBundle

`rpc DeleteArtifactBundle(DeleteArtifactBundleRequest) returns (DeleteArtifactBundleResponse)`

DeleteArtifactBundle removes a bundle with its files; events that
arrive later are no longer resolved with it. Requires ROLE_ADMIN.

<a id="izci-api-v1-symbolservice-listdebugfiles"></a>

#### SymbolService.ListDebugFiles

`rpc ListDebugFiles(ListDebugFilesRequest) returns (ListDebugFilesResponse)`

ListDebugFiles returns a project's debug information files and
obfuscation maps, newest first. Requires ROLE_VIEWER.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-symbolservice-deletedebugfile"></a>

#### SymbolService.DeleteDebugFile

`rpc DeleteDebugFile(DeleteDebugFileRequest) returns (DeleteDebugFileResponse)`

DeleteDebugFile removes a debug file. Requires ROLE_ADMIN.

<a id="izci-api-v1-userservice"></a>

### UserService

UserService manages the signed-in user's own account: profile, personal
access tokens, sessions, two-factor authentication and the linked
Telegram account. It does not use the Izci-Org header.

| Method | Request | Response | Summary |
|---|---|---|---|
| [UpdateProfile](#izci-api-v1-userservice-updateprofile) | [UpdateProfileRequest](#izci-api-v1-updateprofilerequest) | [UpdateProfileResponse](#izci-api-v1-updateprofileresponse) | UpdateProfile changes the display name. |
| [ListAccessTokens](#izci-api-v1-userservice-listaccesstokens) | [ListAccessTokensRequest](#izci-api-v1-listaccesstokensrequest) | [ListAccessTokensResponse](#izci-api-v1-listaccesstokensresponse) | ListAccessTokens lists the user's personal access tokens. |
| [CreateAccessToken](#izci-api-v1-userservice-createaccesstoken) | [CreateAccessTokenRequest](#izci-api-v1-createaccesstokenrequest) | [CreateAccessTokenResponse](#izci-api-v1-createaccesstokenresponse) | CreateAccessToken creates a personal access token for the API, the CLI and MCP clients. |
| [RevokeAccessToken](#izci-api-v1-userservice-revokeaccesstoken) | [RevokeAccessTokenRequest](#izci-api-v1-revokeaccesstokenrequest) | [RevokeAccessTokenResponse](#izci-api-v1-revokeaccesstokenresponse) | RevokeAccessToken revokes a token immediately. |
| [ListSessions](#izci-api-v1-userservice-listsessions) | [ListSessionsRequest](#izci-api-v1-listsessionsrequest) | [ListSessionsResponse](#izci-api-v1-listsessionsresponse) | ListSessions lists the user's active browser sessions. |
| [RevokeSession](#izci-api-v1-userservice-revokesession) | [RevokeSessionRequest](#izci-api-v1-revokesessionrequest) | [RevokeSessionResponse](#izci-api-v1-revokesessionresponse) | RevokeSession ends one of the user's sessions (or all other sessions). |
| [CreateTelegramAccountLinkCode](#izci-api-v1-userservice-createtelegramaccountlinkcode) | [CreateTelegramAccountLinkCodeRequest](#izci-api-v1-createtelegramaccountlinkcoderequest) | [CreateTelegramAccountLinkCodeResponse](#izci-api-v1-createtelegramaccountlinkcoderesponse) | CreateTelegramAccountLinkCode creates a one-time code (valid 15 minutes) that links the user's Telegram account: send "/ben <code>" to the bot. |
| [UnlinkTelegramAccount](#izci-api-v1-userservice-unlinktelegramaccount) | [UnlinkTelegramAccountRequest](#izci-api-v1-unlinktelegramaccountrequest) | [UnlinkTelegramAccountResponse](#izci-api-v1-unlinktelegramaccountresponse) | UnlinkTelegramAccount removes the linked Telegram account. |
| [BeginTwoFactorSetup](#izci-api-v1-userservice-begintwofactorsetup) | [BeginTwoFactorSetupRequest](#izci-api-v1-begintwofactorsetuprequest) | [BeginTwoFactorSetupResponse](#izci-api-v1-begintwofactorsetupresponse) | BeginTwoFactorSetup starts turning on two-factor authentication: it returns a new secret for an authenticator app, which EnableTwoFactor confirms within an hour. |
| [EnableTwoFactor](#izci-api-v1-userservice-enabletwofactor) | [EnableTwoFactorRequest](#izci-api-v1-enabletwofactorrequest) | [EnableTwoFactorResponse](#izci-api-v1-enabletwofactorresponse) | EnableTwoFactor confirms the setup with a code from the authenticator app and turns two-factor authentication on. |
| [DisableTwoFactor](#izci-api-v1-userservice-disabletwofactor) | [DisableTwoFactorRequest](#izci-api-v1-disabletwofactorrequest) | [DisableTwoFactorResponse](#izci-api-v1-disabletwofactorresponse) | DisableTwoFactor turns two-factor authentication off, given a current code or a recovery code. |
| [RegenerateRecoveryCodes](#izci-api-v1-userservice-regeneraterecoverycodes) | [RegenerateRecoveryCodesRequest](#izci-api-v1-regeneraterecoverycodesrequest) | [RegenerateRecoveryCodesResponse](#izci-api-v1-regeneraterecoverycodesresponse) | RegenerateRecoveryCodes replaces the recovery codes, given a current code; the old codes stop working. |

<a id="izci-api-v1-userservice-updateprofile"></a>

#### UserService.UpdateProfile

`rpc UpdateProfile(UpdateProfileRequest) returns (UpdateProfileResponse)`

UpdateProfile changes the display name.

<a id="izci-api-v1-userservice-listaccesstokens"></a>

#### UserService.ListAccessTokens

`rpc ListAccessTokens(ListAccessTokensRequest) returns (ListAccessTokensResponse)`

ListAccessTokens lists the user's personal access tokens.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-userservice-createaccesstoken"></a>

#### UserService.CreateAccessToken

`rpc CreateAccessToken(CreateAccessTokenRequest) returns (CreateAccessTokenResponse)`

CreateAccessToken creates a personal access token for the API, the CLI
and MCP clients. The secret is returned only once. A token never grants
more than the user's role in its organization.

<a id="izci-api-v1-userservice-revokeaccesstoken"></a>

#### UserService.RevokeAccessToken

`rpc RevokeAccessToken(RevokeAccessTokenRequest) returns (RevokeAccessTokenResponse)`

RevokeAccessToken revokes a token immediately.

<a id="izci-api-v1-userservice-listsessions"></a>

#### UserService.ListSessions

`rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse)`

ListSessions lists the user's active browser sessions.

_Side-effect free: may be called with HTTP GET over the Connect protocol._

<a id="izci-api-v1-userservice-revokesession"></a>

#### UserService.RevokeSession

`rpc RevokeSession(RevokeSessionRequest) returns (RevokeSessionResponse)`

RevokeSession ends one of the user's sessions (or all other sessions).

<a id="izci-api-v1-userservice-createtelegramaccountlinkcode"></a>

#### UserService.CreateTelegramAccountLinkCode

`rpc CreateTelegramAccountLinkCode(CreateTelegramAccountLinkCodeRequest) returns (CreateTelegramAccountLinkCodeResponse)`

CreateTelegramAccountLinkCode creates a one-time code (valid 15 minutes)
that links the user's Telegram account: send "/ben <code>" to the bot.

<a id="izci-api-v1-userservice-unlinktelegramaccount"></a>

#### UserService.UnlinkTelegramAccount

`rpc UnlinkTelegramAccount(UnlinkTelegramAccountRequest) returns (UnlinkTelegramAccountResponse)`

UnlinkTelegramAccount removes the linked Telegram account.

<a id="izci-api-v1-userservice-begintwofactorsetup"></a>

#### UserService.BeginTwoFactorSetup

`rpc BeginTwoFactorSetup(BeginTwoFactorSetupRequest) returns (BeginTwoFactorSetupResponse)`

BeginTwoFactorSetup starts turning on two-factor authentication: it
returns a new secret for an authenticator app, which EnableTwoFactor
confirms within an hour. Browser sessions only; fails with
ERROR_REASON_CONFLICT when two-factor authentication is on already.

<a id="izci-api-v1-userservice-enabletwofactor"></a>

#### UserService.EnableTwoFactor

`rpc EnableTwoFactor(EnableTwoFactorRequest) returns (EnableTwoFactorResponse)`

EnableTwoFactor confirms the setup with a code from the authenticator
app and turns two-factor authentication on. The response carries the
recovery codes, which are never shown again. Browser sessions only.

<a id="izci-api-v1-userservice-disabletwofactor"></a>

#### UserService.DisableTwoFactor

`rpc DisableTwoFactor(DisableTwoFactorRequest) returns (DisableTwoFactorResponse)`

DisableTwoFactor turns two-factor authentication off, given a current
code or a recovery code. Organizations requiring it turn the user away
until it is on again. Browser sessions only.

<a id="izci-api-v1-userservice-regeneraterecoverycodes"></a>

#### UserService.RegenerateRecoveryCodes

`rpc RegenerateRecoveryCodes(RegenerateRecoveryCodesRequest) returns (RegenerateRecoveryCodesResponse)`

RegenerateRecoveryCodes replaces the recovery codes, given a current
code; the old codes stop working. Browser sessions only.

## Messages

<a id="izci-api-v1-acceptinvitationrequest"></a>

### AcceptInvitationRequest

AcceptInvitationRequest accepts an invitation.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `token` | `string` | Invitation token from the invitation link. | min length 16, max length 128 |
| `name` | `string` | Display name for a new account; ignored for signed-in users. | max length 200 |
| `password` | `string` | Password for a new account (at least 10 characters); ignored for signed-in users. | max length 200 |

<a id="izci-api-v1-acceptinvitationresponse"></a>

### AcceptInvitationResponse

AcceptInvitationResponse returns the resulting session.

| Field | Type | Description |
|---|---|---|
| `session` | [Session](#izci-api-v1-session) | Session of the user, now a member of the organization. |

<a id="izci-api-v1-accesstoken"></a>

### AccessToken

AccessToken is a personal access token.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Token id (UUID). |
| `name` | `string` | Descriptive name, e.g. "Claude Code on laptop". |
| `prefix` | `string` | First characters of the token, e.g. "izp_8c1d0e". |
| `organization` | [OrganizationRef](#izci-api-v1-organizationref) | Organization the token is bound to; unset for tokens valid in all of the user's organizations. |
| `scopes` | repeated [TokenScope](#izci-api-v1-tokenscope) | Granted scopes. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the token was created. |
| `last_used_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the token was last used. |
| `expires_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the token expires; unset for tokens without expiry. |

<a id="izci-api-v1-addissuecommentrequest"></a>

### AddIssueCommentRequest

AddIssueCommentRequest adds a comment.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Issue id. | UUID |
| `text` | `string` | Markdown text, 1–10000 characters. | min length 1, max length 10000 |

<a id="izci-api-v1-addissuecommentresponse"></a>

### AddIssueCommentResponse

AddIssueCommentResponse returns the comment.

| Field | Type | Description |
|---|---|---|
| `activity` | [IssueActivity](#izci-api-v1-issueactivity) | The created comment. |

<a id="izci-api-v1-adminorganization"></a>

### AdminOrganization

AdminOrganization is an organization with instance-level statistics.

| Field | Type | Description |
|---|---|---|
| `organization` | [Organization](#izci-api-v1-organization) | The organization. |
| `month_events` | `uint64` | Events accepted this month. |
| `last_event_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the organization last received an event. |

<a id="izci-api-v1-adminuser"></a>

### AdminUser

AdminUser is a user with instance-level details.

| Field | Type | Description |
|---|---|---|
| `user` | [User](#izci-api-v1-user) | The user. |
| `disabled` | `bool` | Disabled users cannot sign in and their tokens are rejected. |
| `last_login_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Last successful sign-in. |
| `organization_count` | `uint32` | Number of organizations the user belongs to. |

<a id="izci-api-v1-ailogentry"></a>

### AiLogEntry

AiLogEntry is one line of a run log.

| Field | Type | Description |
|---|---|---|
| `time` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When it was recorded. |
| `kind` | [AiLogKind](#izci-api-v1-ailogkind) | Kind of entry. |
| `message` | `string` | Text of the entry. |
| `tool` | `string` | Tool name for AI_LOG_KIND_TOOL, e.g. "read", "edit", "bash". |

<a id="izci-api-v1-airesult"></a>

### AiResult

AiResult is the structured outcome of an analysis.

| Field | Type | Description |
|---|---|---|
| `summary` | `string` | One-paragraph summary. |
| `root_cause` | `string` | Root cause explanation. |
| `fix_summary` | `string` | What the proposed fix changes and why. |
| `confidence` | `double` | Agent's confidence in the root cause and fix, 0.0–1.0. |
| `risk` | [Risk](#izci-api-v1-risk) | Risk of the proposed change. |
| `test_suggestions` | repeated `string` | Suggested tests or verification steps. |

<a id="izci-api-v1-airun"></a>

### AiRun

AiRun is one AI analysis of an issue.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Run id (UUID). |
| `issue` | [IssueRef](#izci-api-v1-issueref) | Analyzed issue. |
| `project` | [ProjectRef](#izci-api-v1-projectref) | Project of the issue. |
| `event_id` | `string` | Event the analysis focused on. |
| `status` | [AiRunStatus](#izci-api-v1-airunstatus) | Current status. |
| `trigger` | [AiRunTrigger](#izci-api-v1-airuntrigger) | What started the run. |
| `mode` | [AiRunMode](#izci-api-v1-airunmode) | Whether code changes were requested. |
| `model` | `string` | opencode model in "provider/model" form. |
| `requested_by` | [UserRef](#izci-api-v1-userref) | Who requested the run; empty for automatic runs. |
| `base_commit` | `string` | Git commit the analysis ran against. |
| `base_branch` | `string` | Branch the fix is based on (the default branch unless the release commit was used). |
| `result` | [AiResult](#izci-api-v1-airesult) | Structured result of a finished run. |
| `analysis_markdown` | `string` | Full analysis report (Turkish Markdown) of a finished run. |
| `diff` | `string` | Proposed changes as a unified diff; empty without changes. |
| `files` | repeated [ChangedFile](#izci-api-v1-changedfile) | Files touched by the diff. |
| `pull_request` | [PullRequestRef](#izci-api-v1-pullrequestref) | Branch or pull request created from the run. |
| `error` | `string` | Error of a failed run. |
| `usage` | [AiUsage](#izci-api-v1-aiusage) | Model usage. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the run was queued. |
| `started_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the agent started. |
| `finished_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the run finished. |
| `sensitive_files` | repeated `string` | Changed files that CI, build tools, package managers or git act on (workflows, build scripts, dependency manifests and lockfiles, repository settings). A change there runs code with the repository's secrets once CI builds the branch: automatic publishing never pushes it, and CreateBranch and CreatePullRequest need allow_sensitive_changes. |

<a id="izci-api-v1-aisettings"></a>

### AiSettings

AiSettings configure automatic AI analysis for the project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `auto_analyze` | [AutoAnalyze](#izci-api-v1-autoanalyze) | When to start analyses automatically. |  |
| `min_level` | [izci.ingest.v1.Level](izci.ingest.v1.md#izci-ingest-v1-level) | Minimum level of issues analyzed automatically. |  |
| `auto_action` | [AutoAction](#izci-api-v1-autoaction) | What to do automatically after a successful analysis. |  |
| `daily_budget` | `uint32` | Maximum automatic runs per day (0 = no automatic runs). | ≤ 1000 |
| `model` | `string` | opencode model override in "provider/model" form; empty uses the instance default. | max length 128 |
| `allowed_commands` | repeated `string` | Shell commands the AI agent may run in the repository, as prefixes, e.g. ["go test", "dotnet build", "pnpm test"]. Everything else is denied. | max 30 items |
| `instructions` | `string` | Extra instructions appended to every prompt, e.g. coding conventions of the repository. Max 4000 characters. | max length 4000 |

<a id="izci-api-v1-aiusage"></a>

### AiUsage

AiUsage is the model usage of a run.

| Field | Type | Description |
|---|---|---|
| `input_tokens` | `uint64` | Input tokens. |
| `output_tokens` | `uint64` | Output tokens. |
| `cost_usd` | `double` | Estimated cost in US dollars, when reported by opencode. |

<a id="izci-api-v1-alertfilters"></a>

### AlertFilters

AlertFilters restrict which events can trigger a rule.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `min_level` | [izci.ingest.v1.Level](izci.ingest.v1.md#izci-ingest-v1-level) | Minimum level. |  |
| `environments` | repeated `string` | Environments; empty allows all. | max 20 items |
| `customers` | repeated `string` | Customer keys; empty allows all. | max 50 items |
| `tags` | map&lt;`string`, `string`&gt; | Tags that must match exactly. |  |
| `unhandled_only` | `bool` | Only unhandled (crashing) events. |  |

<a id="izci-api-v1-alertrule"></a>

### AlertRule

AlertRule decides when to notify which channels.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Rule id (UUID). |
| `name` | `string` | Descriptive name, e.g. "Production errors". |
| `project` | [ProjectRef](#izci-api-v1-projectref) | Project the rule applies to; unset applies to all projects. |
| `enabled` | `bool` | Disabled rules never fire. |
| `trigger` | [AlertTrigger](#izci-api-v1-alerttrigger) | What makes the rule fire. |
| `filters` | [AlertFilters](#izci-api-v1-alertfilters) | Conditions every triggering event must meet. |
| `channel_ids` | repeated `string` | Channels to notify. |
| `throttle` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | Minimum time between two notifications for the same issue. Repeated events within this window update the existing message instead. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the rule was created. |
| `updated_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the rule was last changed. |
| `last_triggered_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the rule last fired. |

<a id="izci-api-v1-alertruleinput"></a>

### AlertRuleInput

AlertRuleInput holds the editable fields of a rule.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | Descriptive name. | min length 1, max length 100 |
| `project` | `string` | Project slug; empty applies to all projects. | max length 50 |
| `enabled` | `bool` | Whether the rule is active. |  |
| `trigger` | [AlertTrigger](#izci-api-v1-alerttrigger) | Trigger. | required |
| `filters` | [AlertFilters](#izci-api-v1-alertfilters) | Filters. |  |
| `channel_ids` | repeated `string` | Channel ids to notify, 1 to 20. | min 1 items, max 20 items, each UUID |
| `throttle` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | Throttle per issue; defaults to 30 minutes, max 7 days. |  |

<a id="izci-api-v1-alerttrigger"></a>

### AlertTrigger

AlertTrigger is the firing condition of a rule.

| Field | Type | Description |
|---|---|---|
| `kind` | [TriggerKind](#izci-api-v1-triggerkind) | Kind of trigger. |
| `threshold` | `uint32` | For TRIGGER_KIND_FREQUENCY: number of events of one issue within window. |
| `window` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | For TRIGGER_KIND_FREQUENCY: the sliding window, 1 minute to 24 hours. |

<a id="izci-api-v1-apikey"></a>

### ApiKey

ApiKey is an ingest API key of a project.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Key id (UUID). |
| `name` | `string` | Descriptive name, e.g. "production backend". |
| `prefix` | `string` | First characters of the key for identification, e.g. "izk_4f9c2a". |
| `scopes` | repeated [ApiKeyScope](#izci-api-v1-apikeyscope) | What the key may do. |
| `allowed_origins` | repeated `string` | Browser origins allowed to use the key, e.g. ["https://kalem.example.com"] or ["https://*.example.com"]. Empty allows every origin, as for Sentry DSNs; requests without an Origin header (server-side applications) are never restricted. |
| `rate_limit_per_minute` | `uint32` | Maximum events per minute accepted with this key (0 = project limit). |
| `enabled` | `bool` | Disabled keys are rejected but can be re-enabled. |
| `created_by` | [UserRef](#izci-api-v1-userref) | Who created the key. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the key was created. |
| `last_used_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the key was last used (updated at most once per minute). |
| `browser_seen_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When a browser first sent events with this key while it had the releases scope. Such a key is public, so it can no longer register releases or upload files; create a separate key for CI. |

<a id="izci-api-v1-artifactbundle"></a>

### ArtifactBundle

ArtifactBundle is an upload of minified scripts and their source maps.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Bundle id (UUID). |
| `release` | `string` | Release the bundle was uploaded for; empty when its files are matched by debug id only. |
| `dist` | `string` | Distribution within the release, e.g. a build number; often empty. |
| `file_count` | `uint32` | Number of files. |
| `size_bytes` | `uint64` | Size of the uploaded archive in bytes. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the bundle was uploaded. |
| `last_used_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When a file of the bundle last resolved a stack frame (updated at most hourly); unset when never used. |
| `files` | repeated [ArtifactFile](#izci-api-v1-artifactfile) | The bundle's files, at most 50 (scripts and maps first). |

<a id="izci-api-v1-artifactfile"></a>

### ArtifactFile

ArtifactFile is one file of a bundle.

| Field | Type | Description |
|---|---|---|
| `url` | `string` | URL the file is served under, usually without host: "~/assets/index-9f8e.js". |
| `kind` | [ArtifactFileKind](#izci-api-v1-artifactfilekind) | What the file is. |
| `debug_id` | `string` | Debug id linking a script to its source map (UUID); empty for files matched by release and URL. |
| `size_bytes` | `uint32` | Size in bytes. |

<a id="izci-api-v1-auditlogentry"></a>

### AuditLogEntry

AuditLogEntry records one security-relevant change.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Entry id (UUID). |
| `actor` | [UserRef](#izci-api-v1-userref) | Who made the change; empty for system actions. |
| `action` | `string` | Action in "<resource>.<verb>" form, e.g. "api_key.create", "member.update", "integration.delete". |
| `target_type` | `string` | Kind of the changed resource, e.g. "api_key". |
| `target_id` | `string` | Id of the changed resource. |
| `target_name` | `string` | Human-readable name of the changed resource. |
| `metadata` | map&lt;`string`, `string`&gt; | Additional details, e.g. {"role": "ROLE_ADMIN"}. |
| `ip_address` | `string` | Client address of the actor. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the change happened. |

<a id="izci-api-v1-authprovider"></a>

### AuthProvider

AuthProvider is an OpenID Connect identity provider.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Provider id used in URLs, e.g. "kimlik". |
| `display_name` | `string` | Button label, e.g. "KentOS Kimlik". |
| `icon` | `string` | Icon hint for the button: "kimlik", "google", "microsoft", "keycloak", "github" or "generic". |
| `login_url` | `string` | Relative URL that starts the sign-in redirect, e.g. "/auth/oidc/kimlik/start". Append "?next=/path" to return to a page. |

<a id="izci-api-v1-begintwofactorsetuprequest"></a>

### BeginTwoFactorSetupRequest

BeginTwoFactorSetupRequest has no fields.

_No fields._

<a id="izci-api-v1-begintwofactorsetupresponse"></a>

### BeginTwoFactorSetupResponse

BeginTwoFactorSetupResponse carries the secret for the authenticator app.

| Field | Type | Description |
|---|---|---|
| `secret` | `string` | Secret in base32, for typing into the app when the QR code cannot be scanned. |
| `uri` | `string` | otpauth:// URI (SHA-1, 6 digits, 30 seconds) to show as a QR code. |

<a id="izci-api-v1-cancelrunrequest"></a>

### CancelRunRequest

CancelRunRequest identifies a run.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Run id. | UUID |

<a id="izci-api-v1-cancelrunresponse"></a>

### CancelRunResponse

CancelRunResponse returns the cancelled run.

| Field | Type | Description |
|---|---|---|
| `run` | [AiRun](#izci-api-v1-airun) | The run. |

<a id="izci-api-v1-changepasswordrequest"></a>

### ChangePasswordRequest

ChangePasswordRequest holds the old and new password.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `current_password` | `string` | Current password; may be empty only when the account has no password. | max length 200 |
| `new_password` | `string` | New password, at least 10 characters. | min length 10, max length 200 |

<a id="izci-api-v1-changepasswordresponse"></a>

### ChangePasswordResponse

ChangePasswordResponse has no fields.

_No fields._

<a id="izci-api-v1-changedfile"></a>

### ChangedFile

ChangedFile is one file of a diff.

| Field | Type | Description |
|---|---|---|
| `path` | `string` | Repository-relative path. |
| `additions` | `uint32` | Added lines. |
| `deletions` | `uint32` | Removed lines. |
| `status` | `string` | "added", "modified", "deleted" or "renamed". |

<a id="izci-api-v1-channel"></a>

### Channel

Channel is a notification destination.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Channel id (UUID). |
| `kind` | [ChannelKind](#izci-api-v1-channelkind) | Transport of the channel. |
| `name` | `string` | Display name, defaults to the chat title. |
| `telegram` | [TelegramChat](#izci-api-v1-telegramchat) | Telegram chat details for CHANNEL_KIND_TELEGRAM. |
| `project` | [ProjectRef](#izci-api-v1-projectref) | Project the channel was linked for; unset for organization-wide channels. Informational: rules decide what is delivered. |
| `muted` | `bool` | Muted channels receive nothing. |
| `created_by` | [UserRef](#izci-api-v1-userref) | Who linked the channel. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the channel was linked. |
| `last_delivery_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Last successful delivery. |
| `last_error` | `string` | Last delivery error, empty when the last delivery succeeded. |
| `email_recipients` | repeated `string` | Recipients of CHANNEL_KIND_EMAIL. |
| `url_hint` | `string` | Where a Slack, Teams or webhook channel posts, with the secret part of the URL hidden, e.g. "hooks.slack.com/…/x7Qa". |
| `signed` | `bool` | Whether webhook deliveries carry a signature (X-Izci-Signature). |
| `jira` | [JiraSettings](#izci-api-v1-jirasettings) | Jira settings of CHANNEL_KIND_JIRA (without the token). |

<a id="izci-api-v1-channelsettings"></a>

### ChannelSettings

ChannelSettings is the destination of a new or reconfigured channel.

| Field | Type | Description |
|---|---|---|
| `email` | [EmailSettings](#izci-api-v1-emailsettings) | _oneof `kind`._ E-mail recipients. |
| `slack` | [WebhookSettings](#izci-api-v1-webhooksettings) | _oneof `kind`._ Slack incoming webhook. |
| `teams` | [WebhookSettings](#izci-api-v1-webhooksettings) | _oneof `kind`._ Microsoft Teams webhook. |
| `webhook` | [WebhookSettings](#izci-api-v1-webhooksettings) | _oneof `kind`._ Generic webhook. |
| `jira` | [JiraInput](#izci-api-v1-jirainput) | _oneof `kind`._ Jira project. |

<a id="izci-api-v1-checkin"></a>

### CheckIn

CheckIn is one run of a monitored job.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Run id in izci (UUID). |
| `check_in_id` | `string` | Id the job gave the run (check_in_id); izci's own id for missed runs. |
| `environment` | `string` | Environment of the run. |
| `status` | [CheckInStatus](#izci-api-v1-checkinstatus) | Outcome, or IN_PROGRESS while running. |
| `duration` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | How long the run took; unset while in progress or unknown. |
| `release` | `string` | Release of the job's code, when reported. |
| `expected_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | The scheduled time the run answered, when known. |
| `started_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the run started (for missed runs: when izci noticed). |
| `updated_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the run was last updated. |

<a id="izci-api-v1-codeowners"></a>

### Codeowners

Codeowners is a project's CODEOWNERS file.

| Field | Type | Description |
|---|---|---|
| `content` | `string` | File content. |
| `source` | `string` | Where it was imported from, e.g. "kentos/kalem:.github/CODEOWNERS"; empty when pasted. |
| `synced_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When it was imported or pasted. |
| `rule_count` | `uint32` | Rules read from it. |

<a id="izci-api-v1-completesetuprequest"></a>

### CompleteSetupRequest

CompleteSetupRequest holds the first user, organization and project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | Display name of the first user. | min length 1, max length 200 |
| `email` | `string` | Email address of the first user. | email |
| `password` | `string` | Password of the first user, at least 10 characters. | min length 10, max length 200 |
| `organization_name` | `string` | Display name of the first organization, e.g. "KentOS". | min length 1, max length 100 |
| `organization_slug` | `string` | Slug of the first organization: 2–50 lowercase letters, digits or hyphens, starting with a letter, e.g. "kentos". | pattern `^[a-z][a-z0-9-]{0,48}[a-z0-9]$` |
| `project_name` | `string` | Display name of the first project, e.g. "KentOS Kalem". | min length 1, max length 100 |
| `project_platform` | `string` | Platform of the first project, e.g. "dotnet". | max length 32 |
| `setup_token` | `string` | Setup code from the server log ("setup_code" of the "izci is not set up yet" line) or IZCI_SETUP_TOKEN; case, spaces and dashes do not matter. It keeps whoever reaches a new server first from taking it over. | max length 128 |

<a id="izci-api-v1-completesetupresponse"></a>

### CompleteSetupResponse

CompleteSetupResponse returns the new session and the first API key.

| Field | Type | Description |
|---|---|---|
| `session` | [Session](#izci-api-v1-session) | Session of the new user. |
| `project_slug` | `string` | Slug of the created project. |
| `api_key` | `string` | Ingest API key of the created project. Shown only once. |

<a id="izci-api-v1-connectgithubtokenrequest"></a>

### ConnectGitHubTokenRequest

ConnectGitHubTokenRequest holds a token.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | Descriptive name. | min length 1, max length 100 |
| `token` | `string` | The token ("github_pat_…" or "ghp_…"). | min length 20, max length 255 |

<a id="izci-api-v1-connectgithubtokenresponse"></a>

### ConnectGitHubTokenResponse

ConnectGitHubTokenResponse returns the integration.

| Field | Type | Description |
|---|---|---|
| `integration` | [Integration](#izci-api-v1-integration) | The stored integration. |

<a id="izci-api-v1-createaccesstokenrequest"></a>

### CreateAccessTokenRequest

CreateAccessTokenRequest describes the token.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | Descriptive name. | min length 1, max length 100 |
| `organization` | `string` | Organization slug to bind the token to; empty for all organizations. | max length 50 |
| `scopes` | repeated [TokenScope](#izci-api-v1-tokenscope) | Scopes, at least one. | min 1 items, max 3 items |
| `expires_in` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | Lifetime; unset or zero creates a token without expiry. Max 1 year. |  |

<a id="izci-api-v1-createaccesstokenresponse"></a>

### CreateAccessTokenResponse

CreateAccessTokenResponse returns the token and its secret.

| Field | Type | Description |
|---|---|---|
| `token` | [AccessToken](#izci-api-v1-accesstoken) | The token. |
| `secret` | `string` | The full token ("izp_…"). Shown only once. |

<a id="izci-api-v1-createalertrulerequest"></a>

### CreateAlertRuleRequest

CreateAlertRuleRequest creates a rule.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `rule` | [AlertRuleInput](#izci-api-v1-alertruleinput) | The rule. | required |

<a id="izci-api-v1-createalertruleresponse"></a>

### CreateAlertRuleResponse

CreateAlertRuleResponse returns the rule.

| Field | Type | Description |
|---|---|---|
| `rule` | [AlertRule](#izci-api-v1-alertrule) | The created rule. |

<a id="izci-api-v1-createapikeyrequest"></a>

### CreateApiKeyRequest

CreateApiKeyRequest describes the new key.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `name` | `string` | Descriptive name. | min length 1, max length 100 |
| `scopes` | repeated [ApiKeyScope](#izci-api-v1-apikeyscope) | Scopes; defaults to [API_KEY_SCOPE_INGEST]. |  |
| `allowed_origins` | repeated `string` | Allowed browser origins, e.g. ["https://kalem.example.com"]. | max 20 items, each URI |
| `rate_limit_per_minute` | `uint32` | Maximum events per minute (0 = project limit). |  |

<a id="izci-api-v1-createapikeyresponse"></a>

### CreateApiKeyResponse

CreateApiKeyResponse returns the key and its secret.

| Field | Type | Description |
|---|---|---|
| `key` | [ApiKey](#izci-api-v1-apikey) | The new key. |
| `secret` | `string` | The full key ("izk_…"). Shown only once. |
| `sentry_dsn` | `string` | Ready-to-use Sentry DSN for this key. |

<a id="izci-api-v1-createbranchrequest"></a>

### CreateBranchRequest

CreateBranchRequest names the branch.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `run_id` | `string` | Run id. | UUID |
| `branch` | `string` | Branch name; defaults to "izci/fix/<short-id>-<slug>". | max length 200 |
| `allow_sensitive_changes` | `bool` | Confirms that the person reviewed the changes to AiRun.sensitive_files; without it such a run fails with ERROR_REASON_AI_SENSITIVE_CHANGES. |  |

<a id="izci-api-v1-createbranchresponse"></a>

### CreateBranchResponse

CreateBranchResponse returns the pushed branch.

| Field | Type | Description |
|---|---|---|
| `pull_request` | [PullRequestRef](#izci-api-v1-pullrequestref) | The branch (number 0, state "branch"). |

<a id="izci-api-v1-createchannelrequest"></a>

### CreateChannelRequest

CreateChannelRequest describes a new channel.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | Display name, e.g. "Ekip e-postası". | min length 1, max length 100 |
| `project` | `string` | Project slug the channel is meant for; empty for organization-wide. Informational: rules decide what is delivered. | max length 50 |
| `settings` | [ChannelSettings](#izci-api-v1-channelsettings) | Destination. | required |

<a id="izci-api-v1-createchannelresponse"></a>

### CreateChannelResponse

CreateChannelResponse returns the channel.

| Field | Type | Description |
|---|---|---|
| `channel` | [Channel](#izci-api-v1-channel) | The new channel. |
| `webhook_secret` | `string` | Signing secret of a webhook channel, shown only here: receivers verify X-Izci-Signature with it. |

<a id="izci-api-v1-createmonitorrequest"></a>

### CreateMonitorRequest

CreateMonitorRequest describes a new monitor.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. Required. | min length 1, max length 50 |
| `name` | `string` | Display name. Required. | min length 1, max length 100 |
| `slug` | `string` | Slug jobs use; derived from the name when empty. | max length 50 |
| `schedule` | [MonitorSchedule](#izci-api-v1-monitorschedule) | When runs are due. Required. | required |
| `timezone` | `string` | IANA time zone of the crontab; UTC when empty. | max length 64 |
| `checkin_margin_minutes` | `uint32` | Minutes a run may be late (default 1). | ≤ 43200 |
| `max_runtime_minutes` | `uint32` | Minutes a run may take (default 30). | ≤ 10080 |
| `failure_threshold` | `uint32` | Failed runs in a row that open an issue (default 1). | ≤ 720 |
| `recovery_threshold` | `uint32` | Successful runs in a row that resolve it (default 1). | ≤ 720 |

<a id="izci-api-v1-createmonitorresponse"></a>

### CreateMonitorResponse

CreateMonitorResponse holds the new monitor.

| Field | Type | Description |
|---|---|---|
| `monitor` | [Monitor](#izci-api-v1-monitor) | The created monitor. |

<a id="izci-api-v1-createorganizationrequest"></a>

### CreateOrganizationRequest

CreateOrganizationRequest describes the new organization.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | Display name. | min length 1, max length 100 |
| `slug` | `string` | Slug: 2–50 lowercase letters, digits or hyphens, starting with a letter. | pattern `^[a-z][a-z0-9-]{0,48}[a-z0-9]$` |

<a id="izci-api-v1-createorganizationresponse"></a>

### CreateOrganizationResponse

CreateOrganizationResponse returns the new organization.

| Field | Type | Description |
|---|---|---|
| `organization` | [Organization](#izci-api-v1-organization) | The new organization; the caller is its owner. |

<a id="izci-api-v1-createprojectrequest"></a>

### CreateProjectRequest

CreateProjectRequest describes the new project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | Display name. | min length 1, max length 100 |
| `slug` | `string` | Slug: 2–50 lowercase letters, digits or hyphens, starting with a letter. Derived from name when empty. | pattern `^$\|^[a-z][a-z0-9-]{0,48}[a-z0-9]$` |
| `platform` | `string` | Main platform, e.g. "dotnet". | max length 32 |

<a id="izci-api-v1-createprojectresponse"></a>

### CreateProjectResponse

CreateProjectResponse returns the project and its first key.

| Field | Type | Description |
|---|---|---|
| `project` | [Project](#izci-api-v1-project) | The new project. |
| `api_key` | [ApiKey](#izci-api-v1-apikey) | The first API key (scopes INGEST and RELEASES). |
| `api_key_secret` | `string` | Secret of the first API key. Shown only once. |

<a id="izci-api-v1-createpullrequestrequest"></a>

### CreatePullRequestRequest

CreatePullRequestRequest describes the pull request.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `run_id` | `string` | Run id. | UUID |
| `draft` | `bool` | Open as a draft (recommended). |  |
| `title` | `string` | Title; defaults to "fix: <issue title> (<short id>)". | max length 250 |
| `base_branch` | `string` | Target branch; defaults to the repository's default branch. | max length 200 |
| `allow_sensitive_changes` | `bool` | Confirms that the person reviewed the changes to AiRun.sensitive_files; without it such a run fails with ERROR_REASON_AI_SENSITIVE_CHANGES. |  |

<a id="izci-api-v1-createpullrequestresponse"></a>

### CreatePullRequestResponse

CreatePullRequestResponse returns the pull request.

| Field | Type | Description |
|---|---|---|
| `pull_request` | [PullRequestRef](#izci-api-v1-pullrequestref) | The opened pull request. |

<a id="izci-api-v1-createtelegramaccountlinkcoderequest"></a>

### CreateTelegramAccountLinkCodeRequest

CreateTelegramAccountLinkCodeRequest has no fields.

_No fields._

<a id="izci-api-v1-createtelegramaccountlinkcoderesponse"></a>

### CreateTelegramAccountLinkCodeResponse

CreateTelegramAccountLinkCodeResponse returns the code.

| Field | Type | Description |
|---|---|---|
| `code` | `string` | One-time code. |
| `bot_username` | `string` | Bot username without "@". |
| `link` | `string` | Link that opens a private chat with the bot with the command prefilled. |
| `expires_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the code expires. |

<a id="izci-api-v1-createtelegramlinkcoderequest"></a>

### CreateTelegramLinkCodeRequest

CreateTelegramLinkCodeRequest scopes the link.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug the chat is meant for; empty for organization-wide. | max length 50 |

<a id="izci-api-v1-createtelegramlinkcoderesponse"></a>

### CreateTelegramLinkCodeResponse

CreateTelegramLinkCodeResponse returns the code and deep links.

| Field | Type | Description |
|---|---|---|
| `code` | `string` | One-time code, e.g. "IZ-7F3K2Q". |
| `bot_username` | `string` | Bot username without "@". |
| `group_link` | `string` | Link that adds the bot to a group with the code prefilled. |
| `private_link` | `string` | Link that opens a private chat with the bot with the code prefilled. |
| `expires_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the code expires. |

<a id="izci-api-v1-createticketrequest"></a>

### CreateTicketRequest

CreateTicketRequest names the issue and the Jira channel.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue` | `string` | Issue id (UUID) or short id. | min length 1, max length 64 |
| `channel_id` | `string` | Jira channel id (UUID). | UUID |

<a id="izci-api-v1-createticketresponse"></a>

### CreateTicketResponse

CreateTicketResponse returns the ticket.

| Field | Type | Description |
|---|---|---|
| `ticket` | [TicketRef](#izci-api-v1-ticketref) | The ticket. |
| `created` | `bool` | False when the issue already had a ticket in this channel. |

<a id="izci-api-v1-customer"></a>

### Customer

Customer is a customer of the organization's applications.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Customer id (UUID) in izci. |
| `key` | `string` | Stable key reported by applications, e.g. "sivas-belediyesi". |
| `name` | `string` | Display name. |
| `attributes` | map&lt;`string`, `string`&gt; | Attributes reported by applications. |
| `notes` | `string` | Free-form notes kept in izci (Markdown). |
| `first_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | First event from this customer. |
| `last_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Latest event from this customer. |

<a id="izci-api-v1-customerprojectbreakdown"></a>

### CustomerProjectBreakdown

CustomerProjectBreakdown is a customer's activity in one project.

| Field | Type | Description |
|---|---|---|
| `project` | [ProjectRef](#izci-api-v1-projectref) | The project. |
| `unresolved_issues` | `uint32` | Unresolved issues of the customer in the project. |
| `period_events` | `uint64` | Events within the period. |
| `series` | [Series](#izci-api-v1-series) | Event counts over the period. |

<a id="izci-api-v1-customerref"></a>

### CustomerRef

CustomerRef is a compact reference to a customer.

| Field | Type | Description |
|---|---|---|
| `key` | `string` | Stable customer key as reported by applications (Event.customer.id), e.g. "sivas-belediyesi". |
| `name` | `string` | Display name, e.g. "Sivas Belediyesi". |

<a id="izci-api-v1-customersummary"></a>

### CustomerSummary

CustomerSummary is a customer with statistics.

| Field | Type | Description |
|---|---|---|
| `customer` | [Customer](#izci-api-v1-customer) | The customer. |
| `health` | [Health](#izci-api-v1-health) | Health over the period. |
| `unresolved_issues` | `uint32` | Unresolved issues affecting the customer. |
| `new_issues` | `uint32` | Issues first seen for this customer within the period. |
| `period_events` | `uint64` | Events within the period. |
| `affected_projects` | `uint32` | Projects with events from this customer within the period. |
| `series` | [Series](#izci-api-v1-series) | Event counts over the period. |

<a id="izci-api-v1-dailyusage"></a>

### DailyUsage

DailyUsage is the usage of one day.

| Field | Type | Description |
|---|---|---|
| `date` | `string` | Day in the organization's timezone, formatted "YYYY-MM-DD". |
| `accepted` | `uint64` | Events accepted. |
| `rate_limited` | `uint64` | Events dropped by rate limits or quota. |
| `filtered` | `uint64` | Events dropped by project filters. |

<a id="izci-api-v1-debugfile"></a>

### DebugFile

DebugFile is a debug information file or an obfuscation map.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | File id (UUID). |
| `debug_id` | `string` | Debug id the file serves (UUID): events carry it in their debug images. |
| `code_id` | `string` | Build id (ELF) or UUID (Mach-O) in hex; empty for obfuscation maps. |
| `kind` | [DebugFileKind](#izci-api-v1-debugfilekind) | What the file is. |
| `name` | `string` | File name as uploaded, e.g. "app.android-arm64.symbols". |
| `arch` | `string` | CPU architecture, e.g. "arm64"; empty for obfuscation maps. |
| `features` | repeated `string` | What the file offers: "debug" (functions, files and lines), "symtab" (symbol table), "mapping" (obfuscated names). |
| `size_bytes` | `uint64` | Size in bytes. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the file was uploaded. |
| `last_used_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the file last resolved a stack trace (updated at most hourly). |

<a id="izci-api-v1-deletealertrulerequest"></a>

### DeleteAlertRuleRequest

DeleteAlertRuleRequest identifies a rule.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Rule id. | UUID |

<a id="izci-api-v1-deletealertruleresponse"></a>

### DeleteAlertRuleResponse

DeleteAlertRuleResponse has no fields.

_No fields._

<a id="izci-api-v1-deleteartifactbundlerequest"></a>

### DeleteArtifactBundleRequest

DeleteArtifactBundleRequest names a bundle.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `id` | `string` | Bundle id (UUID). | UUID |

<a id="izci-api-v1-deleteartifactbundleresponse"></a>

### DeleteArtifactBundleResponse

DeleteArtifactBundleResponse is empty.

_No fields._

<a id="izci-api-v1-deletechannelrequest"></a>

### DeleteChannelRequest

DeleteChannelRequest identifies a channel.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Channel id. | UUID |

<a id="izci-api-v1-deletechannelresponse"></a>

### DeleteChannelResponse

DeleteChannelResponse has no fields.

_No fields._

<a id="izci-api-v1-deletedebugfilerequest"></a>

### DeleteDebugFileRequest

DeleteDebugFileRequest names a debug file.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `id` | `string` | File id (UUID). | UUID |

<a id="izci-api-v1-deletedebugfileresponse"></a>

### DeleteDebugFileResponse

DeleteDebugFileResponse is empty.

_No fields._

<a id="izci-api-v1-deleteintegrationrequest"></a>

### DeleteIntegrationRequest

DeleteIntegrationRequest identifies the integration.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Integration id. | UUID |

<a id="izci-api-v1-deleteintegrationresponse"></a>

### DeleteIntegrationResponse

DeleteIntegrationResponse has no fields.

_No fields._

<a id="izci-api-v1-deletemonitorrequest"></a>

### DeleteMonitorRequest

DeleteMonitorRequest names the monitor to delete.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Monitor id (UUID). | UUID |

<a id="izci-api-v1-deletemonitorresponse"></a>

### DeleteMonitorResponse

DeleteMonitorResponse confirms the deletion.

_No fields._

<a id="izci-api-v1-deleteprojectrequest"></a>

### DeleteProjectRequest

DeleteProjectRequest identifies the project to delete.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `slug` | `string` | Slug of the project. | min length 1, max length 50 |
| `confirm_slug` | `string` | Must repeat the slug, as a safeguard against accidental deletion. |  |

<a id="izci-api-v1-deleteprojectresponse"></a>

### DeleteProjectResponse

DeleteProjectResponse has no fields.

_No fields._

<a id="izci-api-v1-deploy"></a>

### Deploy

Deploy is a deployment of a release to an environment.

| Field | Type | Description |
|---|---|---|
| `environment` | `string` | Environment, e.g. "production". |
| `deployed_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When it was deployed. |
| `deployed_by` | `string` | Who or what deployed it. |

<a id="izci-api-v1-disabletwofactorrequest"></a>

### DisableTwoFactorRequest

DisableTwoFactorRequest proves the second factor.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `code` | `string` | Code from the authenticator app, or a recovery code. | min length 6, max length 32 |

<a id="izci-api-v1-disabletwofactorresponse"></a>

### DisableTwoFactorResponse

DisableTwoFactorResponse has no fields.

_No fields._

<a id="izci-api-v1-emailsettings"></a>

### EmailSettings

EmailSettings lists e-mail recipients.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `recipients` | repeated `string` | 1 to 20 addresses. | min 1 items, max 20 items, each max length 254, each email |

<a id="izci-api-v1-enabletwofactorrequest"></a>

### EnableTwoFactorRequest

EnableTwoFactorRequest confirms the setup.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `code` | `string` | Six-digit code the authenticator app shows for the new secret. | min length 6, max length 16 |

<a id="izci-api-v1-enabletwofactorresponse"></a>

### EnableTwoFactorResponse

EnableTwoFactorResponse returns the recovery codes.

| Field | Type | Description |
|---|---|---|
| `recovery_codes` | repeated `string` | Ten single-use recovery codes ("xxxxx-xxxxx") that sign in when the authenticator app is lost. Shown only once. |

<a id="izci-api-v1-environment"></a>

### Environment

Environment is a deployment environment seen in events.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name, e.g. "production". |
| `first_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | First event from this environment. |
| `last_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Latest event from this environment. |

<a id="izci-api-v1-errordetail"></a>

### ErrorDetail

ErrorDetail is attached to every error returned by izci.api.v1 services.

| Field | Type | Description |
|---|---|---|
| `reason` | [ErrorReason](#izci-api-v1-errorreason) | Machine-readable cause of the error. |
| `field` | `string` | Request field the error refers to, as a dotted path (e.g. "settings.timezone"), for validation errors. |
| `metadata` | map&lt;`string`, `string`&gt; | Additional values for building a user-facing message, e.g. {"limit": "10"} for ERROR_REASON_LIMIT_REACHED or {"position": "14"} for ERROR_REASON_INVALID_QUERY. |

<a id="izci-api-v1-eventattachment"></a>

### EventAttachment

EventAttachment describes a file sent with an event. The content is served
by GET {url}, with the same authentication as the API (session cookie or
Bearer personal access token).

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Attachment id (UUIDv7). |
| `name` | `string` | File name as sent by the SDK, e.g. "screenshot.png". |
| `content_type` | `string` | Media type as sent by the SDK, e.g. "image/png". |
| `attachment_type` | `string` | Sentry attachment type: "event.attachment" (default), "event.view_hierarchy", "event.minidump", "event.applecrashreport" … |
| `size_bytes` | `uint32` | Size in bytes. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When izci received it. |
| `url` | `string` | Path of the content, e.g. "/attachments/kentos/0192…"; relative to the izci base URL. |

<a id="izci-api-v1-eventdetail"></a>

### EventDetail

EventDetail is a fully normalized event.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Server-side event id (UUIDv7). |
| `event_id` | `string` | Client-side event id. |
| `issue` | [IssueRef](#izci-api-v1-issueref) | Issue the event belongs to. |
| `project` | [ProjectRef](#izci-api-v1-projectref) | Project of the event. |
| `received_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When izci received the event. |
| `event` | [izci.ingest.v1.Event](izci.ingest.v1.md#izci-ingest-v1-event) | The event after normalization, trimming and scrubbing. |
| `client_ip` | `string` | Stored client IP address (subject to project privacy settings). |
| `user_agent` | [UserAgentInfo](#izci-api-v1-useragentinfo) | User-Agent parsed by the server. |
| `warnings` | repeated `string` | Adjustments the server made during ingest (trimming, clock fixes). |
| `size_bytes` | `uint32` | Encoded size of the stored event in bytes. |
| `source_ref` | `string` | Git ref (commit SHA when known, otherwise the default branch) to use for source links, empty when the project has no linked repository. |
| `repository_url` | `string` | Web URL of the linked repository, empty when none. |
| `path_rewrites` | repeated [PathRewrite](#izci-api-v1-pathrewrite) | Path rewrites of the linked repository, to turn frame paths into repository paths. |
| `attachments` | repeated [EventAttachment](#izci-api-v1-eventattachment) | Files sent with the event (screenshots, logs, view hierarchies). |
| `feedback` | repeated [UserFeedback](#izci-api-v1-userfeedback) | What end users wrote about this event (user feedback). |

<a id="izci-api-v1-eventsummary"></a>

### EventSummary

EventSummary is the compact form of an event used in lists.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Server-side event id (UUIDv7). Use it for GetEvent. |
| `event_id` | `string` | Client-side event id (Event.event_id). |
| `issue_id` | `string` | Issue the event belongs to. |
| `received_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When izci received the event. |
| `occurred_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the event happened according to the client. |
| `level` | [izci.ingest.v1.Level](izci.ingest.v1.md#izci-ingest-v1-level) | Severity. |
| `title` | `string` | One-line description: the message, or "Type: value" of the primary exception. |
| `environment` | `string` | Environment, e.g. "production". |
| `release` | `string` | Release, e.g. "1.4.2". |
| `customer` | [CustomerRef](#izci-api-v1-customerref) | Customer, when reported. |
| `tenant` | `string` | Tenant id, when reported. |
| `user` | `string` | Best user label: email, username or id. |
| `browser` | `string` | Browser name and major version, e.g. "Chrome 129". |
| `os` | `string` | Operating system name and version, e.g. "Windows 11". |
| `url` | `string` | Request URL, when any. |
| `client_ip` | `string` | Stored client IP address (subject to project privacy settings). |
| `server_name` | `string` | Server name, e.g. "api-7d9c8f-x2kqp". |

<a id="izci-api-v1-featureflags"></a>

### FeatureFlags

FeatureFlags tells which optional features are configured.

| Field | Type | Description |
|---|---|---|
| `telegram` | `bool` | Telegram bot configured. |
| `ai` | `bool` | AI analysis enabled and opencode found. |
| `github_app` | `bool` | GitHub App configured. |
| `oidc_providers` | repeated `string` | Configured OpenID Connect provider ids. |
| `mcp` | `bool` | MCP endpoint enabled. |

<a id="izci-api-v1-filterday"></a>

### FilterDay

FilterDay is a count per day.

| Field | Type | Description |
|---|---|---|
| `day` | `string` | Day in UTC, "2026-09-27". |
| `count` | `uint64` | Events dropped that day. |

<a id="izci-api-v1-filterstat"></a>

### FilterStat

FilterStat is a count per reason.

| Field | Type | Description |
|---|---|---|
| `reason` | [FilterReason](#izci-api-v1-filterreason) | Why the events were dropped. |
| `count` | `uint64` | How many. |

<a id="izci-api-v1-findsuspectcommitrequest"></a>

### FindSuspectCommitRequest

FindSuspectCommitRequest names an issue.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Issue id. | UUID |

<a id="izci-api-v1-findsuspectcommitresponse"></a>

### FindSuspectCommitResponse

FindSuspectCommitResponse returns the result.

| Field | Type | Description |
|---|---|---|
| `suspect_commit` | [SuspectCommit](#izci-api-v1-suspectcommit) | The suspect commit, or why there is none. |

<a id="izci-api-v1-getaistatusrequest"></a>

### GetAiStatusRequest

GetAiStatusRequest has no fields.

_No fields._

<a id="izci-api-v1-getaistatusresponse"></a>

### GetAiStatusResponse

GetAiStatusResponse describes AI availability.

| Field | Type | Description |
|---|---|---|
| `enabled` | `bool` | Whether AI analysis is enabled on the instance. |
| `opencode_version` | `string` | Detected opencode version, empty when opencode is missing. |
| `default_model` | `string` | Default model. |
| `max_concurrent` | `uint32` | Maximum parallel runs. |
| `queued` | `uint32` | Runs waiting. |
| `running` | `uint32` | Runs in progress. |
| `monthly_runs` | `uint32` | Runs started this month by the organization. |
| `monthly_limit` | `uint32` | Monthly limit of the organization (0 = unlimited). |
| `allowed_commands` | repeated `string` | Shell commands projects may let the agent run (AI_ALLOWED_COMMANDS); a project entry must equal one of them or add arguments. Empty: the agent runs no commands. |
| `sandbox` | `string` | How the agent is isolated (AI_SANDBOX): "bwrap" or "none". |

<a id="izci-api-v1-getcustomerrequest"></a>

### GetCustomerRequest

GetCustomerRequest identifies a customer.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `key` | `string` | Customer key. | min length 1, max length 64 |
| `period` | [StatsPeriod](#izci-api-v1-statsperiod) | Window for statistics; defaults to 7 days. |  |

<a id="izci-api-v1-getcustomerresponse"></a>

### GetCustomerResponse

GetCustomerResponse returns the customer.

| Field | Type | Description |
|---|---|---|
| `summary` | [CustomerSummary](#izci-api-v1-customersummary) | Customer with statistics. |
| `projects` | repeated [CustomerProjectBreakdown](#izci-api-v1-customerprojectbreakdown) | Per-project breakdown, most events first. |
| `top_issues` | repeated [IssueSummary](#izci-api-v1-issuesummary) | Unresolved issues affecting the customer, most events first (max 20). |

<a id="izci-api-v1-geteventrequest"></a>

### GetEventRequest

GetEventRequest selects an event by id, by client event id, or as an
issue's latest/oldest/recommended event.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Server-side event id. Takes precedence when set. |  |
| `issue_id` | `string` | Issue whose event to pick (with pick) when id is empty. |  |
| `pick` | [EventPick](#izci-api-v1-eventpick) | Which event of issue_id to return. |  |
| `client_event_id` | `string` | Client-side event id (e.g. an "error code" shown to an end user), searched across the organization when id and issue_id are empty. | max length 64 |

<a id="izci-api-v1-geteventresponse"></a>

### GetEventResponse

GetEventResponse returns the event and its neighbours.

| Field | Type | Description |
|---|---|---|
| `event` | [EventDetail](#izci-api-v1-eventdetail) | The event. |
| `previous_id` | `string` | Id of the previous (older) event of the same issue, empty when none. |
| `next_id` | `string` | Id of the next (newer) event of the same issue, empty when none. |

<a id="izci-api-v1-getfilterstatsrequest"></a>

### GetFilterStatsRequest

GetFilterStatsRequest names a project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |

<a id="izci-api-v1-getfilterstatsresponse"></a>

### GetFilterStatsResponse

GetFilterStatsResponse counts filtered events.

| Field | Type | Description |
|---|---|---|
| `reasons` | repeated [FilterStat](#izci-api-v1-filterstat) | Events dropped in the last 30 days per reason, largest first. |
| `days` | repeated [FilterDay](#izci-api-v1-filterday) | Events dropped per day (UTC) in the last 30 days, oldest first; missing days had none. |
| `total` | `uint64` | All events dropped in the last 30 days. |

<a id="izci-api-v1-getgithubstatusrequest"></a>

### GetGitHubStatusRequest

GetGitHubStatusRequest has no fields.

_No fields._

<a id="izci-api-v1-getgithubstatusresponse"></a>

### GetGitHubStatusResponse

GetGitHubStatusResponse reports GitHub App availability.

| Field | Type | Description |
|---|---|---|
| `app_configured` | `bool` | Whether the GitHub App is configured. |
| `app_slug` | `string` | App slug, used in installation URLs. |

<a id="izci-api-v1-getingestendpointsrequest"></a>

### GetIngestEndpointsRequest

GetIngestEndpointsRequest identifies the project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |

<a id="izci-api-v1-getingestendpointsresponse"></a>

### GetIngestEndpointsResponse

GetIngestEndpointsResponse lists ingest addresses.

| Field | Type | Description |
|---|---|---|
| `base_url` | `string` | Base URL for the Connect protocol and gRPC-Web, e.g. "https://izci.example.com". |
| `grpc_target` | `string` | Target for native gRPC clients, e.g. "izci.example.com:443". |
| `grpc_tls` | `bool` | Whether native gRPC must use TLS for grpc_target. |
| `sentry_dsn_template` | `string` | Sentry DSN with a "<key>" placeholder, e.g. "https://<key>@izci.example.com/42". |
| `public_id` | `uint64` | Numeric project id used in Sentry DSNs. |

<a id="izci-api-v1-getinstanceinforequest"></a>

### GetInstanceInfoRequest

GetInstanceInfoRequest has no fields.

_No fields._

<a id="izci-api-v1-getinstanceinforesponse"></a>

### GetInstanceInfoResponse

GetInstanceInfoResponse describes the instance.

| Field | Type | Description |
|---|---|---|
| `version` | `string` | izci version. |
| `commit` | `string` | Git commit of the build. |
| `go_version` | `string` | Go version of the build. |
| `postgres_version` | `string` | PostgreSQL server version. |
| `public_url` | `string` | Configured public URL. |
| `organizations` | `uint32` | Number of organizations. |
| `users` | `uint32` | Number of users. |
| `events_24h` | `uint64` | Events received in the last 24 hours across all organizations. |
| `features` | [FeatureFlags](#izci-api-v1-featureflags) | Optional features. |
| `started_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the server process started. |

<a id="izci-api-v1-getinvitationrequest"></a>

### GetInvitationRequest

GetInvitationRequest identifies an invitation.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `token` | `string` | Invitation token from the invitation link. | min length 16, max length 128 |

<a id="izci-api-v1-getinvitationresponse"></a>

### GetInvitationResponse

GetInvitationResponse describes the invitation.

| Field | Type | Description |
|---|---|---|
| `invitation` | [Invitation](#izci-api-v1-invitation) | The invitation. |
| `account_exists` | `bool` | Whether an account with the invited email already exists (the page then asks the user to sign in instead of choosing a password). |

<a id="izci-api-v1-getissueownersrequest"></a>

### GetIssueOwnersRequest

GetIssueOwnersRequest names an issue.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Issue id. | UUID |

<a id="izci-api-v1-getissueownersresponse"></a>

### GetIssueOwnersResponse

GetIssueOwnersResponse returns owners and the suspect commit.

| Field | Type | Description |
|---|---|---|
| `owners` | repeated [IssueOwner](#izci-api-v1-issueowner) | Owners, those of the deciding rule first. |
| `unmapped_owners` | repeated `string` | Owners of matching rules that resolve to no member. |
| `deciding_rule` | `string` | The rule deciding automatic assignment, as written; empty when no rule matches. |
| `suspect_commit` | [SuspectCommit](#izci-api-v1-suspectcommit) | The suspect commit. |

<a id="izci-api-v1-getissuerequest"></a>

### GetIssueRequest

GetIssueRequest identifies an issue.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue` | `string` | Issue id (UUID) or short id (e.g. "KALEM-1A"). | min length 1, max length 64 |
| `stats_period` | [StatsPeriod](#izci-api-v1-statsperiod) | Window for the series in the summary. Defaults to 24 hours. |  |

<a id="izci-api-v1-getissueresponse"></a>

### GetIssueResponse

GetIssueResponse returns the issue.

| Field | Type | Description |
|---|---|---|
| `issue` | [Issue](#izci-api-v1-issue) | The issue. |

<a id="izci-api-v1-getissuestatsrequest"></a>

### GetIssueStatsRequest

GetIssueStatsRequest selects the window.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Issue id. | UUID |
| `period` | [StatsPeriod](#izci-api-v1-statsperiod) | Window; defaults to 24 hours. |  |
| `environment` | `string` | Restrict to one environment; empty for all. | max length 64 |

<a id="izci-api-v1-getissuestatsresponse"></a>

### GetIssueStatsResponse

GetIssueStatsResponse returns counts.

| Field | Type | Description |
|---|---|---|
| `series` | [Series](#izci-api-v1-series) | Event counts over the window. |
| `total` | `uint64` | Total events within the window. |

<a id="izci-api-v1-getissuetagfacetsrequest"></a>

### GetIssueTagFacetsRequest

GetIssueTagFacetsRequest selects facets.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Issue id. | UUID |
| `keys` | repeated `string` | Tag keys to return; empty returns the default set (customer, tenant, environment, release, browser, os, device, server_name, level) plus the most used custom tags. | max 30 items |
| `top_values` | `uint32` | Values per facet (default 10, max 50). | ≤ 50 |

<a id="izci-api-v1-getissuetagfacetsresponse"></a>

### GetIssueTagFacetsResponse

GetIssueTagFacetsResponse returns facets.

| Field | Type | Description |
|---|---|---|
| `facets` | repeated [TagFacet](#izci-api-v1-tagfacet) | Facets in the order requested (or the default order). |

<a id="izci-api-v1-getmonitorrequest"></a>

### GetMonitorRequest

GetMonitorRequest names a monitor.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Monitor id (UUID). | UUID |

<a id="izci-api-v1-getmonitorresponse"></a>

### GetMonitorResponse

GetMonitorResponse holds the monitor.

| Field | Type | Description |
|---|---|---|
| `monitor` | [Monitor](#izci-api-v1-monitor) | The monitor with its environments. |

<a id="izci-api-v1-getnotificationstatusrequest"></a>

### GetNotificationStatusRequest

GetNotificationStatusRequest has no fields.

_No fields._

<a id="izci-api-v1-getnotificationstatusresponse"></a>

### GetNotificationStatusResponse

GetNotificationStatusResponse reports transport availability.

| Field | Type | Description |
|---|---|---|
| `telegram_configured` | `bool` | Whether a Telegram bot is configured. |
| `telegram_bot_username` | `string` | Bot username without "@". |
| `telegram_mode` | `string` | Update mode of the bot: "polling" or "webhook". |
| `email_configured` | `bool` | Whether an SMTP server is configured for e-mail channels. |
| `email_from` | `string` | Sender address of notification e-mails. |

<a id="izci-api-v1-getorganizationrequest"></a>

### GetOrganizationRequest

GetOrganizationRequest has no fields; the organization comes from the
Izci-Org header.

_No fields._

<a id="izci-api-v1-getorganizationresponse"></a>

### GetOrganizationResponse

GetOrganizationResponse returns the organization.

| Field | Type | Description |
|---|---|---|
| `organization` | [Organization](#izci-api-v1-organization) | The organization. |
| `role` | [Role](#izci-api-v1-role) | The caller's role in it. |

<a id="izci-api-v1-getoverviewrequest"></a>

### GetOverviewRequest

GetOverviewRequest selects the scope.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `period` | [StatsPeriod](#izci-api-v1-statsperiod) | Window; defaults to 24 hours. |  |
| `projects` | repeated `string` | Project slugs; empty includes all. | max 50 items |
| `environments` | repeated `string` | Environments; empty includes all. | max 20 items |
| `customers` | repeated `string` | Customer keys; empty includes all. | max 50 items |

<a id="izci-api-v1-getoverviewresponse"></a>

### GetOverviewResponse

GetOverviewResponse holds the dashboard data.

| Field | Type | Description |
|---|---|---|
| `totals` | [OverviewTotals](#izci-api-v1-overviewtotals) | Headline numbers. |
| `events_by_level` | repeated [LevelSeries](#izci-api-v1-levelseries) | Event series per level, most severe first. |
| `trending_issues` | repeated [IssueSummary](#izci-api-v1-issuesummary) | Issues with the largest growth in the period (max 10). |
| `new_issues` | repeated [IssueSummary](#izci-api-v1-issuesummary) | Issues first seen in the period, newest first (max 10). |
| `projects` | repeated [ProjectHealth](#izci-api-v1-projecthealth) | Projects, most events first. |
| `customers` | repeated [CustomerSummary](#izci-api-v1-customersummary) | Most affected customers (max 10). |

<a id="izci-api-v1-getownershiprequest"></a>

### GetOwnershipRequest

GetOwnershipRequest names a project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |

<a id="izci-api-v1-getownershipresponse"></a>

### GetOwnershipResponse

GetOwnershipResponse returns the configuration.

| Field | Type | Description |
|---|---|---|
| `ownership` | [Ownership](#izci-api-v1-ownership) | The configuration. |

<a id="izci-api-v1-getprojectrequest"></a>

### GetProjectRequest

GetProjectRequest identifies a project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `slug` | `string` | Project slug. | min length 1, max length 50 |

<a id="izci-api-v1-getprojectresponse"></a>

### GetProjectResponse

GetProjectResponse returns the project.

| Field | Type | Description |
|---|---|---|
| `project` | [Project](#izci-api-v1-project) | The project. |

<a id="izci-api-v1-getraweventrequest"></a>

### GetRawEventRequest

GetRawEventRequest identifies an event.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Server-side event id. | UUID |

<a id="izci-api-v1-getraweventresponse"></a>

### GetRawEventResponse

GetRawEventResponse returns the stored JSON.

| Field | Type | Description |
|---|---|---|
| `json` | `string` | Stored event as pretty-printed JSON. |

<a id="izci-api-v1-getrunrequest"></a>

### GetRunRequest

GetRunRequest identifies a run.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Run id. | UUID |
| `include_log` | `bool` | Also return the recorded log. |  |

<a id="izci-api-v1-getrunresponse"></a>

### GetRunResponse

GetRunResponse returns the run.

| Field | Type | Description |
|---|---|---|
| `run` | [AiRun](#izci-api-v1-airun) | The run. |
| `log` | repeated [AiLogEntry](#izci-api-v1-ailogentry) | Log entries, oldest first, when requested. |

<a id="izci-api-v1-getsessionrequest"></a>

### GetSessionRequest

GetSessionRequest has no fields.

_No fields._

<a id="izci-api-v1-getsessionresponse"></a>

### GetSessionResponse

GetSessionResponse returns the current session.

| Field | Type | Description |
|---|---|---|
| `session` | [Session](#izci-api-v1-session) | Current session. |

<a id="izci-api-v1-getsetupstaterequest"></a>

### GetSetupStateRequest

GetSetupStateRequest has no fields.

_No fields._

<a id="izci-api-v1-getsetupstateresponse"></a>

### GetSetupStateResponse

GetSetupStateResponse reports whether setup is pending.

| Field | Type | Description |
|---|---|---|
| `setup_required` | `bool` | True when no user exists yet and the setup screen must be shown. |
| `version` | `string` | Server version, e.g. "v1.0.0". |
| `public_url` | `string` | Configured public base URL of the instance. |

<a id="izci-api-v1-getusagerequest"></a>

### GetUsageRequest

GetUsageRequest selects a month.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `month` | `string` | Month formatted "YYYY-MM"; defaults to the current month. | pattern `^$\|^[0-9]{4}-(0[1-9]\|1[0-2])$` |

<a id="izci-api-v1-getusageresponse"></a>

### GetUsageResponse

GetUsageResponse reports usage against limits.

| Field | Type | Description |
|---|---|---|
| `month` | `string` | Month formatted "YYYY-MM". |
| `events_accepted` | `uint64` | Events accepted this month. |
| `events_rate_limited` | `uint64` | Events dropped by rate limits or quota this month. |
| `events_filtered` | `uint64` | Events dropped by project filters this month. |
| `ai_runs` | `uint32` | AI runs started this month. |
| `limits` | [OrganizationLimits](#izci-api-v1-organizationlimits) | Plan limits. |
| `days` | repeated [DailyUsage](#izci-api-v1-dailyusage) | Per-day breakdown, oldest first. |

<a id="izci-api-v1-grouping"></a>

### Grouping

Grouping explains why events belong to this issue.

| Field | Type | Description |
|---|---|---|
| `version` | `uint32` | Algorithm version, e.g. 1. |
| `strategy` | `string` | Strategy that produced the fingerprint: "custom", "stacktrace", "exception" or "message". |
| `fingerprint` | `string` | Fingerprint as lowercase hexadecimal. |
| `components` | repeated `string` | Human-readable inputs that were hashed, in order. |

<a id="izci-api-v1-ignoreoptions"></a>

### IgnoreOptions

IgnoreOptions define when an ignored issue comes back.

| Field | Type | Description |
|---|---|---|
| `duration` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | Ignore for this long. |
| `count` | `uint64` | Ignore until this many more events arrive. |
| `until_escalating` | `bool` | Ignore until the event rate escalates. |

<a id="izci-api-v1-importcodeownersrequest"></a>

### ImportCodeownersRequest

ImportCodeownersRequest names a project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |

<a id="izci-api-v1-importcodeownersresponse"></a>

### ImportCodeownersResponse

ImportCodeownersResponse returns the configuration.

| Field | Type | Description |
|---|---|---|
| `ownership` | [Ownership](#izci-api-v1-ownership) | The configuration with the imported file. |

<a id="izci-api-v1-inapprules"></a>

### InAppRules

InAppRules classify stack frames as application code.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `include` | repeated `string` | Module or path prefixes that are application code, e.g. ["KentOS.", "github.com/mcihad/"]. | max 50 items |
| `exclude` | repeated `string` | Module or path prefixes that are never application code, e.g. ["KentOS.Shared.Vendor."]. | max 50 items |

<a id="izci-api-v1-inboundfilters"></a>

### InboundFilters

InboundFilters drop events at ingest, before rate limits and quotas: a
filtered event is answered as FILTERED and counts toward nothing but the
filter statistics.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `browser_extensions` | `bool` | Errors of browser extensions: scripts loaded from chrome-extension://, moz-extension://, safari-web-extension:// and similar, and the error messages extensions are known for. |  |
| `web_crawlers` | `bool` | Events whose User-Agent is a web crawler, a link preview or an uptime checker (Googlebot, Bingbot, Pingdom, Lighthouse, …). |  |
| `legacy_browsers` | `bool` | Events from old browsers: Internet Explorer, Edge before 79 (EdgeHTML), Safari before 12, Chrome before 80, Firefox before 78, Opera before 67, Opera Mini and the old Android browser. |  |
| `localhost` | `bool` | Events from local development: requests to localhost, 127.0.0.1 or ::1 and events whose client address is a loopback address. |  |
| `error_messages` | repeated `string` | Glob patterns matched case-insensitively against the error message ("Type: value", the value alone or the log message); "*" matches anything, e.g. "*ResizeObserver loop*". At most 50. | max 50 items, each min length 1, each max length 200 |
| `ip_addresses` | repeated `string` | IP addresses or CIDR ranges, e.g. "10.0.0.0/8", matched against the event's client (the end user) and the host that sent it. At most 50. | max 50 items, each min length 1, each max length 50 |
| `releases` | repeated `string` | Glob patterns matched case-insensitively against the release, e.g. "*-dev" or "kalem@1.2.*". At most 50. | max 50 items, each min length 1, each max length 200 |

<a id="izci-api-v1-integration"></a>

### Integration

Integration is a connected source-code host account.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Integration id (UUID). |
| `provider` | `string` | Host, currently always "github". |
| `auth` | [IntegrationAuth](#izci-api-v1-integrationauth) | How izci authenticates. |
| `name` | `string` | Descriptive name. |
| `account_login` | `string` | Account (user or organization) login, e.g. "mcihad". |
| `account_avatar_url` | `string` | Account avatar URL. |
| `installation_id` | `int64` | GitHub App installation id for INTEGRATION_AUTH_APP. |
| `healthy` | `bool` | Whether the last verification succeeded. |
| `status_message` | `string` | Last verification error, empty when healthy. |
| `created_by` | [UserRef](#izci-api-v1-userref) | Who connected it. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When it was connected. |
| `verified_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the credentials were last verified. |

<a id="izci-api-v1-invitation"></a>

### Invitation

Invitation invites an email address into an organization.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Invitation id (UUID). |
| `organization` | [OrganizationRef](#izci-api-v1-organizationref) | Organization to join. |
| `email` | `string` | Invited email address. |
| `role` | [Role](#izci-api-v1-role) | Role granted on acceptance. |
| `invited_by` | [UserRef](#izci-api-v1-userref) | Who sent the invitation. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the invitation was created. |
| `expires_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the invitation stops being valid. |
| `url` | `string` | Link to share with the invitee. Only returned to organization admins right after InviteMember. |

<a id="izci-api-v1-invitememberrequest"></a>

### InviteMemberRequest

InviteMemberRequest invites an email address.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `email` | `string` | Email address to invite. | email |
| `role` | [Role](#izci-api-v1-role) | Role granted on acceptance. | defined values only, must be set |

<a id="izci-api-v1-invitememberresponse"></a>

### InviteMemberResponse

InviteMemberResponse returns the invitation with its link.

| Field | Type | Description |
|---|---|---|
| `invitation` | [Invitation](#izci-api-v1-invitation) | The invitation; url holds the link to share. |

<a id="izci-api-v1-issue"></a>

### Issue

Issue is the full form of an issue.

| Field | Type | Description |
|---|---|---|
| `summary` | [IssueSummary](#izci-api-v1-issuesummary) | Summary fields. |
| `first_release` | `string` | Release of the first event. |
| `last_release` | `string` | Release of the latest event. |
| `resolved_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the issue was resolved, if it is. |
| `resolved_by` | [UserRef](#izci-api-v1-userref) | Who resolved it. |
| `resolved_in_release` | `string` | Release the issue is resolved in: an event from a newer release counts as a regression. Empty means any new event is a regression. |
| `snooze` | [Snooze](#izci-api-v1-snooze) | Ignore condition of an ignored issue. |
| `grouping` | [Grouping](#izci-api-v1-grouping) | How the issue was grouped. |
| `location` | [IssueLocation](#izci-api-v1-issuelocation) | Location of the top in-app frame. |
| `latest_event_id` | `string` | Id of the latest event. |
| `pull_requests` | repeated [PullRequestRef](#izci-api-v1-pullrequestref) | Branches and pull requests created for this issue. |
| `tickets` | repeated [TicketRef](#izci-api-v1-ticketref) | Tickets created for this issue in external trackers. |

<a id="izci-api-v1-issueactivity"></a>

### IssueActivity

IssueActivity is a comment or change on an issue.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Activity id (UUID). |
| `kind` | [ActivityKind](#izci-api-v1-activitykind) | Kind of activity. |
| `actor` | [UserRef](#izci-api-v1-userref) | Who did it; empty for system activity. |
| `text` | `string` | Comment text (Markdown) for comments. |
| `data` | map&lt;`string`, `string`&gt; | Details, e.g. {"from": "ISSUE_STATUS_UNRESOLVED", "to": "ISSUE_STATUS_RESOLVED"} or {"url": "https://github.com/…/pull/12"}. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When it happened. |

<a id="izci-api-v1-issuelocation"></a>

### IssueLocation

IssueLocation is where the error was raised.

| Field | Type | Description |
|---|---|---|
| `filename` | `string` | Source file, e.g. "src/Services/OrderService.cs". |
| `function` | `string` | Function, e.g. "OrderService.CreateOrder". |
| `module` | `string` | Module or namespace. |
| `lineno` | `uint32` | Line number. |

<a id="izci-api-v1-issueowner"></a>

### IssueOwner

IssueOwner is a member who owns an issue.

| Field | Type | Description |
|---|---|---|
| `user` | [UserRef](#izci-api-v1-userref) | The member. |
| `rules` | repeated `string` | Rules naming the member, e.g. "path:src/billing/*" or "codeowners:/src/billing/"; the deciding rule first. |
| `suggested` | `bool` | Whether automatic assignment by rules picks this member. |

<a id="izci-api-v1-issueref"></a>

### IssueRef

IssueRef is a compact reference to an issue.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Issue id (UUID). |
| `short_id` | `string` | Short id, e.g. "KALEM-1A". |
| `title` | `string` | Issue title. |

<a id="izci-api-v1-issuesummary"></a>

### IssueSummary

IssueSummary is the compact form of an issue used in lists.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Issue id (UUID). |
| `short_id` | `string` | Short id, e.g. "KALEM-1A". |
| `project` | [ProjectRef](#izci-api-v1-projectref) | Project of the issue. |
| `title` | `string` | Title: "Type: value" of the primary exception, or the message. |
| `culprit` | `string` | Where the error happened: the top in-app frame ("OrderService.Create in OrderService.cs") or the transaction. |
| `type` | `string` | Exception type, e.g. "System.NullReferenceException"; empty for message issues. |
| `value` | `string` | Exception message or message text (first line, truncated). |
| `level` | [izci.ingest.v1.Level](izci.ingest.v1.md#izci-ingest-v1-level) | Highest level seen. |
| `platform` | `string` | Platform of the events. |
| `status` | [IssueStatus](#izci-api-v1-issuestatus) | Triage status. |
| `substatus` | [IssueSubstatus](#izci-api-v1-issuesubstatus) | Substatus of unresolved issues. |
| `priority` | [Priority](#izci-api-v1-priority) | Priority. |
| `first_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | First event. |
| `last_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Latest event. |
| `times_seen` | `uint64` | Total number of events ("how many times it repeated"). |
| `users_count` | `uint32` | Distinct users affected. |
| `customers_count` | `uint32` | Distinct customers affected. |
| `period_count` | `uint64` | Events within the requested stats period. |
| `series` | [Series](#izci-api-v1-series) | Event counts over the requested stats period (sparkline). |
| `assignee` | [UserRef](#izci-api-v1-userref) | Assigned member, when any. |
| `unhandled` | `bool` | True when at least one event was unhandled (crashed). |
| `ai_state` | [AiState](#izci-api-v1-aistate) | State of AI analysis for this issue. |
| `permalink` | `string` | Absolute link to the issue in the web UI. |

<a id="izci-api-v1-jirainput"></a>

### JiraInput

JiraInput configures a Jira channel.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `base_url` | `string` | Site address, e.g. "https://kentos.atlassian.net". | min length 1, max length 300 |
| `project_key` | `string` | Project key, e.g. "KALEM". | min length 1, max length 50 |
| `issue_type` | `string` | Issue type name, e.g. "Bug" (default). | max length 100 |
| `email` | `string` | Account e-mail for a Jira Cloud API token; empty for a personal access token (Jira Server, Data Center). | max length 254 |
| `token` | `string` | API token or personal access token. On update, empty keeps the current token. | max length 1000 |
| `labels` | repeated `string` | Extra labels, at most 10. | max 10 items, each min length 1, each max length 50 |

<a id="izci-api-v1-jirasettings"></a>

### JiraSettings

JiraSettings is where a Jira channel creates tickets.

| Field | Type | Description |
|---|---|---|
| `base_url` | `string` | Site address, e.g. "https://kentos.atlassian.net". |
| `project_key` | `string` | Project key, e.g. "KALEM". |
| `issue_type` | `string` | Issue type name as configured in Jira, e.g. "Bug". |
| `email` | `string` | Account e-mail for Jira Cloud API tokens; empty when the token is a personal access token (Jira Server, Data Center). |
| `labels` | repeated `string` | Labels added to tickets besides "izci". |

<a id="izci-api-v1-levelseries"></a>

### LevelSeries

LevelSeries is the event series of one level.

| Field | Type | Description |
|---|---|---|
| `level` | [izci.ingest.v1.Level](izci.ingest.v1.md#izci-ingest-v1-level) | The level. |
| `series` | [Series](#izci-api-v1-series) | Event counts. |

<a id="izci-api-v1-linkrepositoryrequest"></a>

### LinkRepositoryRequest

LinkRepositoryRequest links a repository.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `integration_id` | `string` | Integration that grants access; empty for a public link without AI features. |  |
| `repository` | `string` | Repository in "owner/name" form or as a URL. | min length 3, max length 300 |
| `default_branch` | `string` | Default branch; detected from the provider when empty. | max length 200 |
| `path_mappings` | repeated [PathMapping](#izci-api-v1-pathmapping) | Path rewrites for stack frames. | max 20 items |

<a id="izci-api-v1-linkrepositoryresponse"></a>

### LinkRepositoryResponse

LinkRepositoryResponse returns the link.

| Field | Type | Description |
|---|---|---|
| `repository` | [ProjectRepository](#izci-api-v1-projectrepository) | The linked repository. |

<a id="izci-api-v1-listaccesstokensrequest"></a>

### ListAccessTokensRequest

ListAccessTokensRequest has no fields.

_No fields._

<a id="izci-api-v1-listaccesstokensresponse"></a>

### ListAccessTokensResponse

ListAccessTokensResponse lists tokens.

| Field | Type | Description |
|---|---|---|
| `tokens` | repeated [AccessToken](#izci-api-v1-accesstoken) | Tokens, newest first. |

<a id="izci-api-v1-listalertrulesrequest"></a>

### ListAlertRulesRequest

ListAlertRulesRequest filters rules.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug; empty lists all rules. | max length 50 |

<a id="izci-api-v1-listalertrulesresponse"></a>

### ListAlertRulesResponse

ListAlertRulesResponse lists rules.

| Field | Type | Description |
|---|---|---|
| `rules` | repeated [AlertRule](#izci-api-v1-alertrule) | Rules ordered by name. |

<a id="izci-api-v1-listapikeysrequest"></a>

### ListApiKeysRequest

ListApiKeysRequest identifies the project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `include_revoked` | `bool` | Also return revoked keys. |  |

<a id="izci-api-v1-listapikeysresponse"></a>

### ListApiKeysResponse

ListApiKeysResponse lists API keys.

| Field | Type | Description |
|---|---|---|
| `keys` | repeated [ApiKey](#izci-api-v1-apikey) | Keys, newest first. |

<a id="izci-api-v1-listartifactbundlesrequest"></a>

### ListArtifactBundlesRequest

ListArtifactBundlesRequest names a project and a page.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `page_size` | `uint32` | Page size, 1-100 (default 20). | ≤ 100 |
| `page_token` | `string` | Token from a previous response for the next page. | max length 100 |

<a id="izci-api-v1-listartifactbundlesresponse"></a>

### ListArtifactBundlesResponse

ListArtifactBundlesResponse is a page of bundles.

| Field | Type | Description |
|---|---|---|
| `bundles` | repeated [ArtifactBundle](#izci-api-v1-artifactbundle) | Bundles, newest first. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listauditlogrequest"></a>

### ListAuditLogRequest

ListAuditLogRequest pages through the audit log.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `page_size` | `uint32` | Maximum entries to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listauditlogresponse"></a>

### ListAuditLogResponse

ListAuditLogResponse is a page of audit log entries.

| Field | Type | Description |
|---|---|---|
| `entries` | repeated [AuditLogEntry](#izci-api-v1-auditlogentry) | Entries, newest first. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listauthprovidersrequest"></a>

### ListAuthProvidersRequest

ListAuthProvidersRequest has no fields.

_No fields._

<a id="izci-api-v1-listauthprovidersresponse"></a>

### ListAuthProvidersResponse

ListAuthProvidersResponse lists sign-in options.

| Field | Type | Description |
|---|---|---|
| `providers` | repeated [AuthProvider](#izci-api-v1-authprovider) | Configured OpenID Connect providers. |
| `password_login_enabled` | `bool` | Whether email + password sign-in is enabled. |
| `signup_mode` | [SignupMode](#izci-api-v1-signupmode) | Who may create new accounts. |

<a id="izci-api-v1-listchannelsrequest"></a>

### ListChannelsRequest

ListChannelsRequest has no fields.

_No fields._

<a id="izci-api-v1-listchannelsresponse"></a>

### ListChannelsResponse

ListChannelsResponse lists channels.

| Field | Type | Description |
|---|---|---|
| `channels` | repeated [Channel](#izci-api-v1-channel) | Channels ordered by name. |

<a id="izci-api-v1-listcheckinsrequest"></a>

### ListCheckInsRequest

ListCheckInsRequest pages the runs of a monitor.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `monitor_id` | `string` | Monitor id (UUID). | UUID |
| `environment` | `string` | Environment to show; empty for all. | max length 64 |
| `page_size` | `uint32` | Maximum runs to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listcheckinsresponse"></a>

### ListCheckInsResponse

ListCheckInsResponse is a page of runs.

| Field | Type | Description |
|---|---|---|
| `check_ins` | repeated [CheckIn](#izci-api-v1-checkin) | Runs, newest first. |
| `next_page_token` | `string` | Token for the next page, empty on the last one. |

<a id="izci-api-v1-listcustomersrequest"></a>

### ListCustomersRequest

ListCustomersRequest filters and sorts customers.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `query` | `string` | Text matched against key and name. | max length 200 |
| `period` | [StatsPeriod](#izci-api-v1-statsperiod) | Window for statistics; defaults to 24 hours. |  |
| `sort` | [CustomerSort](#izci-api-v1-customersort) | Sort order. |  |
| `page_size` | `uint32` | Maximum customers to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listcustomersresponse"></a>

### ListCustomersResponse

ListCustomersResponse is a page of customers.

| Field | Type | Description |
|---|---|---|
| `customers` | repeated [CustomerSummary](#izci-api-v1-customersummary) | Customers in the requested order. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listdebugfilesrequest"></a>

### ListDebugFilesRequest

ListDebugFilesRequest names a project and a page.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `page_size` | `uint32` | Page size, 1-100 (default 20). | ≤ 100 |
| `page_token` | `string` | Token from a previous response for the next page. | max length 100 |

<a id="izci-api-v1-listdebugfilesresponse"></a>

### ListDebugFilesResponse

ListDebugFilesResponse is a page of debug files.

| Field | Type | Description |
|---|---|---|
| `files` | repeated [DebugFile](#izci-api-v1-debugfile) | Files, newest first. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listenvironmentsrequest"></a>

### ListEnvironmentsRequest

ListEnvironmentsRequest identifies the project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug; empty lists environments of all projects. | max length 50 |

<a id="izci-api-v1-listenvironmentsresponse"></a>

### ListEnvironmentsResponse

ListEnvironmentsResponse lists environments.

| Field | Type | Description |
|---|---|---|
| `environments` | repeated [Environment](#izci-api-v1-environment) | Environments ordered by name. |

<a id="izci-api-v1-listfeedbackrequest"></a>

### ListFeedbackRequest

ListFeedbackRequest filters and pages feedback.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug; empty for every project of the organization. | max length 50 |
| `issue_id` | `string` | Issue id (UUID); empty for all feedback. | max length 36 |
| `query` | `string` | Case-insensitive text to find in the message, name or email. | max length 200 |
| `page_size` | `uint32` | Maximum items to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listfeedbackresponse"></a>

### ListFeedbackResponse

ListFeedbackResponse is a page of feedback.

| Field | Type | Description |
|---|---|---|
| `feedback` | repeated [UserFeedback](#izci-api-v1-userfeedback) | Feedback, newest first. |
| `next_page_token` | `string` | Token for the next page, empty on the last one. |

<a id="izci-api-v1-listintegrationsrequest"></a>

### ListIntegrationsRequest

ListIntegrationsRequest has no fields.

_No fields._

<a id="izci-api-v1-listintegrationsresponse"></a>

### ListIntegrationsResponse

ListIntegrationsResponse lists integrations.

| Field | Type | Description |
|---|---|---|
| `integrations` | repeated [Integration](#izci-api-v1-integration) | Integrations, newest first. |

<a id="izci-api-v1-listissueactivityrequest"></a>

### ListIssueActivityRequest

ListIssueActivityRequest pages through activity.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Issue id. | UUID |
| `page_size` | `uint32` | Maximum entries to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listissueactivityresponse"></a>

### ListIssueActivityResponse

ListIssueActivityResponse is a page of activity.

| Field | Type | Description |
|---|---|---|
| `activities` | repeated [IssueActivity](#izci-api-v1-issueactivity) | Activity, newest first. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listissueeventsrequest"></a>

### ListIssueEventsRequest

ListIssueEventsRequest pages through an issue's events.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Issue id. | UUID |
| `query` | `string` | Optional filter in the search language, restricted to event-level keys (environment, release, customer, tenant, user, browser, os, server). | max length 2000 |
| `page_size` | `uint32` | Maximum events to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listissueeventsresponse"></a>

### ListIssueEventsResponse

ListIssueEventsResponse is a page of events.

| Field | Type | Description |
|---|---|---|
| `events` | repeated [EventSummary](#izci-api-v1-eventsummary) | Events, newest first. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listissuesrequest"></a>

### ListIssuesRequest

ListIssuesRequest searches issues.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `query` | `string` | Search query in the izci search language. Defaults to "is:unresolved". | max length 2000 |
| `sort` | [IssueSort](#izci-api-v1-issuesort) | Sort order. |  |
| `stats_period` | [StatsPeriod](#izci-api-v1-statsperiod) | Window for period_count and series; also limits results to issues seen within it. Defaults to 24 hours. |  |
| `projects` | repeated `string` | Project slugs to include; empty includes all projects. | max 50 items |
| `environments` | repeated `string` | Environments to include; empty includes all. | max 20 items |
| `customers` | repeated `string` | Customer keys to include; empty includes all. | max 50 items |
| `page_size` | `uint32` | Maximum issues to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listissuesresponse"></a>

### ListIssuesResponse

ListIssuesResponse is a page of issues.

| Field | Type | Description |
|---|---|---|
| `issues` | repeated [IssueSummary](#izci-api-v1-issuesummary) | Matching issues in the requested order. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |
| `total_estimate` | `uint32` | Estimated total number of matching issues (capped at 10000). |

<a id="izci-api-v1-listmembersrequest"></a>

### ListMembersRequest

ListMembersRequest has no fields.

_No fields._

<a id="izci-api-v1-listmembersresponse"></a>

### ListMembersResponse

ListMembersResponse lists members and invitations.

| Field | Type | Description |
|---|---|---|
| `members` | repeated [Member](#izci-api-v1-member) | Members ordered by name. |
| `invitations` | repeated [Invitation](#izci-api-v1-invitation) | Pending invitations, newest first (only for ROLE_ADMIN and above). |

<a id="izci-api-v1-listmonitorsrequest"></a>

### ListMonitorsRequest

ListMonitorsRequest filters monitors.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug; empty for every project. | max length 50 |
| `query` | `string` | Case-insensitive text to find in the name or slug. | max length 100 |

<a id="izci-api-v1-listmonitorsresponse"></a>

### ListMonitorsResponse

ListMonitorsResponse lists monitors ordered by name.

| Field | Type | Description |
|---|---|---|
| `monitors` | repeated [Monitor](#izci-api-v1-monitor) | Monitors with their environments and recent runs. |

<a id="izci-api-v1-listorganizationsrequest"></a>

### ListOrganizationsRequest

ListOrganizationsRequest pages through organizations.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `query` | `string` | Text matched against slug and name. | max length 100 |
| `page_size` | `uint32` | Maximum organizations to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listorganizationsresponse"></a>

### ListOrganizationsResponse

ListOrganizationsResponse is a page of organizations.

| Field | Type | Description |
|---|---|---|
| `organizations` | repeated [AdminOrganization](#izci-api-v1-adminorganization) | Organizations ordered by name. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listowneraliasesrequest"></a>

### ListOwnerAliasesRequest

ListOwnerAliasesRequest has no fields.

_No fields._

<a id="izci-api-v1-listowneraliasesresponse"></a>

### ListOwnerAliasesResponse

ListOwnerAliasesResponse lists aliases.

| Field | Type | Description |
|---|---|---|
| `aliases` | repeated [OwnerAlias](#izci-api-v1-owneralias) | Aliases by name. |

<a id="izci-api-v1-listprojectsrequest"></a>

### ListProjectsRequest

ListProjectsRequest has no fields.

_No fields._

<a id="izci-api-v1-listprojectsresponse"></a>

### ListProjectsResponse

ListProjectsResponse lists projects.

| Field | Type | Description |
|---|---|---|
| `projects` | repeated [Project](#izci-api-v1-project) | Projects ordered by name. |

<a id="izci-api-v1-listreleasesrequest"></a>

### ListReleasesRequest

ListReleasesRequest pages through releases.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `page_size` | `uint32` | Maximum releases to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |
| `health_period` | [StatsPeriod](#izci-api-v1-statsperiod) | Window of Release.health (default: last 14 days). |  |

<a id="izci-api-v1-listreleasesresponse"></a>

### ListReleasesResponse

ListReleasesResponse is a page of releases.

| Field | Type | Description |
|---|---|---|
| `releases` | repeated [Release](#izci-api-v1-release) | Releases, newest first. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listrepositoriesrequest"></a>

### ListRepositoriesRequest

ListRepositoriesRequest selects the integration.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `integration_id` | `string` | Integration id. | UUID |
| `query` | `string` | Text matched against repository names. | max length 100 |

<a id="izci-api-v1-listrepositoriesresponse"></a>

### ListRepositoriesResponse

ListRepositoriesResponse lists repositories.

| Field | Type | Description |
|---|---|---|
| `repositories` | repeated [Repository](#izci-api-v1-repository) | Repositories ordered by name (max 100). |

<a id="izci-api-v1-listrunsrequest"></a>

### ListRunsRequest

ListRunsRequest filters runs.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Only runs of this issue. |  |
| `project` | `string` | Only runs of this project (slug). | max length 50 |
| `status` | [AiRunStatus](#izci-api-v1-airunstatus) | Only runs with this status. |  |
| `page_size` | `uint32` | Maximum runs to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listrunsresponse"></a>

### ListRunsResponse

ListRunsResponse is a page of runs (without diff and full analysis).

| Field | Type | Description |
|---|---|---|
| `runs` | repeated [AiRun](#izci-api-v1-airun) | Runs, newest first. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-listsessionsrequest"></a>

### ListSessionsRequest

ListSessionsRequest has no fields.

_No fields._

<a id="izci-api-v1-listsessionsresponse"></a>

### ListSessionsResponse

ListSessionsResponse lists sessions.

| Field | Type | Description |
|---|---|---|
| `sessions` | repeated [SessionInfo](#izci-api-v1-sessioninfo) | Sessions, most recently used first. |

<a id="izci-api-v1-listusersrequest"></a>

### ListUsersRequest

ListUsersRequest pages through users.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `query` | `string` | Text matched against email and name. | max length 100 |
| `page_size` | `uint32` | Maximum users to return (default 50, max 100). | ≤ 100 |
| `page_token` | `string` | Token from a previous response. |  |

<a id="izci-api-v1-listusersresponse"></a>

### ListUsersResponse

ListUsersResponse is a page of users.

| Field | Type | Description |
|---|---|---|
| `users` | repeated [AdminUser](#izci-api-v1-adminuser) | Users ordered by email. |
| `next_page_token` | `string` | Token for the next page; empty on the last page. |

<a id="izci-api-v1-loginrequest"></a>

### LoginRequest

LoginRequest holds credentials.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `email` | `string` | Email address. | min length 3, max length 320 |
| `password` | `string` | Password. | min length 1, max length 200 |

<a id="izci-api-v1-loginresponse"></a>

### LoginResponse

LoginResponse returns the new session, or asks for the second step.

| Field | Type | Description |
|---|---|---|
| `session` | [Session](#izci-api-v1-session) | Session of the signed-in user; unset when two_factor_required. |
| `two_factor_required` | `bool` | The account uses two-factor authentication: send a code with VerifyTwoFactor to finish signing in. |

<a id="izci-api-v1-logoutrequest"></a>

### LogoutRequest

LogoutRequest has no fields.

_No fields._

<a id="izci-api-v1-logoutresponse"></a>

### LogoutResponse

LogoutResponse has no fields.

_No fields._

<a id="izci-api-v1-member"></a>

### Member

Member is a user's membership in the organization.

| Field | Type | Description |
|---|---|---|
| `user` | [UserRef](#izci-api-v1-userref) | The user. |
| `role` | [Role](#izci-api-v1-role) | Role in the organization. |
| `joined_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the user joined. |
| `last_seen_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the user was last active in izci. |
| `two_factor_enabled` | `bool` | Whether the user signs in with two-factor authentication. |

<a id="izci-api-v1-membership"></a>

### Membership

Membership is a user's role in one organization.

| Field | Type | Description |
|---|---|---|
| `organization` | [OrganizationRef](#izci-api-v1-organizationref) | The organization. |
| `role` | [Role](#izci-api-v1-role) | The user's role in it. |
| `requires_two_factor` | `bool` | The organization requires two-factor authentication (OrganizationSettings.require_two_factor). While the user has not turned it on, calls in the organization fail with ERROR_REASON_TWO_FACTOR_REQUIRED. |

<a id="izci-api-v1-monitor"></a>

### Monitor

Monitor is a scheduled job izci watches.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Monitor id (UUID). |
| `project` | [ProjectRef](#izci-api-v1-projectref) | Project the monitor belongs to. |
| `slug` | `string` | Identifier jobs use in check-ins (monitor_slug), unique in the project, e.g. "daily-billing". |
| `name` | `string` | Display name, e.g. "Günlük tahsilat". |
| `schedule` | [MonitorSchedule](#izci-api-v1-monitorschedule) | When runs are due. |
| `timezone` | `string` | IANA time zone the crontab is read in, e.g. "Europe/Istanbul". |
| `checkin_margin_minutes` | `uint32` | Minutes a run may check in late before it counts as missed. |
| `max_runtime_minutes` | `uint32` | Minutes a run may stay in progress before it counts as timed out. |
| `failure_threshold` | `uint32` | Failed runs in a row that open an issue. |
| `recovery_threshold` | `uint32` | Successful runs in a row that resolve the issue again. |
| `paused` | `bool` | A paused monitor records runs but raises no issues and never counts a run as missed. |
| `environments` | repeated [MonitorEnvironment](#izci-api-v1-monitorenvironment) | State per environment the job reported from, ordered by name. |
| `recent_check_ins` | repeated [CheckIn](#izci-api-v1-checkin) | Latest runs across environments, newest first (at most 30); filled by ListMonitors. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the monitor was created. |
| `updated_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the monitor was last changed. |

<a id="izci-api-v1-monitorenvironment"></a>

### MonitorEnvironment

MonitorEnvironment is the state of a monitor in one environment.

| Field | Type | Description |
|---|---|---|
| `environment` | `string` | Environment name, e.g. "production". |
| `status` | [MonitorStatus](#izci-api-v1-monitorstatus) | Outcome of the last finished run. |
| `last_check_in_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the last run started (or finished, for single check-ins). |
| `next_check_in_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the next run is expected; unset before the first run. |
| `next_check_in_latest` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | The latest the next run may check in before it counts as missed. |
| `failures` | `uint32` | Failed runs in a row. |
| `issue` | [IssueRef](#izci-api-v1-issueref) | The open issue raised for this environment, if any. |

<a id="izci-api-v1-monitorinterval"></a>

### MonitorInterval

MonitorInterval is an interval schedule, e.g. every 2 hours.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `value` | `uint32` | Number of units between runs. | ≤ 100000 |
| `unit` | [MonitorIntervalUnit](#izci-api-v1-monitorintervalunit) | Unit of the interval. | defined values only, must be set |

<a id="izci-api-v1-monitorschedule"></a>

### MonitorSchedule

MonitorSchedule says when runs are due.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `crontab` | `string` | _oneof `kind`._ Crontab expression in five fields or a descriptor, e.g. "0 3 * * *" (03:00 every day) or "@hourly". | min length 1, max length 100 |
| `interval` | [MonitorInterval](#izci-api-v1-monitorinterval) | _oneof `kind`._ A run every interval, counted from the previous run. |  |

<a id="izci-api-v1-organization"></a>

### Organization

Organization is a tenant of izci.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Organization id (UUID). |
| `slug` | `string` | URL-safe unique identifier, e.g. "kentos". |
| `name` | `string` | Display name. |
| `plan` | `string` | Plan name, e.g. "internal", "free", "team". |
| `settings` | [OrganizationSettings](#izci-api-v1-organizationsettings) | Editable settings. |
| `limits` | [OrganizationLimits](#izci-api-v1-organizationlimits) | Limits of the plan (read-only; changed by instance administrators). |
| `project_count` | `uint32` | Number of projects. |
| `member_count` | `uint32` | Number of members. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the organization was created. |

<a id="izci-api-v1-organizationlimits"></a>

### OrganizationLimits

OrganizationLimits are the plan limits of an organization. Zero means
unlimited.

| Field | Type | Description |
|---|---|---|
| `monthly_events` | `uint64` | Events accepted per calendar month. |
| `retention_days` | `uint32` | Days events are kept. |
| `max_projects` | `uint32` | Maximum number of projects. |
| `max_members` | `uint32` | Maximum number of members. |
| `monthly_ai_runs` | `uint32` | AI runs per calendar month. |

<a id="izci-api-v1-organizationref"></a>

### OrganizationRef

OrganizationRef is a compact reference to an organization.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Organization id (UUID). |
| `slug` | `string` | URL-safe unique identifier, e.g. "kentos". |
| `name` | `string` | Display name, e.g. "KentOS". |

<a id="izci-api-v1-organizationsettings"></a>

### OrganizationSettings

OrganizationSettings are editable organization settings.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `timezone` | `string` | IANA timezone used for notifications, digests and day boundaries in charts, e.g. "Europe/Istanbul". | max length 64 |
| `auto_join_domains` | repeated `string` | Email domains whose users join the organization automatically when they first sign in with OpenID Connect, e.g. ["kentos.com.tr"]. | max 20 items, each hostname |
| `auto_join_role` | [Role](#izci-api-v1-role) | Role granted to users who join through auto_join_domains. |  |
| `require_two_factor` | `bool` | Members must turn on two-factor authentication. Until they do, every call in the organization (sessions and access tokens alike) fails with ERROR_REASON_TWO_FACTOR_REQUIRED. Only a caller who uses two-factor authentication may turn this on. |  |

<a id="izci-api-v1-overviewtotals"></a>

### OverviewTotals

OverviewTotals are the headline numbers of a period.

| Field | Type | Description |
|---|---|---|
| `events` | `uint64` | Events in the period. |
| `previous_events` | `uint64` | Events in the preceding period of equal length (for the trend). |
| `events_per_minute` | `double` | Average events per minute over the last 15 minutes. |
| `new_issues` | `uint32` | Issues first seen in the period. |
| `regressed_issues` | `uint32` | Issues that regressed in the period. |
| `unresolved_issues` | `uint32` | Currently unresolved issues. |
| `affected_users` | `uint32` | Distinct users affected in the period. |
| `affected_customers` | `uint32` | Distinct customers affected in the period. |

<a id="izci-api-v1-owneralias"></a>

### OwnerAlias

OwnerAlias maps an owner name used in rules to members.

| Field | Type | Description |
|---|---|---|
| `alias` | `string` | "@login" (a GitHub user), "@org/team" or "#team", lowercased. |
| `users` | repeated [UserRef](#izci-api-v1-userref) | Members the alias stands for, in order; the first is assigned. |
| `updated_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the mapping last changed. |

<a id="izci-api-v1-ownership"></a>

### Ownership

Ownership is a project's ownership configuration.

Rule syntax, one rule per line ("#" starts a comment line):

  path:src/billing/*        ayse@kentos.com.tr @mehmet
  module:Kentos.Tahsilat.*  #tahsilat
  url:*/odeme/*             ayse@kentos.com.tr
  tags.customer:sivas       ali@kentos.com.tr
  codeowners:/src/billing/  @kentos/billing

"path" matches file paths of stack frames (relative patterns at any
directory), "module" their modules, "url" the request URL, "tags.<key>"
an event tag (also level, environment, release, transaction,
server_name, logger, customer and tenant); "*" matches anything, case is
ignored. "codeowners" uses CODEOWNERS patterns on repository paths.

| Field | Type | Description |
|---|---|---|
| `rules` | `string` | The project's own rules. |
| `codeowners` | [Codeowners](#izci-api-v1-codeowners) | The CODEOWNERS file; unset when none. |
| `auto_assign` | [AutoAssign](#izci-api-v1-autoassign) | How new issues are assigned. |
| `unmapped_owners` | repeated `string` | Owners that resolve to no member: e-mail addresses of non-members and aliases without a mapping, lowercased. |
| `problems` | repeated [RuleProblem](#izci-api-v1-ruleproblem) | Invalid lines of the stored CODEOWNERS text (and rules saved before a validation change). |
| `rule_count` | `uint32` | Rules in effect from the project's own rules. |
| `github_linked` | `bool` | Whether a repository is linked through a GitHub connection, which importing CODEOWNERS and finding suspect commits need. |
| `updated_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the configuration last changed; unset when never saved. |

<a id="izci-api-v1-pathmapping"></a>

### PathMapping

PathMapping rewrites a stack frame path prefix to a repository path
prefix, e.g. "/app/" → "src/".

| Field | Type | Description | Constraints |
|---|---|---|---|
| `from_prefix` | `string` | Prefix of Frame.abs_path or Frame.filename as reported by the application, e.g. "/app/" or "C:\\build\\src\\". | min length 1, max length 512 |
| `to_prefix` | `string` | Replacement prefix relative to the repository root, e.g. "src/"; may be empty. | max length 512 |

<a id="izci-api-v1-pathrewrite"></a>

### PathRewrite

PathRewrite maps a frame path prefix to a repository path prefix.

| Field | Type | Description |
|---|---|---|
| `from_prefix` | `string` | Prefix as reported by the application. |
| `to_prefix` | `string` | Replacement prefix relative to the repository root. |

<a id="izci-api-v1-privacysettings"></a>

### PrivacySettings

PrivacySettings control what personal data is stored.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `ip_storage` | [IpStorage](#izci-api-v1-ipstorage) | How end-user IP addresses are stored. |  |
| `scrub_fields` | repeated `string` | Additional field names (case-insensitive substrings) to scrub everywhere in events, on top of the built-in list. | max 100 items |
| `safe_fields` | repeated `string` | Field names that must never be scrubbed even if they match a pattern. | max 100 items |
| `store_cookie_values` | `bool` | Whether cookie values are stored (names are always stored). |  |
| `store_request_bodies` | `bool` | Whether request bodies are stored. |  |
| `discard_attachments` | `bool` | Files sent with events (screenshots, log files, view hierarchies) are dropped instead of stored. Off by default: SDKs send attachments only when the application turns them on. |  |

<a id="izci-api-v1-project"></a>

### Project

Project is one monitored application.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Project id (UUID). |
| `slug` | `string` | URL-safe identifier unique within the organization, e.g. "kalem". |
| `name` | `string` | Display name, e.g. "KentOS Kalem". |
| `platform` | `string` | Main platform, e.g. "dotnet", "go", "javascript", "dart". |
| `public_id` | `uint64` | Numeric id used in Sentry DSNs ("https://<key>@host/<public_id>"). |
| `settings` | [ProjectSettings](#izci-api-v1-projectsettings) | Settings. |
| `repository` | [ProjectRepository](#izci-api-v1-projectrepository) | Linked source repository, when any. |
| `stats` | [ProjectStats](#izci-api-v1-projectstats) | Short statistics for lists. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the project was created. |

<a id="izci-api-v1-projecthealth"></a>

### ProjectHealth

ProjectHealth summarizes a project in the period.

| Field | Type | Description |
|---|---|---|
| `project` | [ProjectRef](#izci-api-v1-projectref) | The project. |
| `events` | `uint64` | Events in the period. |
| `unresolved_issues` | `uint32` | Unresolved issues. |
| `new_issues` | `uint32` | Issues first seen in the period. |
| `series` | [Series](#izci-api-v1-series) | Event counts over the period. |

<a id="izci-api-v1-projectref"></a>

### ProjectRef

ProjectRef is a compact reference to a project.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Project id (UUID). |
| `slug` | `string` | URL-safe identifier unique within the organization, e.g. "kalem". |
| `name` | `string` | Display name, e.g. "KentOS Kalem". |
| `platform` | `string` | Main platform of the project, e.g. "dotnet", "go", "javascript". |

<a id="izci-api-v1-projectrepository"></a>

### ProjectRepository

ProjectRepository links a project to a source repository.

| Field | Type | Description |
|---|---|---|
| `integration_id` | `string` | Integration used to access the repository; empty for public links without an integration (source links only). |
| `provider` | `string` | Hosting provider, e.g. "github". |
| `owner` | `string` | Repository owner, e.g. "mcihad". |
| `name` | `string` | Repository name, e.g. "kentos-kalem". |
| `url` | `string` | Web URL, e.g. "https://github.com/mcihad/kentos-kalem". |
| `default_branch` | `string` | Default branch, e.g. "main". |
| `path_mappings` | repeated [PathMapping](#izci-api-v1-pathmapping) | Rewrites applied to stack frame paths to find files in the repository, applied in order; the first matching prefix wins. |
| `linked_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the repository was linked. |

<a id="izci-api-v1-projectsettings"></a>

### ProjectSettings

ProjectSettings are the editable settings of a project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `privacy` | [PrivacySettings](#izci-api-v1-privacysettings) | Privacy and scrubbing rules. |  |
| `in_app` | [InAppRules](#izci-api-v1-inapprules) | Rules that decide which stack frames are application code. |  |
| `rate_limit_per_minute` | `uint32` | Maximum events per minute accepted for the project (0 = instance default). |  |
| `ignored_environments` | repeated `string` | Environments whose events are dropped at ingest, e.g. ["development"]. | max 20 items |
| `ai` | [AiSettings](#izci-api-v1-aisettings) | AI analysis automation. |  |
| `filters` | [InboundFilters](#izci-api-v1-inboundfilters) | Inbound filters that drop unwanted events before they are stored. |  |

<a id="izci-api-v1-projectstats"></a>

### ProjectStats

ProjectStats summarizes recent activity.

| Field | Type | Description |
|---|---|---|
| `unresolved_issues` | `uint32` | Number of unresolved issues. |
| `events_24h` | `uint64` | Events received in the last 24 hours. |
| `series_24h` | [Series](#izci-api-v1-series) | Hourly event counts of the last 24 hours. |
| `last_event_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the last event was received. |

<a id="izci-api-v1-pullrequestref"></a>

### PullRequestRef

PullRequestRef is a branch or pull request created for an issue.

| Field | Type | Description |
|---|---|---|
| `number` | `uint32` | Pull request number; 0 when only a branch exists. |
| `url` | `string` | Web URL of the pull request (or the branch). |
| `title` | `string` | Title. |
| `branch` | `string` | Branch name. |
| `draft` | `bool` | Whether the pull request is a draft. |
| `state` | `string` | State: "open", "closed", "merged" or "branch". |
| `ai_run_id` | `string` | AI run that produced it. |

<a id="izci-api-v1-regeneraterecoverycodesrequest"></a>

### RegenerateRecoveryCodesRequest

RegenerateRecoveryCodesRequest proves the second factor.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `code` | `string` | Code from the authenticator app, or a recovery code. | min length 6, max length 32 |

<a id="izci-api-v1-regeneraterecoverycodesresponse"></a>

### RegenerateRecoveryCodesResponse

RegenerateRecoveryCodesResponse returns the new recovery codes.

| Field | Type | Description |
|---|---|---|
| `recovery_codes` | repeated `string` | Ten new single-use recovery codes. Shown only once. |

<a id="izci-api-v1-release"></a>

### Release

Release is a version of the project's application.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Release id (UUID). |
| `version` | `string` | Version as reported in events, e.g. "1.4.2". |
| `commit_sha` | `string` | Commit SHA, when registered. |
| `repository` | `string` | Repository of the commit, "owner/name". |
| `ref` | `string` | Branch or tag. |
| `url` | `string` | Link to the release page or CI run. |
| `first_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | First event from this release. |
| `last_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Latest event from this release. |
| `deploys` | repeated [Deploy](#izci-api-v1-deploy) | Deployments of this release. |
| `new_issues` | `uint32` | Issues first seen in this release. |
| `health` | [ReleaseHealth](#izci-api-v1-releasehealth) | Session-based health over ListReleasesRequest.health_period; unset when the release reported no sessions in that window. |

<a id="izci-api-v1-releasehealth"></a>

### ReleaseHealth

ReleaseHealth summarizes the sessions Sentry SDKs report for a release
(release health). A session is one run of a mobile or desktop
application, one page visit of a browser application or one request of a
server.

| Field | Type | Description |
|---|---|---|
| `sessions` | `uint64` | Sessions started in the window. |
| `errored_sessions` | `uint64` | Sessions that had at least one error, crashes included. |
| `crashed_sessions` | `uint64` | Sessions that ended in a crash (an unhandled error). |
| `abnormal_sessions` | `uint64` | Sessions that ended abnormally (e.g. the app was killed or hung). |
| `crash_free_sessions` | `double` | Percentage of sessions without a crash, 0-100. |
| `users` | `uint64` | Distinct users (session distinct ids) in the window. |
| `crash_free_users` | `double` | Percentage of users who had no crash, 0-100; 0 when no user ids were reported. |
| `adoption` | `double` | Share of the project's sessions in the window that came from this release, 0-100. |

<a id="izci-api-v1-removememberrequest"></a>

### RemoveMemberRequest

RemoveMemberRequest identifies a member.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `user_id` | `string` | User id of the member. | UUID |

<a id="izci-api-v1-removememberresponse"></a>

### RemoveMemberResponse

RemoveMemberResponse has no fields.

_No fields._

<a id="izci-api-v1-repository"></a>

### Repository

Repository is a repository reachable through an integration.

| Field | Type | Description |
|---|---|---|
| `owner` | `string` | Owner login. |
| `name` | `string` | Repository name. |
| `full_name` | `string` | "owner/name". |
| `url` | `string` | Web URL. |
| `default_branch` | `string` | Default branch. |
| `private` | `bool` | Whether the repository is private. |
| `description` | `string` | Description. |

<a id="izci-api-v1-resolveoptions"></a>

### ResolveOptions

ResolveOptions refine a resolution.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `in_next_release` | `bool` | Resolve in the next release: only events from a release newer than the latest seen release count as regressions. |  |
| `in_release` | `string` | Resolve in this specific release, e.g. "1.4.3". | max length 200 |

<a id="izci-api-v1-revokeaccesstokenrequest"></a>

### RevokeAccessTokenRequest

RevokeAccessTokenRequest identifies a token.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Token id. | UUID |

<a id="izci-api-v1-revokeaccesstokenresponse"></a>

### RevokeAccessTokenResponse

RevokeAccessTokenResponse has no fields.

_No fields._

<a id="izci-api-v1-revokeapikeyrequest"></a>

### RevokeApiKeyRequest

RevokeApiKeyRequest identifies the key to revoke.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `key_id` | `string` | Key id. | UUID |

<a id="izci-api-v1-revokeapikeyresponse"></a>

### RevokeApiKeyResponse

RevokeApiKeyResponse has no fields.

_No fields._

<a id="izci-api-v1-revokeinvitationrequest"></a>

### RevokeInvitationRequest

RevokeInvitationRequest identifies an invitation.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `invitation_id` | `string` | Invitation id. | UUID |

<a id="izci-api-v1-revokeinvitationresponse"></a>

### RevokeInvitationResponse

RevokeInvitationResponse has no fields.

_No fields._

<a id="izci-api-v1-revokesessionrequest"></a>

### RevokeSessionRequest

RevokeSessionRequest identifies the sessions to end.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Session id to end; empty together with all_others ends every session except the current one. |
| `all_others` | `bool` | End all sessions except the current one. |

<a id="izci-api-v1-revokesessionresponse"></a>

### RevokeSessionResponse

RevokeSessionResponse reports how many sessions ended.

| Field | Type | Description |
|---|---|---|
| `revoked` | `uint32` | Number of ended sessions. |

<a id="izci-api-v1-ruleproblem"></a>

### RuleProblem

RuleProblem is an invalid line, skipped when matching.

| Field | Type | Description |
|---|---|---|
| `field` | `string` | Where: "rules" or "codeowners". |
| `line` | `uint32` | Line number, 1-based. |
| `kind` | [RuleProblemKind](#izci-api-v1-ruleproblemkind) | What is wrong. |
| `value` | `string` | The offending type, owner or pattern, when any. |

<a id="izci-api-v1-searchsuggestion"></a>

### SearchSuggestion

SearchSuggestion is one completion for the search box.

| Field | Type | Description |
|---|---|---|
| `text` | `string` | Text that replaces the term under the cursor, e.g. "customer:" or "customer:sivas-belediyesi". |
| `label` | `string` | Label to display, e.g. "Sivas Belediyesi". |
| `description` | `string` | Extra description in English for keys (e.g. "Customer key reported by the application"); the UI shows its own localized text for known keys. |
| `kind` | [SuggestionKind](#izci-api-v1-suggestionkind) | What the suggestion completes. |
| `count` | `uint64` | Number of events or issues with this value, when relevant. |

<a id="izci-api-v1-sendtesteventrequest"></a>

### SendTestEventRequest

SendTestEventRequest identifies the project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |

<a id="izci-api-v1-sendtesteventresponse"></a>

### SendTestEventResponse

SendTestEventResponse points at the created test issue.

| Field | Type | Description |
|---|---|---|
| `event_id` | `string` | Id of the test event. |
| `issue_id` | `string` | Id of the issue it was grouped into. |
| `issue_short_id` | `string` | Short id of that issue. |

<a id="izci-api-v1-sendtestnotificationrequest"></a>

### SendTestNotificationRequest

SendTestNotificationRequest identifies the channel.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `channel_id` | `string` | Channel id. | UUID |

<a id="izci-api-v1-sendtestnotificationresponse"></a>

### SendTestNotificationResponse

SendTestNotificationResponse has no fields.

_No fields._

<a id="izci-api-v1-series"></a>

### Series

Series is a compact, evenly spaced time series of counts.

| Field | Type | Description |
|---|---|---|
| `start` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Start of the first bucket. |
| `interval` | [google.protobuf.Duration](https://protobuf.dev/reference/protobuf/google.protobuf/#duration) | Width of every bucket. |
| `counts` | repeated `uint64` | Count per bucket, oldest first; bucket i covers [start + i·interval, start + (i+1)·interval). |

<a id="izci-api-v1-session"></a>

### Session

Session describes the signed-in user.

| Field | Type | Description |
|---|---|---|
| `user` | [User](#izci-api-v1-user) | The signed-in user. |
| `memberships` | repeated [Membership](#izci-api-v1-membership) | Organizations the user belongs to, ordered by name. |
| `expires_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the session expires unless it is used again (sliding expiration). |

<a id="izci-api-v1-sessioninfo"></a>

### SessionInfo

SessionInfo describes a browser session.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Session id. |
| `current` | `bool` | Whether this is the session making the request. |
| `ip_address` | `string` | Client address that created the session. |
| `client` | `string` | Browser and operating system, e.g. "Chrome 129 · macOS". |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the session was created. |
| `last_seen_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the session was last used. |
| `expires_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the session expires unless used. |

<a id="izci-api-v1-setowneraliasrequest"></a>

### SetOwnerAliasRequest

SetOwnerAliasRequest maps an alias.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `alias` | `string` | "@login", "@org/team" or "#team". | pattern `^[@#][^\s@#]{1,100}$` |
| `user_ids` | repeated `string` | Member ids in order; empty removes the alias. | max 50 items, each UUID |

<a id="izci-api-v1-setowneraliasresponse"></a>

### SetOwnerAliasResponse

SetOwnerAliasResponse returns the alias.

| Field | Type | Description |
|---|---|---|
| `alias` | [OwnerAlias](#izci-api-v1-owneralias) | The alias; unset when removed. |

<a id="izci-api-v1-signuprequest"></a>

### SignUpRequest

SignUpRequest creates an account and an organization.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | Display name. | min length 1, max length 200 |
| `email` | `string` | Email address. | email |
| `password` | `string` | Password, at least 10 characters. | min length 10, max length 200 |
| `organization_name` | `string` | Display name of the new organization. | min length 1, max length 100 |
| `organization_slug` | `string` | Slug of the new organization (same rules as in CompleteSetupRequest). | pattern `^[a-z][a-z0-9-]{0,48}[a-z0-9]$` |

<a id="izci-api-v1-signupresponse"></a>

### SignUpResponse

SignUpResponse returns the new session.

| Field | Type | Description |
|---|---|---|
| `session` | [Session](#izci-api-v1-session) | Session of the new user. |

<a id="izci-api-v1-snooze"></a>

### Snooze

Snooze describes when an ignored issue becomes unresolved again.

| Field | Type | Description |
|---|---|---|
| `until` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Ignore until this moment. |
| `until_count` | `uint64` | Ignore until the issue receives this many more events. |
| `count_since` | `uint64` | Events counted since the issue was ignored. |
| `until_escalating` | `bool` | Ignore until the issue starts escalating. |

<a id="izci-api-v1-startgithubappinstallationrequest"></a>

### StartGitHubAppInstallationRequest

StartGitHubAppInstallationRequest has no fields.

_No fields._

<a id="izci-api-v1-startgithubappinstallationresponse"></a>

### StartGitHubAppInstallationResponse

StartGitHubAppInstallationResponse holds the install URL.

| Field | Type | Description |
|---|---|---|
| `install_url` | `string` | URL to open in the browser. |

<a id="izci-api-v1-startrunrequest"></a>

### StartRunRequest

StartRunRequest describes the analysis.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_id` | `string` | Issue id. | UUID |
| `event_id` | `string` | Event to focus on; defaults to the issue's recommended event. |  |
| `mode` | [AiRunMode](#izci-api-v1-airunmode) | Whether code changes are wanted. |  |
| `model` | `string` | Model override in "provider/model" form. | max length 128 |
| `instructions` | `string` | Extra instructions for this run, e.g. "the bug appears only for customers on the old tariff". Max 4000 characters. | max length 4000 |

<a id="izci-api-v1-startrunresponse"></a>

### StartRunResponse

StartRunResponse returns the queued run.

| Field | Type | Description |
|---|---|---|
| `run` | [AiRun](#izci-api-v1-airun) | The queued run. |

<a id="izci-api-v1-suggestsearchrequest"></a>

### SuggestSearchRequest

SuggestSearchRequest describes the search box state.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `query` | `string` | Current query text. | max length 2000 |
| `cursor` | `uint32` | Cursor position (in characters) within query. |  |
| `projects` | repeated `string` | Projects to draw values from; empty uses all. | max 50 items |

<a id="izci-api-v1-suggestsearchresponse"></a>

### SuggestSearchResponse

SuggestSearchResponse lists completions.

| Field | Type | Description |
|---|---|---|
| `suggestions` | repeated [SearchSuggestion](#izci-api-v1-searchsuggestion) | Suggestions, best first (max 20). |
| `replace_from` | `uint32` | Start (in characters) of the term the suggestions replace. |
| `replace_to` | `uint32` | End (exclusive) of the term the suggestions replace. |

<a id="izci-api-v1-suspectcommit"></a>

### SuspectCommit

SuspectCommit is the commit that last changed the failing lines.

| Field | Type | Description |
|---|---|---|
| `state` | [SuspectCommitState](#izci-api-v1-suspectcommitstate) | Whether it is known. |
| `sha` | `string` | Commit SHA. |
| `message` | `string` | First line of the commit message. |
| `url` | `string` | Commit page on GitHub. |
| `author_name` | `string` | Author name as in the commit. |
| `author_email` | `string` | Author e-mail address as in the commit. |
| `author_login` | `string` | Author's GitHub login, when GitHub knows the address. |
| `author` | [UserRef](#izci-api-v1-userref) | The member who wrote it, when the e-mail address or an "@login" alias matches one. |
| `committed_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When it was committed. |
| `path` | `string` | Repository file whose line it changed. |
| `line` | `uint32` | Line number in that file. |
| `detail` | `string` | Why the search failed (FAILED; GitHub's message in English). |
| `computed_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the search ran. |

<a id="izci-api-v1-tagfacet"></a>

### TagFacet

TagFacet is the value distribution of one tag.

| Field | Type | Description |
|---|---|---|
| `key` | `string` | Tag key, e.g. "customer", "browser", "release". |
| `total` | `uint64` | Events carrying the tag. |
| `unique_values` | `uint32` | Distinct values. |
| `values` | repeated [TagValueCount](#izci-api-v1-tagvaluecount) | Most frequent values, most frequent first. |

<a id="izci-api-v1-tagvaluecount"></a>

### TagValueCount

TagValueCount is one value of a tag facet.

| Field | Type | Description |
|---|---|---|
| `value` | `string` | Raw value, e.g. "sivas-belediyesi". |
| `label` | `string` | Display label, e.g. the customer name "Sivas Belediyesi". |
| `count` | `uint64` | Events with this value. |
| `first_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | First event with this value. |
| `last_seen` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | Latest event with this value. |

<a id="izci-api-v1-telegramaccount"></a>

### TelegramAccount

TelegramAccount is a Telegram user linked to an izci account, which lets
the user act on notification buttons.

| Field | Type | Description |
|---|---|---|
| `user_id` | `int64` | Telegram user id. |
| `username` | `string` | Telegram username without "@", may be empty. |
| `linked_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the account was linked. |

<a id="izci-api-v1-telegramchat"></a>

### TelegramChat

TelegramChat identifies a Telegram chat.

| Field | Type | Description |
|---|---|---|
| `chat_id` | `int64` | Chat id. |
| `thread_id` | `int64` | Forum topic (message_thread_id) in supergroups with topics; 0 for the main chat. |
| `title` | `string` | Chat title or the private chat user's name. |
| `type` | `string` | Chat type: "private", "group", "supergroup" or "channel". |
| `username` | `string` | Public username of the chat without "@", may be empty. |

<a id="izci-api-v1-ticketref"></a>

### TicketRef

TicketRef is a ticket in an external tracker (Jira).

| Field | Type | Description |
|---|---|---|
| `key` | `string` | Ticket key, e.g. "KALEM-123". |
| `url` | `string` | Link to the ticket. |
| `channel_id` | `string` | Channel the ticket was created through. |
| `channel_name` | `string` | Name of that channel. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the ticket was created. |

<a id="izci-api-v1-unlinkrepositoryrequest"></a>

### UnlinkRepositoryRequest

UnlinkRepositoryRequest identifies the project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |

<a id="izci-api-v1-unlinkrepositoryresponse"></a>

### UnlinkRepositoryResponse

UnlinkRepositoryResponse has no fields.

_No fields._

<a id="izci-api-v1-unlinktelegramaccountrequest"></a>

### UnlinkTelegramAccountRequest

UnlinkTelegramAccountRequest has no fields.

_No fields._

<a id="izci-api-v1-unlinktelegramaccountresponse"></a>

### UnlinkTelegramAccountResponse

UnlinkTelegramAccountResponse has no fields.

_No fields._

<a id="izci-api-v1-updatealertrulerequest"></a>

### UpdateAlertRuleRequest

UpdateAlertRuleRequest replaces a rule.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Rule id. | UUID |
| `rule` | [AlertRuleInput](#izci-api-v1-alertruleinput) | New definition. | required |

<a id="izci-api-v1-updatealertruleresponse"></a>

### UpdateAlertRuleResponse

UpdateAlertRuleResponse returns the rule.

| Field | Type | Description |
|---|---|---|
| `rule` | [AlertRule](#izci-api-v1-alertrule) | The updated rule. |

<a id="izci-api-v1-updateapikeyrequest"></a>

### UpdateApiKeyRequest

UpdateApiKeyRequest changes a key.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `key_id` | `string` | Key id. | UUID |
| `name` | `string` | New name; unchanged when empty. | max length 100 |
| `allowed_origins` | repeated `string` | New allowed origins; replaces the list. | max 20 items |
| `rate_limit_per_minute` | `uint32` | New rate limit (0 = project limit). |  |
| `enabled` | `bool` | Enable or disable the key. |  |

<a id="izci-api-v1-updateapikeyresponse"></a>

### UpdateApiKeyResponse

UpdateApiKeyResponse returns the updated key.

| Field | Type | Description |
|---|---|---|
| `key` | [ApiKey](#izci-api-v1-apikey) | The updated key. |

<a id="izci-api-v1-updatechannelrequest"></a>

### UpdateChannelRequest

UpdateChannelRequest changes a channel.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Channel id. | UUID |
| `name` | `string` | New name; unchanged when empty. | max length 100 |
| `muted` | `bool` | Mute or unmute. |  |
| `settings` | [ChannelSettings](#izci-api-v1-channelsettings) | New destination of the same kind; unchanged when unset. Empty secrets (webhook URL, Jira token) keep the current ones. |  |

<a id="izci-api-v1-updatechannelresponse"></a>

### UpdateChannelResponse

UpdateChannelResponse returns the channel.

| Field | Type | Description |
|---|---|---|
| `channel` | [Channel](#izci-api-v1-channel) | The updated channel. |

<a id="izci-api-v1-updatecustomerrequest"></a>

### UpdateCustomerRequest

UpdateCustomerRequest changes a customer.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `key` | `string` | Customer key. | min length 1, max length 64 |
| `name` | optional `string` | New display name; unchanged when unset. | max length 200 |
| `notes` | optional `string` | New notes; unchanged when unset. | max length 10000 |

<a id="izci-api-v1-updatecustomerresponse"></a>

### UpdateCustomerResponse

UpdateCustomerResponse returns the updated customer.

| Field | Type | Description |
|---|---|---|
| `customer` | [Customer](#izci-api-v1-customer) | The updated customer. |

<a id="izci-api-v1-updateissuesrequest"></a>

### UpdateIssuesRequest

UpdateIssuesRequest changes issues.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `issue_ids` | repeated `string` | Issue ids (UUIDs), 1 to 100. | min 1 items, max 100 items, each UUID |
| `status` | optional [IssueStatus](#izci-api-v1-issuestatus) | New status; unchanged when unset. |  |
| `resolve` | [ResolveOptions](#izci-api-v1-resolveoptions) | Resolution details when status is RESOLVED. |  |
| `ignore` | [IgnoreOptions](#izci-api-v1-ignoreoptions) | Ignore condition when status is IGNORED; without it the issue is ignored until changed manually. |  |
| `assignee_id` | optional `string` | New assignee user id; an empty string unassigns; unchanged when unset. |  |
| `priority` | optional [Priority](#izci-api-v1-priority) | New priority; unchanged when unset. |  |

<a id="izci-api-v1-updateissuesresponse"></a>

### UpdateIssuesResponse

UpdateIssuesResponse returns the updated issues.

| Field | Type | Description |
|---|---|---|
| `issues` | repeated [IssueSummary](#izci-api-v1-issuesummary) | The updated issues. |

<a id="izci-api-v1-updatememberrequest"></a>

### UpdateMemberRequest

UpdateMemberRequest changes a member's role.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `user_id` | `string` | User id of the member. | UUID |
| `role` | [Role](#izci-api-v1-role) | New role. | defined values only, must be set |

<a id="izci-api-v1-updatememberresponse"></a>

### UpdateMemberResponse

UpdateMemberResponse returns the updated member.

| Field | Type | Description |
|---|---|---|
| `member` | [Member](#izci-api-v1-member) | The updated member. |

<a id="izci-api-v1-updatemonitorrequest"></a>

### UpdateMonitorRequest

UpdateMonitorRequest changes a monitor; unset fields keep their value.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Monitor id (UUID). | UUID |
| `name` | optional `string` | New display name. | min length 1, max length 100 |
| `schedule` | [MonitorSchedule](#izci-api-v1-monitorschedule) | New schedule. |  |
| `timezone` | optional `string` | New time zone. | max length 64 |
| `checkin_margin_minutes` | optional `uint32` | New lateness margin in minutes. | ≤ 43200 |
| `max_runtime_minutes` | optional `uint32` | New maximum runtime in minutes. | ≤ 10080 |
| `failure_threshold` | optional `uint32` | New failure threshold. | ≤ 720 |
| `recovery_threshold` | optional `uint32` | New recovery threshold. | ≤ 720 |
| `paused` | optional `bool` | Pauses (true) or resumes (false) the monitor. |  |

<a id="izci-api-v1-updatemonitorresponse"></a>

### UpdateMonitorResponse

UpdateMonitorResponse holds the changed monitor.

| Field | Type | Description |
|---|---|---|
| `monitor` | [Monitor](#izci-api-v1-monitor) | The monitor after the change. |

<a id="izci-api-v1-updateorganizationplanrequest"></a>

### UpdateOrganizationPlanRequest

UpdateOrganizationPlanRequest changes plan and limits.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `organization_id` | `string` | Organization id. | UUID |
| `plan` | `string` | Plan name, e.g. "team". | min length 1, max length 50 |
| `limits` | [OrganizationLimits](#izci-api-v1-organizationlimits) | New limits. | required |

<a id="izci-api-v1-updateorganizationplanresponse"></a>

### UpdateOrganizationPlanResponse

UpdateOrganizationPlanResponse returns the organization.

| Field | Type | Description |
|---|---|---|
| `organization` | [Organization](#izci-api-v1-organization) | The updated organization. |

<a id="izci-api-v1-updateorganizationrequest"></a>

### UpdateOrganizationRequest

UpdateOrganizationRequest changes the organization.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | New display name; unchanged when empty. | max length 100 |
| `settings` | [OrganizationSettings](#izci-api-v1-organizationsettings) | New settings; unchanged when unset. |  |

<a id="izci-api-v1-updateorganizationresponse"></a>

### UpdateOrganizationResponse

UpdateOrganizationResponse returns the updated organization.

| Field | Type | Description |
|---|---|---|
| `organization` | [Organization](#izci-api-v1-organization) | The updated organization. |

<a id="izci-api-v1-updateownershiprequest"></a>

### UpdateOwnershipRequest

UpdateOwnershipRequest changes a configuration.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `project` | `string` | Project slug. | min length 1, max length 50 |
| `rules` | optional `string` | New rules; unchanged when unset. |  |
| `auto_assign` | optional [AutoAssign](#izci-api-v1-autoassign) | New assignment mode; unchanged when unset. | defined values only |
| `codeowners` | optional `string` | New CODEOWNERS text, e.g. pasted from a repository GitHub cannot reach; empty removes it; unchanged when unset. |  |

<a id="izci-api-v1-updateownershipresponse"></a>

### UpdateOwnershipResponse

UpdateOwnershipResponse returns the configuration.

| Field | Type | Description |
|---|---|---|
| `ownership` | [Ownership](#izci-api-v1-ownership) | The updated configuration. |

<a id="izci-api-v1-updateprofilerequest"></a>

### UpdateProfileRequest

UpdateProfileRequest changes the profile.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `name` | `string` | New display name. | min length 1, max length 200 |

<a id="izci-api-v1-updateprofileresponse"></a>

### UpdateProfileResponse

UpdateProfileResponse returns the user.

| Field | Type | Description |
|---|---|---|
| `user` | [User](#izci-api-v1-user) | The updated user. |

<a id="izci-api-v1-updateprojectrequest"></a>

### UpdateProjectRequest

UpdateProjectRequest changes a project.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `slug` | `string` | Slug of the project to change. | min length 1, max length 50 |
| `name` | `string` | New display name; unchanged when empty. | max length 100 |
| `platform` | `string` | New platform; unchanged when empty. | max length 32 |
| `settings` | [ProjectSettings](#izci-api-v1-projectsettings) | New settings; replaces all settings when set. |  |

<a id="izci-api-v1-updateprojectresponse"></a>

### UpdateProjectResponse

UpdateProjectResponse returns the updated project.

| Field | Type | Description |
|---|---|---|
| `project` | [Project](#izci-api-v1-project) | The updated project. |

<a id="izci-api-v1-updateuserrequest"></a>

### UpdateUserRequest

UpdateUserRequest changes a user.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `user_id` | `string` | User id. | UUID |
| `superadmin` | optional `bool` | New superadmin flag; unchanged when unset. |  |
| `disabled` | optional `bool` | New disabled flag; unchanged when unset. |  |
| `reset_two_factor` | `bool` | Turns the user's two-factor authentication off and deletes their recovery codes, for a user who lost both. They can sign in with their password alone and set it up again. |  |

<a id="izci-api-v1-updateuserresponse"></a>

### UpdateUserResponse

UpdateUserResponse returns the user.

| Field | Type | Description |
|---|---|---|
| `user` | [AdminUser](#izci-api-v1-adminuser) | The updated user. |

<a id="izci-api-v1-user"></a>

### User

User is an izci account.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | User id (UUID). |
| `email` | `string` | Email address, unique per instance. |
| `name` | `string` | Display name. |
| `avatar_url` | `string` | Avatar image URL (from the identity provider or Gravatar), may be empty. |
| `superadmin` | `bool` | Whether the user administers the whole instance (all organizations). |
| `has_password` | `bool` | Whether the account has a local password (OpenID Connect-only accounts do not). |
| `linked_providers` | repeated `string` | Identity providers linked to the account, e.g. ["kimlik"]. |
| `telegram` | [TelegramAccount](#izci-api-v1-telegramaccount) | Linked Telegram account, when any. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When the account was created. |
| `two_factor_enabled` | `bool` | Whether sign-ins ask for a code from an authenticator app (TOTP). |
| `recovery_codes_left` | `uint32` | Unused recovery codes; set only on the signed-in user's own account (GetSession) while two-factor authentication is on. |

<a id="izci-api-v1-useragentinfo"></a>

### UserAgentInfo

UserAgentInfo is a parsed User-Agent header.

| Field | Type | Description |
|---|---|---|
| `browser` | `string` | Browser name, e.g. "Chrome". |
| `browser_version` | `string` | Browser version, e.g. "129.0.6668.58". |
| `os` | `string` | Operating system, e.g. "Windows". |
| `os_version` | `string` | Operating system version, e.g. "10". |
| `device` | `string` | Device model, e.g. "iPhone". |
| `device_type` | `string` | Form factor: "desktop", "mobile", "tablet", "tv" or "bot". |
| `bot` | `bool` | True for crawlers and other automated clients. |
| `raw` | `string` | The raw header. |

<a id="izci-api-v1-userfeedback"></a>

### UserFeedback

UserFeedback is one message from an end user.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | Feedback id (UUIDv7). |
| `project` | [ProjectRef](#izci-api-v1-projectref) | Project that received the feedback. |
| `issue` | [IssueRef](#izci-api-v1-issueref) | Issue of the event the feedback is about; unset when the feedback is not tied to an event or the event is not stored. |
| `event_id` | `string` | Client-side id of the event the feedback is about, empty when none. |
| `name` | `string` | Name the user gave, e.g. "Ayşe Yılmaz"; empty when anonymous. |
| `email` | `string` | Contact email the user gave; empty when anonymous. |
| `message` | `string` | What the user wrote (scrubbed like event data). |
| `url` | `string` | Page or screen the feedback was sent from. |
| `environment` | `string` | Environment of the reporting application, e.g. "production". |
| `release` | `string` | Release of the reporting application, e.g. "kalem-web@5.0.0". |
| `source` | `string` | Where it came from: "widget" (feedback widget), "api" (captureFeedback), "user_report" (legacy SDK call) or what the SDK sent. |
| `customer` | [CustomerRef](#izci-api-v1-customerref) | Customer of the reporting application, when reported. |
| `user` | `string` | Best label of the application user: email, username or id. |
| `browser` | `string` | Browser name and version, e.g. "Chrome 140", when known. |
| `os` | `string` | Operating system name and version, e.g. "Android 16", when known. |
| `created_at` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When izci received the feedback. |

<a id="izci-api-v1-userref"></a>

### UserRef

UserRef is a compact reference to a user.

| Field | Type | Description |
|---|---|---|
| `id` | `string` | User id (UUID). |
| `name` | `string` | Display name. |
| `email` | `string` | Email address. |
| `avatar_url` | `string` | Avatar image URL, empty when none. |

<a id="izci-api-v1-verifytwofactorrequest"></a>

### VerifyTwoFactorRequest

VerifyTwoFactorRequest carries the second step of a sign-in.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `code` | `string` | Six-digit code from the authenticator app, or a recovery code ("xxxxx-xxxxx"). Spaces and dashes are ignored. | min length 6, max length 32 |

<a id="izci-api-v1-verifytwofactorresponse"></a>

### VerifyTwoFactorResponse

VerifyTwoFactorResponse returns the new session.

| Field | Type | Description |
|---|---|---|
| `session` | [Session](#izci-api-v1-session) | Session of the signed-in user. |

<a id="izci-api-v1-watchliverequest"></a>

### WatchLiveRequest

WatchLiveRequest filters the live stream.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `projects` | repeated `string` | Project slugs; empty includes all. | max 50 items |
| `min_level` | [izci.ingest.v1.Level](izci.ingest.v1.md#izci-ingest-v1-level) | Only events at or above this level. |  |

<a id="izci-api-v1-watchliveresponse"></a>

### WatchLiveResponse

WatchLiveResponse is one live update.

| Field | Type | Description |
|---|---|---|
| `kind` | [LiveEventKind](#izci-api-v1-liveeventkind) | Kind of update. |
| `time` | [google.protobuf.Timestamp](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp) | When it happened. |
| `issue` | [IssueSummary](#izci-api-v1-issuesummary) | The affected issue (all kinds except HEARTBEAT). |
| `event` | [EventSummary](#izci-api-v1-eventsummary) | The stored event (kinds EVENT, ISSUE_CREATED, ISSUE_REGRESSED). |

<a id="izci-api-v1-watchrunrequest"></a>

### WatchRunRequest

WatchRunRequest identifies a run.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `id` | `string` | Run id. | UUID |

<a id="izci-api-v1-watchrunresponse"></a>

### WatchRunResponse

WatchRunResponse is one update of a watched run.

| Field | Type | Description |
|---|---|---|
| `run` | [AiRun](#izci-api-v1-airun) | _oneof `update`._ The run changed state (sent first, and whenever the status changes; the last message carries the finished run). |
| `log` | [AiLogEntry](#izci-api-v1-ailogentry) | _oneof `update`._ A new log entry. |

<a id="izci-api-v1-webhooksettings"></a>

### WebhookSettings

WebhookSettings is the URL a Slack, Teams or webhook channel posts to.

| Field | Type | Description | Constraints |
|---|---|---|---|
| `url` | `string` | Webhook URL (https for Slack and Teams). On update, empty keeps the current URL. | max length 2000 |

## Enums

<a id="izci-api-v1-activitykind"></a>

### ActivityKind

ActivityKind is the kind of an issue activity.

| Name | Number | Description |
|---|---|---|
| `ACTIVITY_KIND_UNSPECIFIED` | 0 | Not set. |
| `ACTIVITY_KIND_CREATED` | 1 | The issue was first seen. |
| `ACTIVITY_KIND_COMMENT` | 2 | A member commented. |
| `ACTIVITY_KIND_STATUS_CHANGED` | 3 | The status changed. |
| `ACTIVITY_KIND_ASSIGNED` | 4 | The assignee changed; data carries "assignee" (an e-mail address, empty when unassigned). Automatic assignment has no actor and adds "auto" ("rule" or "suspect_commit") with "rule" or "commit". |
| `ACTIVITY_KIND_PRIORITY_CHANGED` | 5 | The priority changed. |
| `ACTIVITY_KIND_REGRESSED` | 6 | A resolved issue received a new event. |
| `ACTIVITY_KIND_AI_ANALYSIS` | 7 | An AI analysis finished. |
| `ACTIVITY_KIND_BRANCH_CREATED` | 8 | A fix branch was pushed. |
| `ACTIVITY_KIND_PULL_REQUEST_OPENED` | 9 | A fix pull request was opened. |
| `ACTIVITY_KIND_NOTIFIED` | 10 | A notification was sent. |
| `ACTIVITY_KIND_TICKET_CREATED` | 11 | A ticket was created in an external tracker (Jira); data carries "channel", "key" and "url". |

<a id="izci-api-v1-ailogkind"></a>

### AiLogKind

AiLogKind classifies log entries.

| Name | Number | Description |
|---|---|---|
| `AI_LOG_KIND_UNSPECIFIED` | 0 | Not set. |
| `AI_LOG_KIND_SYSTEM` | 1 | Progress reported by izci (checkout, prompt, collection of results). |
| `AI_LOG_KIND_TEXT` | 2 | Text written by the agent. |
| `AI_LOG_KIND_TOOL` | 3 | A tool call of the agent. |
| `AI_LOG_KIND_STEP` | 4 | An agent step boundary. |
| `AI_LOG_KIND_ERROR` | 5 | An error. |

<a id="izci-api-v1-airunmode"></a>

### AiRunMode

AiRunMode selects whether the agent may change code.

| Name | Number | Description |
|---|---|---|
| `AI_RUN_MODE_UNSPECIFIED` | 0 | Not set; treated as AI_RUN_MODE_FIX when a repository is linked, otherwise AI_RUN_MODE_ANALYZE. |
| `AI_RUN_MODE_ANALYZE` | 1 | Explain the root cause without changing code. |
| `AI_RUN_MODE_FIX` | 2 | Explain the root cause and propose a code fix. |

<a id="izci-api-v1-airunstatus"></a>

### AiRunStatus

AiRunStatus is the lifecycle state of a run.

| Name | Number | Description |
|---|---|---|
| `AI_RUN_STATUS_UNSPECIFIED` | 0 | Not set. |
| `AI_RUN_STATUS_QUEUED` | 1 | Waiting for a free worker. |
| `AI_RUN_STATUS_PREPARING` | 2 | Fetching the repository and preparing the workspace. |
| `AI_RUN_STATUS_RUNNING` | 3 | The agent is working. |
| `AI_RUN_STATUS_SUCCEEDED` | 4 | Finished with a result. |
| `AI_RUN_STATUS_FAILED` | 5 | Finished with an error. |
| `AI_RUN_STATUS_CANCELLED` | 6 | Stopped by a user or by shutdown. |

<a id="izci-api-v1-airuntrigger"></a>

### AiRunTrigger

AiRunTrigger is what started a run.

| Name | Number | Description |
|---|---|---|
| `AI_RUN_TRIGGER_UNSPECIFIED` | 0 | Not set. |
| `AI_RUN_TRIGGER_MANUAL` | 1 | A member started it in the web UI. |
| `AI_RUN_TRIGGER_AUTOMATIC` | 2 | Project automation started it for a new issue. |
| `AI_RUN_TRIGGER_CLI` | 3 | Started with the izci CLI. |
| `AI_RUN_TRIGGER_MCP` | 4 | Started through an MCP tool. |
| `AI_RUN_TRIGGER_TELEGRAM` | 5 | Started from a Telegram notification button. |

<a id="izci-api-v1-aistate"></a>

### AiState

AiState summarizes AI work on an issue.

| Name | Number | Description |
|---|---|---|
| `AI_STATE_UNSPECIFIED` | 0 | Not set. |
| `AI_STATE_NONE` | 1 | No analysis yet. |
| `AI_STATE_RUNNING` | 2 | An analysis is queued or running. |
| `AI_STATE_ANALYZED` | 3 | Analyzed, without proposed code changes. |
| `AI_STATE_FIX_PROPOSED` | 4 | A code fix was proposed and awaits review. |
| `AI_STATE_PULL_REQUEST` | 5 | A branch or pull request with the fix exists. |
| `AI_STATE_FAILED` | 6 | The latest analysis failed. |

<a id="izci-api-v1-apikeyscope"></a>

### ApiKeyScope

ApiKeyScope is a permission of an API key.

| Name | Number | Description |
|---|---|---|
| `API_KEY_SCOPE_UNSPECIFIED` | 0 | Not set. |
| `API_KEY_SCOPE_INGEST` | 1 | Send events (IngestService.SendEvent, SendEvents, Sentry endpoints). |
| `API_KEY_SCOPE_RELEASES` | 2 | Register releases (IngestService.RegisterRelease) and upload source maps and debug files through the Sentry-compatible upload endpoints (sentry-cli, bundler plugins), for CI. Keys embedded in browser or mobile apps must not have it: once a browser sends events with such a key (an Origin header), the key loses this permission for good (see ApiKey.browser_seen_at). New projects get a key without it. |

<a id="izci-api-v1-artifactfilekind"></a>

### ArtifactFileKind

ArtifactFileKind classifies bundle files.

| Name | Number | Description |
|---|---|---|
| `ARTIFACT_FILE_KIND_UNSPECIFIED` | 0 | Not set. |
| `ARTIFACT_FILE_KIND_MINIFIED_SOURCE` | 1 | A minified (generated) script. |
| `ARTIFACT_FILE_KIND_SOURCE_MAP` | 2 | A source map. |
| `ARTIFACT_FILE_KIND_SOURCE` | 3 | An original source file. |

<a id="izci-api-v1-autoaction"></a>

### AutoAction

AutoAction selects what happens after a successful automatic analysis.

| Name | Number | Description |
|---|---|---|
| `AUTO_ACTION_UNSPECIFIED` | 0 | Not set; treated as AUTO_ACTION_NONE. |
| `AUTO_ACTION_NONE` | 1 | Keep the result for review in the UI. |
| `AUTO_ACTION_BRANCH` | 2 | Push the proposed fix to a branch under "izci-auto/". |
| `AUTO_ACTION_DRAFT_PULL_REQUEST` | 3 | Push the fix to a branch under "izci-auto/" and open a draft pull request. |
| `AUTO_ACTION_PULL_REQUEST` | 4 | Legacy value kept for stored settings: automatic pull requests are always drafts, so this is treated as AUTO_ACTION_DRAFT_PULL_REQUEST. |

<a id="izci-api-v1-autoanalyze"></a>

### AutoAnalyze

AutoAnalyze selects when AI analyses start automatically.

| Name | Number | Description |
|---|---|---|
| `AUTO_ANALYZE_UNSPECIFIED` | 0 | Not set; treated as AUTO_ANALYZE_OFF. |
| `AUTO_ANALYZE_OFF` | 1 | Only manual analyses. |
| `AUTO_ANALYZE_NEW_ISSUES` | 2 | Analyze every new issue at or above AiSettings.min_level. |

<a id="izci-api-v1-autoassign"></a>

### AutoAssign

AutoAssign is how a project's new issues are assigned.

| Name | Number | Description |
|---|---|---|
| `AUTO_ASSIGN_UNSPECIFIED` | 0 | Not set; new issues stay unassigned. |
| `AUTO_ASSIGN_OFF` | 1 | New issues stay unassigned. |
| `AUTO_ASSIGN_OWNERS` | 2 | To the first member the deciding rule names, when the issue is created (notifications already show the assignee). |
| `AUTO_ASSIGN_SUSPECT_COMMIT` | 3 | To the author of the suspect commit when they are a member (matched by e-mail address, or by GitHub login through an "@login" alias), otherwise like AUTO_ASSIGN_OWNERS. Runs right after the issue is created, so notifications may go out before it. |

<a id="izci-api-v1-channelkind"></a>

### ChannelKind

ChannelKind is the transport of a channel.

| Name | Number | Description |
|---|---|---|
| `CHANNEL_KIND_UNSPECIFIED` | 0 | Not set. |
| `CHANNEL_KIND_TELEGRAM` | 1 | A Telegram private chat, group, supergroup topic or channel. |
| `CHANNEL_KIND_EMAIL` | 2 | E-mail recipients (needs SMTP_HOST). |
| `CHANNEL_KIND_SLACK` | 3 | A Slack channel through an incoming webhook. |
| `CHANNEL_KIND_TEAMS` | 4 | A Microsoft Teams channel through a Workflows (or incoming) webhook. |
| `CHANNEL_KIND_WEBHOOK` | 5 | Any HTTP endpoint that receives signed JSON. |
| `CHANNEL_KIND_JIRA` | 6 | A Jira project where issues become tickets. |

<a id="izci-api-v1-checkinstatus"></a>

### CheckInStatus

CheckInStatus is the state of a run.

| Name | Number | Description |
|---|---|---|
| `CHECK_IN_STATUS_UNSPECIFIED` | 0 | Not set. |
| `CHECK_IN_STATUS_IN_PROGRESS` | 1 | Still running. |
| `CHECK_IN_STATUS_OK` | 2 | Finished successfully. |
| `CHECK_IN_STATUS_ERROR` | 3 | Finished with an error. |
| `CHECK_IN_STATUS_MISSED` | 4 | Did not check in on schedule. |
| `CHECK_IN_STATUS_TIMEOUT` | 5 | Stayed in progress longer than the maximum runtime. |

<a id="izci-api-v1-customersort"></a>

### CustomerSort

CustomerSort orders customer lists.

| Name | Number | Description |
|---|---|---|
| `CUSTOMER_SORT_UNSPECIFIED` | 0 | Not set; treated as CUSTOMER_SORT_EVENTS. |
| `CUSTOMER_SORT_EVENTS` | 1 | Most events in the period first. |
| `CUSTOMER_SORT_ISSUES` | 2 | Most unresolved issues first. |
| `CUSTOMER_SORT_LAST_SEEN` | 3 | Most recently seen first. |
| `CUSTOMER_SORT_NAME` | 4 | Alphabetical by name. |

<a id="izci-api-v1-debugfilekind"></a>

### DebugFileKind

DebugFileKind classifies debug files.

| Name | Number | Description |
|---|---|---|
| `DEBUG_FILE_KIND_UNSPECIFIED` | 0 | Not set. |
| `DEBUG_FILE_KIND_ELF` | 1 | ELF object with DWARF, e.g. the split debug info of an Android build. |
| `DEBUG_FILE_KIND_MACHO` | 2 | Mach-O object (dSYM companion) with DWARF, e.g. of an iOS build. |
| `DEBUG_FILE_KIND_DART_SYMBOL_MAP` | 3 | Dart obfuscation map (--save-obfuscation-map), for exception types. |

<a id="izci-api-v1-errorreason"></a>

### ErrorReason

ErrorReason enumerates the causes of API errors. The web UI maps each
reason to a localized message.

| Name | Number | Description |
|---|---|---|
| `ERROR_REASON_UNSPECIFIED` | 0 | Not set. |
| `ERROR_REASON_UNAUTHENTICATED` | 1 | The caller is not signed in or the session expired. |
| `ERROR_REASON_PERMISSION_DENIED` | 2 | The caller's role or token scope does not allow the operation. |
| `ERROR_REASON_ORGANIZATION_REQUIRED` | 3 | The call needs an organization (Izci-Org header) and none was given, or the caller is not a member of it. |
| `ERROR_REASON_NOT_FOUND` | 4 | The requested resource does not exist or is not visible to the caller. |
| `ERROR_REASON_INVALID_ARGUMENT` | 5 | A request field is invalid; ErrorDetail.field names it. |
| `ERROR_REASON_INVALID_CREDENTIALS` | 6 | Email or password is wrong. |
| `ERROR_REASON_ACCOUNT_DISABLED` | 7 | The user account is disabled. |
| `ERROR_REASON_TOO_MANY_ATTEMPTS` | 8 | Too many attempts; retry later (metadata "retry_after_seconds"). |
| `ERROR_REASON_WEAK_PASSWORD` | 9 | The new password does not meet the policy (metadata "min_length"). |
| `ERROR_REASON_SETUP_COMPLETED` | 10 | Initial setup was already completed. |
| `ERROR_REASON_SETUP_REQUIRED` | 11 | Initial setup must be completed first. |
| `ERROR_REASON_SIGNUP_DISABLED` | 12 | Self sign-up is disabled on this instance. |
| `ERROR_REASON_EMAIL_TAKEN` | 13 | Another account already uses this email address. |
| `ERROR_REASON_SLUG_TAKEN` | 14 | The slug is already in use. |
| `ERROR_REASON_INVITATION_INVALID` | 15 | The invitation does not exist, was revoked, used or expired. |
| `ERROR_REASON_LAST_OWNER` | 16 | The operation would leave the organization without an owner. |
| `ERROR_REASON_LIMIT_REACHED` | 17 | A plan limit was reached (metadata "limit", "resource"). |
| `ERROR_REASON_QUOTA_EXCEEDED` | 18 | The monthly quota is exhausted. |
| `ERROR_REASON_INVALID_QUERY` | 19 | The search query cannot be parsed (metadata "position", "token"). |
| `ERROR_REASON_INTEGRATION_FAILED` | 20 | An external integration call failed (metadata "provider", "status"). |
| `ERROR_REASON_GITHUB_TOKEN_INVALID` | 21 | The GitHub token is invalid or lacks the required permissions. |
| `ERROR_REASON_GITHUB_APP_NOT_CONFIGURED` | 22 | The GitHub App is not configured on this instance. |
| `ERROR_REASON_REPOSITORY_NOT_LINKED` | 23 | The project has no linked repository. |
| `ERROR_REASON_AI_DISABLED` | 24 | AI analysis is disabled on this instance or project. |
| `ERROR_REASON_AI_BUDGET_EXCEEDED` | 25 | The AI run budget (daily or monthly) is exhausted. |
| `ERROR_REASON_AI_RUN_ACTIVE` | 26 | An AI run is already queued or running for this issue. |
| `ERROR_REASON_AI_RUN_NOT_READY` | 27 | The AI run has not finished successfully or has no code changes. |
| `ERROR_REASON_TELEGRAM_NOT_CONFIGURED` | 28 | Telegram is not configured on this instance. |
| `ERROR_REASON_CONFLICT` | 29 | The resource was modified concurrently; reload and retry. |
| `ERROR_REASON_INTERNAL` | 30 | Unexpected server error. |
| `ERROR_REASON_CHANNEL_UNAVAILABLE` | 31 | The channel kind is not available on this server (e-mail without an SMTP server). |
| `ERROR_REASON_TWO_FACTOR_REQUIRED` | 32 | The organization requires two-factor authentication and the caller has not turned it on (UserService.BeginTwoFactorSetup). |
| `ERROR_REASON_INVALID_TWO_FACTOR_CODE` | 33 | The two-factor code (from the authenticator app, or a recovery code) is wrong or was used already. |
| `ERROR_REASON_TWO_FACTOR_EXPIRED` | 34 | The sign-in or setup waiting for a two-factor code expired; sign in or start the setup again. |
| `ERROR_REASON_AI_SENSITIVE_CHANGES` | 35 | An AI run changes files CI, build tools or git run; metadata "files" lists them. Publish with allow_sensitive_changes after reviewing them. |
| `ERROR_REASON_SETUP_TOKEN_INVALID` | 36 | The setup code is missing or wrong; izci prints it in its log while no user exists. |

<a id="izci-api-v1-eventpick"></a>

### EventPick

EventPick selects an event of an issue.

| Name | Number | Description |
|---|---|---|
| `EVENT_PICK_UNSPECIFIED` | 0 | Not set; treated as EVENT_PICK_LATEST. |
| `EVENT_PICK_LATEST` | 1 | Most recently received event. |
| `EVENT_PICK_OLDEST` | 2 | First received event. |
| `EVENT_PICK_RECOMMENDED` | 3 | Most informative recent event (has a stack trace with in-app frames, request and user data), falling back to the latest. |

<a id="izci-api-v1-filterreason"></a>

### FilterReason

FilterReason is why an event was filtered at ingest.

| Name | Number | Description |
|---|---|---|
| `FILTER_REASON_UNSPECIFIED` | 0 | Not set. |
| `FILTER_REASON_ENVIRONMENT` | 1 | Its environment is ignored. |
| `FILTER_REASON_BROWSER_EXTENSION` | 2 | It came from a browser extension. |
| `FILTER_REASON_WEB_CRAWLER` | 3 | A web crawler sent it. |
| `FILTER_REASON_LEGACY_BROWSER` | 4 | An old browser sent it. |
| `FILTER_REASON_LOCALHOST` | 5 | It came from local development. |
| `FILTER_REASON_ERROR_MESSAGE` | 6 | Its message matched a pattern. |
| `FILTER_REASON_IP_ADDRESS` | 7 | Its client address matched. |
| `FILTER_REASON_RELEASE` | 8 | Its release matched a pattern. |

<a id="izci-api-v1-health"></a>

### Health

Health rates how a customer is affected by errors.

| Name | Number | Description |
|---|---|---|
| `HEALTH_UNSPECIFIED` | 0 | Not set. |
| `HEALTH_HEALTHY` | 1 | No unresolved errors of level ERROR or above in the period. |
| `HEALTH_DEGRADED` | 2 | Unresolved errors, none critical. |
| `HEALTH_CRITICAL` | 3 | Fatal, unhandled or rapidly growing errors. |

<a id="izci-api-v1-integrationauth"></a>

### IntegrationAuth

IntegrationAuth is how izci authenticates to the host.

| Name | Number | Description |
|---|---|---|
| `INTEGRATION_AUTH_UNSPECIFIED` | 0 | Not set. |
| `INTEGRATION_AUTH_TOKEN` | 1 | A personal access token, stored encrypted. |
| `INTEGRATION_AUTH_APP` | 2 | A GitHub App installation. |

<a id="izci-api-v1-ipstorage"></a>

### IpStorage

IpStorage selects how IP addresses are stored.

| Name | Number | Description |
|---|---|---|
| `IP_STORAGE_UNSPECIFIED` | 0 | Not set; treated as IP_STORAGE_FULL. |
| `IP_STORAGE_FULL` | 1 | Store the full address. |
| `IP_STORAGE_ANONYMIZED` | 2 | Store the network only (IPv4 /24, IPv6 /48). |
| `IP_STORAGE_NONE` | 3 | Do not store addresses at all. |

<a id="izci-api-v1-issuesort"></a>

### IssueSort

IssueSort orders issue lists.

| Name | Number | Description |
|---|---|---|
| `ISSUE_SORT_UNSPECIFIED` | 0 | Not set; treated as ISSUE_SORT_LAST_SEEN. |
| `ISSUE_SORT_LAST_SEEN` | 1 | Most recently seen first. |
| `ISSUE_SORT_FIRST_SEEN` | 2 | Newest issues first. |
| `ISSUE_SORT_EVENTS` | 3 | Most events within the stats period first. |
| `ISSUE_SORT_USERS` | 4 | Most affected users first. |
| `ISSUE_SORT_PRIORITY` | 5 | Highest priority first. |

<a id="izci-api-v1-issuestatus"></a>

### IssueStatus

IssueStatus is the triage state of an issue.

| Name | Number | Description |
|---|---|---|
| `ISSUE_STATUS_UNSPECIFIED` | 0 | Not set. |
| `ISSUE_STATUS_UNRESOLVED` | 1 | Needs attention. New events keep arriving into it. |
| `ISSUE_STATUS_RESOLVED` | 2 | Marked as fixed. A new event reopens it as a regression. |
| `ISSUE_STATUS_IGNORED` | 3 | Muted. New events are counted but do not notify, until the ignore condition (duration, event count or escalation) expires. |

<a id="izci-api-v1-issuesubstatus"></a>

### IssueSubstatus

IssueSubstatus refines an unresolved issue.

| Name | Number | Description |
|---|---|---|
| `ISSUE_SUBSTATUS_UNSPECIFIED` | 0 | Not set (resolved and ignored issues). |
| `ISSUE_SUBSTATUS_NEW` | 1 | First seen within the last 72 hours. |
| `ISSUE_SUBSTATUS_ONGOING` | 2 | Unresolved for longer than 72 hours with a normal event rate. |
| `ISSUE_SUBSTATUS_REGRESSED` | 3 | Was resolved and received a new event (in a newer release when the issue was resolved "in next release"). |
| `ISSUE_SUBSTATUS_ESCALATING` | 4 | Event rate is significantly above its recent baseline. |

<a id="izci-api-v1-liveeventkind"></a>

### LiveEventKind

LiveEventKind classifies live updates.

| Name | Number | Description |
|---|---|---|
| `LIVE_EVENT_KIND_UNSPECIFIED` | 0 | Not set. |
| `LIVE_EVENT_KIND_HEARTBEAT` | 1 | Keep-alive; carries no data. |
| `LIVE_EVENT_KIND_EVENT` | 2 | An event was stored. |
| `LIVE_EVENT_KIND_ISSUE_CREATED` | 3 | A new issue was created. |
| `LIVE_EVENT_KIND_ISSUE_REGRESSED` | 4 | A resolved issue regressed. |
| `LIVE_EVENT_KIND_ISSUE_UPDATED` | 5 | An issue was triaged (status, assignee, priority). |

<a id="izci-api-v1-monitorintervalunit"></a>

### MonitorIntervalUnit

MonitorIntervalUnit is the unit of an interval schedule.

| Name | Number | Description |
|---|---|---|
| `MONITOR_INTERVAL_UNIT_UNSPECIFIED` | 0 | Not set. |
| `MONITOR_INTERVAL_UNIT_MINUTE` | 1 | Minutes. |
| `MONITOR_INTERVAL_UNIT_HOUR` | 2 | Hours. |
| `MONITOR_INTERVAL_UNIT_DAY` | 3 | Days. |
| `MONITOR_INTERVAL_UNIT_WEEK` | 4 | Weeks. |
| `MONITOR_INTERVAL_UNIT_MONTH` | 5 | Calendar months. |
| `MONITOR_INTERVAL_UNIT_YEAR` | 6 | Calendar years. |

<a id="izci-api-v1-monitorstatus"></a>

### MonitorStatus

MonitorStatus is the state of a monitor environment.

| Name | Number | Description |
|---|---|---|
| `MONITOR_STATUS_UNSPECIFIED` | 0 | Not set. |
| `MONITOR_STATUS_WAITING` | 1 | No run has finished yet. |
| `MONITOR_STATUS_OK` | 2 | The last run succeeded. |
| `MONITOR_STATUS_ERROR` | 3 | The last run failed. |
| `MONITOR_STATUS_MISSED` | 4 | The last expected run did not check in. |
| `MONITOR_STATUS_TIMEOUT` | 5 | The last run did not finish within the maximum runtime. |

<a id="izci-api-v1-priority"></a>

### Priority

Priority is the triage priority of an issue.

| Name | Number | Description |
|---|---|---|
| `PRIORITY_UNSPECIFIED` | 0 | Not set. |
| `PRIORITY_LOW` | 1 | Low impact. |
| `PRIORITY_MEDIUM` | 2 | Default for new issues. |
| `PRIORITY_HIGH` | 3 | Needs attention soon. |
| `PRIORITY_CRITICAL` | 4 | Needs attention now: fatal, unhandled or wide impact. |

<a id="izci-api-v1-risk"></a>

### Risk

Risk rates a proposed change.

| Name | Number | Description |
|---|---|---|
| `RISK_UNSPECIFIED` | 0 | Not set. |
| `RISK_LOW` | 1 | Local, well-contained change. |
| `RISK_MEDIUM` | 2 | Touches shared code paths. |
| `RISK_HIGH` | 3 | Broad or behavior-changing; review carefully. |

<a id="izci-api-v1-role"></a>

### Role

Role is a member's role in an organization. Every role includes the
permissions of the roles listed before it.

| Name | Number | Description |
|---|---|---|
| `ROLE_UNSPECIFIED` | 0 | Not set. |
| `ROLE_VIEWER` | 1 | Read-only access to issues, events, customers and dashboards. |
| `ROLE_MEMBER` | 2 | Triage issues (status, assignment, priority), comment and run AI analyses. |
| `ROLE_ADMIN` | 3 | Manage projects, API keys, alert rules, channels, integrations and members (except owners). |
| `ROLE_OWNER` | 4 | Full control, including owners, organization settings and deletion. |

<a id="izci-api-v1-ruleproblemkind"></a>

### RuleProblemKind

RuleProblemKind is what is wrong with a rule line.

| Name | Number | Description |
|---|---|---|
| `RULE_PROBLEM_KIND_UNSPECIFIED` | 0 | Not set. |
| `RULE_PROBLEM_KIND_SYNTAX` | 1 | The line is not "type:pattern owner...". |
| `RULE_PROBLEM_KIND_UNKNOWN_TYPE` | 2 | Unknown rule type (value); known are path, module, url, tags.<key> and codeowners. |
| `RULE_PROBLEM_KIND_NO_OWNERS` | 3 | The rule names no owner. |
| `RULE_PROBLEM_KIND_INVALID_OWNER` | 4 | An owner (value) is neither an e-mail address nor "@name" or "#team". |
| `RULE_PROBLEM_KIND_INVALID_PATTERN` | 5 | The pattern (value) cannot be compiled. |
| `RULE_PROBLEM_KIND_TOO_MANY` | 6 | More than 5000 rules, or more than 20 owners on a line. |

<a id="izci-api-v1-signupmode"></a>

### SignupMode

SignupMode controls who may create accounts.

| Name | Number | Description |
|---|---|---|
| `SIGNUP_MODE_UNSPECIFIED` | 0 | Not set. |
| `SIGNUP_MODE_INVITE` | 1 | Accounts are created only through invitations (and OpenID Connect just-in-time provisioning when enabled). |
| `SIGNUP_MODE_OPEN` | 2 | Anyone may sign up and create an organization. |
| `SIGNUP_MODE_CLOSED` | 3 | No new accounts except by instance administrators. |

<a id="izci-api-v1-statsperiod"></a>

### StatsPeriod

StatsPeriod is a trailing time window used for counts and charts.

| Name | Number | Description |
|---|---|---|
| `STATS_PERIOD_UNSPECIFIED` | 0 | Not set; methods document their own default (usually 24 hours). |
| `STATS_PERIOD_LAST_HOUR` | 1 | Last hour, in 1-minute buckets. |
| `STATS_PERIOD_LAST_24_HOURS` | 2 | Last 24 hours, in 1-hour buckets. |
| `STATS_PERIOD_LAST_7_DAYS` | 3 | Last 7 days, in 6-hour buckets. |
| `STATS_PERIOD_LAST_14_DAYS` | 4 | Last 14 days, in 1-day buckets. |
| `STATS_PERIOD_LAST_30_DAYS` | 5 | Last 30 days, in 1-day buckets. |
| `STATS_PERIOD_LAST_90_DAYS` | 6 | Last 90 days, in 1-day buckets. |

<a id="izci-api-v1-suggestionkind"></a>

### SuggestionKind

SuggestionKind classifies search suggestions.

| Name | Number | Description |
|---|---|---|
| `SUGGESTION_KIND_UNSPECIFIED` | 0 | Not set. |
| `SUGGESTION_KIND_KEY` | 1 | A search key, e.g. "customer:". |
| `SUGGESTION_KIND_VALUE` | 2 | A value for a key. |
| `SUGGESTION_KIND_QUERY` | 3 | A saved or recent full query. |

<a id="izci-api-v1-suspectcommitstate"></a>

### SuspectCommitState

SuspectCommitState says whether an issue's suspect commit is known.

| Name | Number | Description |
|---|---|---|
| `SUSPECT_COMMIT_STATE_UNSPECIFIED` | 0 | Not set. |
| `SUSPECT_COMMIT_STATE_NOT_CONFIGURED` | 1 | No repository is linked through a GitHub connection. |
| `SUSPECT_COMMIT_STATE_UNKNOWN` | 2 | Not searched yet: call FindSuspectCommit. |
| `SUSPECT_COMMIT_STATE_FOUND` | 3 | Found. |
| `SUSPECT_COMMIT_STATE_NO_FRAMES` | 4 | The event has no stack frames with line numbers. |
| `SUSPECT_COMMIT_STATE_NOT_IN_REPOSITORY` | 5 | The files of the frames are not in the repository at that commit. |
| `SUSPECT_COMMIT_STATE_FAILED` | 6 | GitHub refused or failed; detail says why. |

<a id="izci-api-v1-tokenscope"></a>

### TokenScope

TokenScope is a permission of a personal access token.

| Name | Number | Description |
|---|---|---|
| `TOKEN_SCOPE_UNSPECIFIED` | 0 | Not set. |
| `TOKEN_SCOPE_READ` | 1 | Read issues, events, customers, projects and dashboards. |
| `TOKEN_SCOPE_WRITE` | 2 | Triage issues and comment (requires the member role). |
| `TOKEN_SCOPE_AI` | 3 | Start AI runs and create branches or pull requests. |

<a id="izci-api-v1-triggerkind"></a>

### TriggerKind

TriggerKind is the kind of an alert trigger.

| Name | Number | Description |
|---|---|---|
| `TRIGGER_KIND_UNSPECIFIED` | 0 | Not set. |
| `TRIGGER_KIND_NEW_ISSUE` | 1 | A new issue was created. |
| `TRIGGER_KIND_REGRESSION` | 2 | A resolved issue received a new event. |
| `TRIGGER_KIND_FREQUENCY` | 3 | An issue received at least threshold events within window. |

