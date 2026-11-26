import { createFileRoute } from "@tanstack/react-router";
import { SettingsIcon } from "lucide-react";
import { gutter, Page } from "@/components/app/page";
import { TabbedCard, TabNav } from "@/components/common/tab-nav";
import { Integrations } from "@/components/settings/integrations";
import { Members } from "@/components/settings/members";
import { AuditLog, OrganizationGeneral, Usage } from "@/components/settings/organization";
import { PrivacyRequests } from "@/components/settings/privacy";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";

const tabs = [
  { id: "general", label: "Genel" },
  { id: "members", label: "Üyeler" },
  { id: "integrations", label: "Entegrasyonlar" },
  { id: "usage", label: "Kullanım" },
  { id: "privacy", label: "Kişisel veriler" },
  { id: "audit", label: "Denetim kaydı" },
] as const;
type Tab = (typeof tabs)[number]["id"];

export const Route = createFileRoute("/o/$org/settings")({
  validateSearch: (s: Record<string, unknown>): { tab?: Tab; github?: string } => ({
    tab: typeof s.tab === "string" && tabs.some((t) => t.id === s.tab) ? (s.tab as Tab) : undefined,
    github: typeof s.github === "string" ? s.github : undefined,
  }),
  component: SettingsPage,
});

function SettingsPage() {
  const org = useOrg();
  const search = Route.useSearch();
  const tab = search.tab ?? "general";
  // Data subject requests are for administrators.
  const visible = tabs.filter((t) => t.id !== "privacy" || canAdmin(org.role));
  return (
    <Page icon={SettingsIcon} title="Organizasyon ayarları" description={org.name}>
      <div className={gutter}>
        <TabbedCard
          tabs={
            <TabNav
              tabs={visible}
              active={tab}
              from={Route.fullPath}
              label="Ayar bölümleri"
              className="px-2 sm:px-3"
            />
          }
        >
          {tab === "general" ? <OrganizationGeneral /> : null}
          {tab === "members" ? <Members /> : null}
          {tab === "integrations" ? <Integrations result={search.github} /> : null}
          {tab === "usage" ? <Usage /> : null}
          {tab === "privacy" && canAdmin(org.role) ? <PrivacyRequests /> : null}
          {tab === "audit" ? <AuditLog /> : null}
        </TabbedCard>
      </div>
    </Page>
  );
}
