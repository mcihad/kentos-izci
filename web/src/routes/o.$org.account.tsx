import { createFileRoute } from "@tanstack/react-router";
import { UserIcon } from "lucide-react";
import { NotificationPreferences } from "@/components/account/notifications";
import { Profile } from "@/components/account/profile";
import { Sessions } from "@/components/account/sessions";
import { Telegram } from "@/components/account/telegram";
import { Tokens } from "@/components/account/tokens";
import { TwoFactor } from "@/components/account/two-factor";
import { gutter, Page } from "@/components/app/page";
import { TabbedCard, TabNav } from "@/components/common/tab-nav";
import { useSession } from "@/lib/session";

const tabs = [
  { id: "profile", label: "Profil" },
  { id: "notifications", label: "Bildirimler" },
  { id: "two-factor", label: "İki adımlı doğrulama" },
  { id: "sessions", label: "Oturumlar" },
  { id: "tokens", label: "Erişim anahtarları" },
  { id: "telegram", label: "Telegram" },
] as const;
type Tab = (typeof tabs)[number]["id"];

type AccountSearch = { tab?: Tab };

export const Route = createFileRoute("/o/$org/account")({
  validateSearch: (s: Record<string, unknown>): AccountSearch => ({
    tab: typeof s.tab === "string" && tabs.some((t) => t.id === s.tab) ? (s.tab as Tab) : undefined,
  }),
  component: AccountPage,
});

function AccountPage() {
  const search = Route.useSearch();
  const tab = search.tab ?? "profile";
  const user = useSession()?.user;
  return (
    <Page icon={UserIcon} title="Hesabım" description={user?.email}>
      <div className={gutter}>
        <TabbedCard
          tabs={
            <TabNav
              tabs={tabs}
              active={tab}
              from={Route.fullPath}
              label="Hesap bölümleri"
              className="px-2 sm:px-3"
            />
          }
        >
          {tab === "profile" ? <Profile /> : null}
          {tab === "notifications" ? <NotificationPreferences /> : null}
          {tab === "two-factor" ? <TwoFactor /> : null}
          {tab === "sessions" ? <Sessions /> : null}
          {tab === "tokens" ? <Tokens /> : null}
          {tab === "telegram" ? <Telegram /> : null}
        </TabbedCard>
      </div>
    </Page>
  );
}
