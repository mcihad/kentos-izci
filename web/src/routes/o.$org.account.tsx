import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
import { toast } from "sonner";
import { Profile } from "@/components/account/profile";
import { Sessions } from "@/components/account/sessions";
import { Telegram } from "@/components/account/telegram";
import { Tokens } from "@/components/account/tokens";
import { TwoFactor } from "@/components/account/two-factor";
import { gutter, PageHeader } from "@/components/app/page";
import { TabbedCard, TabNav } from "@/components/common/tab-nav";
import { oidcErrors } from "@/lib/errors";
import { sessionQuery, useSession } from "@/lib/session";

const tabs = [
  { id: "profile", label: "Profil" },
  { id: "two-factor", label: "İki adımlı doğrulama" },
  { id: "sessions", label: "Oturumlar" },
  { id: "tokens", label: "Erişim anahtarları" },
  { id: "telegram", label: "Telegram" },
] as const;
type Tab = (typeof tabs)[number]["id"];

type AccountSearch = {
  tab?: Tab;
  // Result of linking an identity provider (set by the OIDC callback).
  oidc_linked?: string;
  oidc_error?: string;
};

export const Route = createFileRoute("/o/$org/account")({
  validateSearch: (s: Record<string, unknown>): AccountSearch => ({
    tab: typeof s.tab === "string" && tabs.some((t) => t.id === s.tab) ? (s.tab as Tab) : undefined,
    oidc_linked: typeof s.oidc_linked === "string" ? s.oidc_linked : undefined,
    oidc_error: typeof s.oidc_error === "string" ? s.oidc_error : undefined,
  }),
  component: AccountPage,
});

function AccountPage() {
  const search = Route.useSearch();
  const tab = search.tab ?? "profile";
  const user = useSession()?.user;
  const navigate = useNavigate();
  const qc = useQueryClient();
  // Report the outcome of linking an identity provider once, then drop
  // it from the address.
  useEffect(() => {
    if (!search.oidc_linked && !search.oidc_error) return;
    if (search.oidc_linked) {
      toast.success("Kimlik sağlayıcısı hesabınıza bağlandı");
      void qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
    } else if (search.oidc_error) {
      toast.error(oidcErrors[search.oidc_error] ?? "Kimlik sağlayıcısı bağlanamadı. Yeniden deneyin.");
    }
    void navigate({ to: ".", search: { tab: search.tab }, replace: true });
  }, [search.oidc_linked, search.oidc_error, search.tab, navigate, qc]);
  return (
    <div className="pb-12">
      <PageHeader title="Hesabım" description={user?.email} />
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
          {tab === "two-factor" ? <TwoFactor /> : null}
          {tab === "sessions" ? <Sessions /> : null}
          {tab === "tokens" ? <Tokens /> : null}
          {tab === "telegram" ? <Telegram /> : null}
        </TabbedCard>
      </div>
    </div>
  );
}
