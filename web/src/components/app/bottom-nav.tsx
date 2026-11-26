import { AppShellBottomNav, AppShellBottomNavItem } from "@kentos/components/layout/app-shell";
import { useSidebar } from "@kentos/components/layout/sidebar";
import { Link } from "@tanstack/react-router";
import { MenuIcon } from "lucide-react";
import { useOrg } from "@/lib/org";
import { type NavItem, primaryNav } from "./nav";

// The destinations people check on the go; everything else is one tap away
// in the menu (the sidebar drawer).
const destinations: NavItem["to"][] = ["/o/$org", "/o/$org/issues", "/o/$org/live", "/o/$org/dashboards"];

/** The phone tab bar: four primary pages and the full menu. Hidden from md up. */
export function BottomNav() {
  const org = useOrg();
  const { setOpenMobile, openMobile } = useSidebar();
  const items = destinations
    .map((to) => primaryNav.find((item) => item.to === to))
    .filter((item): item is NavItem => item !== undefined);
  return (
    <AppShellBottomNav label="Hızlı erişim">
      {items.map((item) => (
        <AppShellBottomNavItem key={item.to} icon={item.icon} label={item.label} asChild>
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
