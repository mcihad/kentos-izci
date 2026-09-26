import { createFileRoute } from "@tanstack/react-router";
import { gutter, PageHeader } from "@/components/app/page";
import { TabNav } from "@/components/common/tab-nav";
import { Members } from "@/components/settings/members";
import { AuditLog, OrganizationGeneral, Usage } from "@/components/settings/organization";
import { useOrg } from "@/lib/org";

const tabs = [
  { id: "general", label: "Genel" },
  { id: "members", label: "Üyeler" },
  { id: "usage", label: "Kullanım" },
  { id: "audit", label: "Denetim kaydı" },
] as const;
type Tab = (typeof tabs)[number]["id"];

export const Route = createFileRoute("/o/$org/settings")({
  validateSearch: (s: Record<string, unknown>): { tab?: Tab } => ({
    tab: typeof s.tab === "string" && tabs.some((t) => t.id === s.tab) ? (s.tab as Tab) : undefined,
  }),
  component: SettingsPage,
});

function SettingsPage() {
  const org = useOrg();
  const tab = Route.useSearch().tab ?? "general";
  return (
    <div className="pb-12">
      <PageHeader title="Organizasyon ayarları" description={org.name}>
        <TabNav tabs={tabs} active={tab} from={Route.fullPath} label="Ayar bölümleri" />
      </PageHeader>
      <div className={gutter}>
        {tab === "general" ? <OrganizationGeneral /> : null}
        {tab === "members" ? <Members /> : null}
        {tab === "usage" ? <Usage /> : null}
        {tab === "audit" ? <AuditLog /> : null}
      </div>
    </div>
  );
}
