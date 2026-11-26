import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarHeader,
  SidebarItem,
  SidebarSearch,
} from "@kentos/components/layout/sidebar";
import { Link } from "@tanstack/react-router";
import { BookOpenIcon, type LucideIcon, SettingsIcon, ShieldCheckIcon } from "lucide-react";
import { useOrg } from "@/lib/org";
import { canAdmin, useSession } from "@/lib/session";
import { type NavItem, primaryNav } from "./nav";
import { OrgSwitcher } from "./org-switcher";
import { UserMenu } from "./user-menu";

type Item = {
  to: "/o/$org/docs" | "/o/$org/settings" | "/o/$org/admin" | NavItem["to"];
  label: string;
  icon: LucideIcon;
  exact?: boolean;
  keywords?: string[];
};

const groups = ["İzleme", "Müşteri", "Yapılandırma"] as const;

// The router link marks the current page with aria-current="page", which the
// kit's item styles as active.
function NavLink({ item }: { item: Item }) {
  const org = useOrg();
  return (
    <SidebarItem icon={item.icon} label={item.label} keywords={item.keywords} asChild>
      <Link
        to={item.to}
        params={{ org: org.slug }}
        activeOptions={{ exact: item.exact ?? false, includeSearch: false }}
      />
    </SidebarItem>
  );
}

/** The organization's sidebar: switcher, search, grouped pages, management and account. */
export function AppSidebar() {
  const org = useOrg();
  const session = useSession();
  const manage: Item[] = [
    { to: "/o/$org/docs", label: "Belgeler", icon: BookOpenIcon, keywords: ["yardım", "SDK", "kurulum"] },
  ];
  if (canAdmin(org.role))
    manage.push({
      to: "/o/$org/settings",
      label: "Organizasyon ayarları",
      icon: SettingsIcon,
      keywords: ["üyeler", "davet", "güvenlik", "saklama", "gizlilik", "kayıt defteri"],
    });
  if (session?.user?.superadmin)
    manage.push({
      to: "/o/$org/admin",
      label: "Sunucu yönetimi",
      icon: ShieldCheckIcon,
      keywords: ["admin", "kullanıcılar", "organizasyonlar"],
    });

  return (
    <Sidebar label="Menü">
      <SidebarHeader>
        <OrgSwitcher />
      </SidebarHeader>
      <SidebarSearch />
      <SidebarContent>
        <SidebarGroup>
          {primaryNav
            .filter((item) => !item.group)
            .map((item) => (
              <NavLink key={item.to} item={item} />
            ))}
        </SidebarGroup>
        {groups.map((group) => (
          <SidebarGroup key={group} label={group}>
            {primaryNav
              .filter((item) => item.group === group)
              .map((item) => (
                <NavLink key={item.to} item={item} />
              ))}
          </SidebarGroup>
        ))}
        <SidebarGroup label="Yönetim" className="mt-auto">
          {manage.map((item) => (
            <NavLink key={item.to} item={item} />
          ))}
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <UserMenu />
      </SidebarFooter>
    </Sidebar>
  );
}
