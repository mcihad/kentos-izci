package api_test

import (
	"context"
	"strings"
	"testing"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	ingestv1 "github.com/mcihad/kentos-izci/sdk/go/proto/izci/ingest/v1"
)

// TestSavedSearchesAndDashboards keeps personal and shared views apart and
// sums up searches for dashboard charts.
func TestSavedSearchesAndDashboards(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := setupKentos(t, e)
	for range 3 {
		sendEvent(t, e, "kalem", "a", func(*ingestv1.Event) {})
	}
	sendEvent(t, e, "kalem", "b", func(*ingestv1.Event) {})
	inv, err := owner.org.InviteMember(ctx, req(&apiv1.InviteMemberRequest{Email: "izleyici@example.com", Role: apiv1.Role_ROLE_VIEWER}))
	if err != nil {
		t.Fatal(err)
	}
	url := inv.Msg.GetInvitation().GetUrl()
	viewer := e.client(map[string]string{"Izci-Org": "kentos"})
	if _, err := viewer.auth.AcceptInvitation(ctx, req(&apiv1.AcceptInvitationRequest{Token: url[strings.LastIndex(url, "/")+1:],
		Name: "İzleyici", Password: "izleyicinin uzun parolası"})); err != nil {
		t.Fatal(err)
	}

	mine, err := owner.view.SaveSearch(ctx, req(&apiv1.SaveSearchRequest{Search: &apiv1.SavedSearch{Name: "Benim", Query: "is:unresolved",
		Sort: apiv1.IssueSort_ISSUE_SORT_EVENTS}}))
	if err != nil {
		t.Fatal(err)
	}
	shared, err := owner.view.SaveSearch(ctx, req(&apiv1.SaveSearchRequest{Search: &apiv1.SavedSearch{Name: "Ortak", Query: "times_seen:>2",
		Shared: true, Projects: []string{"KALEM"}}}))
	if err != nil || shared.Msg.GetSearch().GetProjects()[0] != "kalem" {
		t.Fatalf("shared search: %v %v", shared, err)
	}
	if _, err := owner.view.SaveSearch(ctx, req(&apiv1.SaveSearchRequest{Search: &apiv1.SavedSearch{Name: "Bozuk", Query: "level:loud"}})); reason(err) != apiv1.ErrorReason_ERROR_REASON_INVALID_QUERY {
		t.Fatalf("invalid query saved: %v", err)
	}
	// The viewer sees the shared search only, cannot change it and cannot
	// share their own.
	list, err := viewer.view.ListSavedSearches(ctx, req(&apiv1.ListSavedSearchesRequest{}))
	if err != nil || len(list.Msg.GetSearches()) != 1 || list.Msg.GetSearches()[0].GetName() != "Ortak" || list.Msg.GetSearches()[0].GetEditable() {
		t.Fatalf("viewer's searches: %v %v", list, err)
	}
	if _, err := viewer.view.DeleteSavedSearch(ctx, req(&apiv1.DeleteSavedSearchRequest{Id: shared.Msg.GetSearch().GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("viewer deleting a shared search: %v", err)
	}
	if _, err := viewer.view.DeleteSavedSearch(ctx, req(&apiv1.DeleteSavedSearchRequest{Id: mine.Msg.GetSearch().GetId()})); reason(err) != apiv1.ErrorReason_ERROR_REASON_NOT_FOUND {
		t.Fatalf("viewer deleting someone's personal search: %v", err)
	}
	if _, err := viewer.view.SaveSearch(ctx, req(&apiv1.SaveSearchRequest{Search: &apiv1.SavedSearch{Name: "Paylaş", Shared: true}})); reason(err) != apiv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED {
		t.Fatalf("viewer sharing: %v", err)
	}
	if _, err := viewer.view.SaveSearch(ctx, req(&apiv1.SaveSearchRequest{Search: &apiv1.SavedSearch{Name: "Kendime"}})); err != nil {
		t.Fatalf("viewer's own search: %v", err)
	}

	// Dashboards.
	board, err := owner.view.SaveDashboard(ctx, req(&apiv1.SaveDashboardRequest{Dashboard: &apiv1.Dashboard{Name: "Ödeme", Shared: true,
		Widgets: []*apiv1.DashboardWidget{
			{Title: "Olaylar", Kind: apiv1.DashboardWidgetKind_DASHBOARD_WIDGET_KIND_CHART, Query: "is:unresolved"},
			{Title: "En çok", Kind: apiv1.DashboardWidgetKind_DASHBOARD_WIDGET_KIND_ISSUES, Sort: apiv1.IssueSort_ISSUE_SORT_EVENTS, Limit: 5},
		}}}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := viewer.view.GetDashboard(ctx, req(&apiv1.GetDashboardRequest{Id: board.Msg.GetDashboard().GetId()}))
	if err != nil || len(got.Msg.GetDashboard().GetWidgets()) != 2 || got.Msg.GetDashboard().GetEditable() ||
		got.Msg.GetDashboard().GetOwner().GetEmail() != "owner@example.com" {
		t.Fatalf("dashboard: %v %v", got, err)
	}
	boards, err := viewer.view.ListDashboards(ctx, req(&apiv1.ListDashboardsRequest{}))
	if err != nil || len(boards.Msg.GetDashboards()) != 1 || len(boards.Msg.GetDashboards()[0].GetWidgets()) != 0 {
		t.Fatalf("dashboards: %v %v", boards, err)
	}

	// Charts sum up the matching issues.
	stats, err := viewer.issue.GetQueryStats(ctx, req(&apiv1.GetQueryStatsRequest{Query: "is:unresolved"}))
	if err != nil {
		t.Fatal(err)
	}
	var sum uint64
	for _, c := range stats.Msg.GetSeries().GetCounts() {
		sum += c
	}
	if stats.Msg.GetIssues() != 2 || stats.Msg.GetEvents() != 4 || sum != 4 || len(stats.Msg.GetSeries().GetCounts()) != 24 {
		t.Fatalf("stats: %v", stats.Msg)
	}
	if stats, err := viewer.issue.GetQueryStats(ctx, req(&apiv1.GetQueryStatsRequest{Query: "times_seen:>2",
		StatsPeriod: apiv1.StatsPeriod_STATS_PERIOD_LAST_7_DAYS})); err != nil || stats.Msg.GetIssues() != 1 || stats.Msg.GetEvents() != 3 {
		t.Fatalf("filtered stats: %v %v", stats, err)
	}
	if _, err := owner.view.DeleteDashboard(ctx, req(&apiv1.DeleteDashboardRequest{Id: board.Msg.GetDashboard().GetId()})); err != nil {
		t.Fatal(err)
	}
}
