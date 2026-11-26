import { useQuery } from "@connectrpc/connect-query";
import { AppShellBottomNav, AppShellBottomNavItem } from "@kentos/components/layout/app-shell";
import { useSidebar } from "@kentos/components/layout/sidebar";
import { Link } from "@tanstack/react-router";
import { MenuIcon } from "lucide-react";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { ViewService } from "@/gen/izci/api/v1/view_pb";
import { formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { type NavItem, primaryNav } from "./nav";

// The destinations people check on the go; everything else is one tap away
// in the menu (the sidebar drawer).
const destinations: NavItem["to"][] = ["/o/$org", "/o/$org/issues", "/o/$org/live", "/o/$org/dashboards"];

/**
 * The phone tab bar: four primary pages (issues with the open count,
 * dashboards with their number) and the full menu. Hidden from md up.
 */
export function BottomNav() {
  const org = useOrg();
  const { setOpenMobile, openMobile, isMobile } = useSidebar();
  // Open issues across the organization's projects: each project carries its
  // unresolved count. Shares the cache with the project filter and palette,
  // and only runs while the bar is shown.
  const projects = useQuery(
    ProjectService.method.listProjects,
    {},
    { enabled: isMobile, staleTime: 60_000, refetchInterval: isMobile ? 60_000 : false },
  );
  const open = (projects.data?.projects ?? []).reduce((sum, p) => sum + (p.stats?.unresolvedIssues ?? 0), 0);
  // The caller's and shared dashboards; the same query as the dashboards
  // page, so creating or deleting one updates the count.
  const dashboards = useQuery(
    ViewService.method.listDashboards,
    {},
    { enabled: isMobile, staleTime: 60_000 },
  );
  const boards = dashboards.data?.dashboards.length ?? 0;
  const badges: Partial<Record<NavItem["to"], { count: number; label: string; tone: "danger" | "neutral" }>> =
    {
      "/o/$org/issues": { count: open, label: `${formatNumber(open)} açık hata`, tone: "danger" },
      "/o/$org/dashboards": { count: boards, label: `${formatNumber(boards)} pano`, tone: "neutral" },
    };
  const items = destinations
    .map((to) => primaryNav.find((item) => item.to === to))
    .filter((item): item is NavItem => item !== undefined);
  return (
    <AppShellBottomNav label="Hızlı erişim">
      {items.map((item) => (
        <AppShellBottomNavItem
          key={item.to}
          icon={item.icon}
          label={item.label}
          badge={badges[item.to]?.count || undefined}
          badgeLabel={badges[item.to]?.label}
          badgeTone={badges[item.to]?.tone}
          asChild
        >
          <Link
            to={item.to}
            params={{ org: org.slug }}
            activeOptions={{ exact: item.exact ?? false, includeSearch: false }}
          />
        </AppShellBottomNavItem>
      ))}
      <AppShellBottomNavItem icon={MenuIcon} label="Menü" asChild>
        <button type="button" aria-expanded={openMobile} onClick={() => setOpenMobile(true)} />
      </AppShellBottomNavItem>
    </AppShellBottomNav>
  );
}
