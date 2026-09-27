import { Link } from "@tanstack/react-router";
import { BookOpenIcon, SearchIcon, SettingsIcon, ShieldCheckIcon } from "lucide-react";
import { Kbd } from "@/components/common/kbd";
import { useOrg } from "@/lib/org";
import { canAdmin, useSession } from "@/lib/session";
import { cn } from "@/lib/utils";
import { primaryNav } from "./nav";
import { OrgSwitcher } from "./org-switcher";
import { UserMenu } from "./user-menu";

const itemClass =
  "group relative flex h-8 items-center gap-2.5 rounded-md px-2.5 text-[13px] text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground";
// The active item carries a short teal blaze on its left edge, the same mark
// that flags levels on issue rows.
const activeClass =
  "bg-sidebar-accent font-medium text-foreground before:absolute before:inset-y-1.5 before:-left-2 before:w-[3px] before:rounded-full before:bg-primary";

export function SidebarContent({ onNavigate, onSearch }: { onNavigate?: () => void; onSearch: () => void }) {
  const org = useOrg();
  const session = useSession();
  return (
    <div className="flex h-full flex-col gap-4 px-3 py-3">
      <OrgSwitcher />
      <button
        type="button"
        onClick={onSearch}
        className="flex h-8 items-center gap-2 rounded-md border bg-background/60 px-2.5 text-[13px] text-muted-foreground transition-colors hover:text-foreground"
      >
        <SearchIcon className="size-3.5" />
        <span className="flex-1 text-left">Ara ya da git</span>
        <Kbd>⌘K</Kbd>
      </button>
      <nav className="grid gap-0.5" aria-label="Ana menü">
        {primaryNav.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            params={{ org: org.slug }}
            activeOptions={{ exact: item.exact ?? false, includeSearch: false }}
            className={itemClass}
            activeProps={{ className: activeClass }}
            onClick={onNavigate}
          >
            <item.icon className="size-4 shrink-0" />
            {item.label}
          </Link>
        ))}
      </nav>
      <div className="mt-auto grid gap-0.5">
        <Link
          to="/o/$org/docs"
          params={{ org: org.slug }}
          className={itemClass}
          activeProps={{ className: activeClass }}
          onClick={onNavigate}
        >
          <BookOpenIcon className="size-4 shrink-0" />
          Belgeler
        </Link>
        {canAdmin(org.role) ? (
          <Link
            to="/o/$org/settings"
            params={{ org: org.slug }}
            className={itemClass}
            activeProps={{ className: activeClass }}
            onClick={onNavigate}
          >
            <SettingsIcon className="size-4 shrink-0" />
            Organizasyon ayarları
          </Link>
        ) : null}
        {session?.user?.superadmin ? (
          <Link
            to="/o/$org/admin"
            params={{ org: org.slug }}
            className={itemClass}
            activeProps={{ className: activeClass }}
            onClick={onNavigate}
          >
            <ShieldCheckIcon className="size-4 shrink-0" />
            Sunucu yönetimi
          </Link>
        ) : null}
        <div className={cn("mt-2 border-t pt-2")}>
          <UserMenu />
        </div>
      </div>
    </div>
  );
}
