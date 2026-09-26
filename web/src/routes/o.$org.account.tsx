import { createFileRoute } from "@tanstack/react-router";
import { Profile } from "@/components/account/profile";
import { Sessions } from "@/components/account/sessions";
import { Telegram } from "@/components/account/telegram";
import { Tokens } from "@/components/account/tokens";
import { gutter, PageHeader } from "@/components/app/page";
import { TabNav } from "@/components/common/tab-nav";
import { useSession } from "@/lib/session";

const tabs = [
  { id: "profile", label: "Profil" },
  { id: "sessions", label: "Oturumlar" },
  { id: "tokens", label: "Erişim anahtarları" },
  { id: "telegram", label: "Telegram" },
] as const;
type Tab = (typeof tabs)[number]["id"];

export const Route = createFileRoute("/o/$org/account")({
  validateSearch: (s: Record<string, unknown>): { tab?: Tab } => ({
    tab: typeof s.tab === "string" && tabs.some((t) => t.id === s.tab) ? (s.tab as Tab) : undefined,
  }),
  component: AccountPage,
});

function AccountPage() {
  const tab = Route.useSearch().tab ?? "profile";
  const user = useSession()?.user;
  return (
    <div className="pb-12">
      <PageHeader title="Hesabım" description={user?.email}>
        <TabNav tabs={tabs} active={tab} from={Route.fullPath} label="Hesap bölümleri" />
      </PageHeader>
      <div className={gutter}>
        {tab === "profile" ? <Profile /> : null}
        {tab === "sessions" ? <Sessions /> : null}
        {tab === "tokens" ? <Tokens /> : null}
        {tab === "telegram" ? <Telegram /> : null}
      </div>
    </div>
  );
}
