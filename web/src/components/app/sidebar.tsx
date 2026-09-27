import { Link } from "@tanstack/react-router";
import { BookOpenIcon, type LucideIcon, SettingsIcon, ShieldCheckIcon } from "lucide-react";
import { useOrg } from "@/lib/org";
import { canAdmin, useSession } from "@/lib/session";
import { cn } from "@/lib/utils";
import { primaryNav } from "./nav";
import { OrgSwitcher } from "./org-switcher";
import { UserMenu } from "./user-menu";

// Rail items of the KentOS sidebar: white text on the dark rail, the current
// page on a lighter tint. Colours and weights live in the active/inactive
// props so the two states never fight over the same property.
const itemClass =
  "relative flex h-10 items-center gap-2.5 rounded-md text-sm transition-colors duration-(--duration-fast) md:h-9";
const activeClass = "bg-(--sidebar-item-active-bg) font-semibold text-(--sidebar-item-active-fg)";
const inactiveClass = "font-medium text-(--sidebar-fg) hover:bg-(--sidebar-item-hover) [&>svg]:opacity-80";

type Item = {
  to: "/o/$org" | "/o/$org/docs" | "/o/$org/settings" | "/o/$org/admin" | (typeof primaryNav)[number]["to"];
  label: string;
  icon: LucideIcon;
  exact?: boolean;
};

function RailLink({
  item,
  collapsed,
  onNavigate,
}: {
  item: Item;
  collapsed: boolean;
  onNavigate?: () => void;
}) {
  const org = useOrg();
  return (
    <Link
      to={item.to}
      params={{ org: org.slug }}
      activeOptions={{ exact: item.exact ?? false, includeSearch: false }}
      className={cn(itemClass, collapsed ? "justify-center" : "px-2.5")}
      activeProps={{ className: activeClass }}
      inactiveProps={{ className: inactiveClass }}
      title={collapsed ? item.label : undefined}
      onClick={onNavigate}
    >
      <item.icon className="size-4 shrink-0" />
      <span className={collapsed ? "sr-only" : "truncate"}>{item.label}</span>
    </Link>
  );
}

export function SidebarContent({
  collapsed = false,
  onNavigate,
  inSheet = false,
}: {
  collapsed?: boolean;
  onNavigate?: () => void;
  /** Leaves room for the drawer's close button beside the organization switch. */
  inSheet?: boolean;
}) {
  const org = useOrg();
  const session = useSession();
  const bottom: Item[] = [{ to: "/o/$org/docs", label: "Belgeler", icon: BookOpenIcon }];
  if (canAdmin(org.role))
    bottom.push({ to: "/o/$org/settings", label: "Organizasyon ayarları", icon: SettingsIcon });
  if (session?.user?.superadmin)
    bottom.push({ to: "/o/$org/admin", label: "Sunucu yönetimi", icon: ShieldCheckIcon });
  return (
    <div className="flex h-full flex-col">
      <div className={cn("pt-2.5", collapsed ? "px-2" : "px-2.5", inSheet && "pr-12")}>
        <OrgSwitcher collapsed={collapsed} />
      </div>
      <div
        className={cn("flex min-h-0 flex-1 flex-col overflow-y-auto py-2.5", collapsed ? "px-2" : "px-2.5")}
      >
        <nav className="grid gap-0.5" aria-label="Ana menü">
          {primaryNav.map((item) => (
            <RailLink key={item.to} item={item} collapsed={collapsed} onNavigate={onNavigate} />
          ))}
        </nav>
        <nav className="mt-auto grid gap-0.5 pt-4" aria-label="Yönetim">
          {bottom.map((item) => (
            <RailLink key={item.to} item={item} collapsed={collapsed} onNavigate={onNavigate} />
          ))}
        </nav>
      </div>
      <div className={cn("border-(--sidebar-border) border-t", collapsed ? "p-2" : "p-2.5")}>
        <UserMenu collapsed={collapsed} />
      </div>
    </div>
  );
}
