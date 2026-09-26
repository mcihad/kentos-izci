import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { CircleAlertIcon, ExternalLinkIcon, KeyRoundIcon, PlugIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { GitHubMark } from "@/components/brand/github-mark";
import { Field, FormError } from "@/components/common/field";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { UserAvatar } from "@/components/common/user-avatar";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { type Integration, IntegrationAuth, IntegrationService } from "@/gen/izci/api/v1/integration_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { errorField, errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";
import { cn } from "@/lib/utils";

/** Messages for the result GitHub's installation callback reports. */
const installResults: Record<string, { ok: boolean; text: string }> = {
  connected: { ok: true, text: "GitHub App bağlandı." },
  requested: {
    ok: true,
    text: "Kurulum isteği gönderildi; GitHub organizasyon sahibi onaylayınca yeniden bağlayın.",
  },
  denied: {
    ok: false,
    text: "Kurulum doğrulanamadı: GitHub'da giriş yaptığınız hesap bu kuruluma erişemiyor.",
  },
  taken: { ok: false, text: "Bu GitHub App kurulumu başka bir organizasyona bağlı." },
  session: { ok: false, text: "Kurulumu başlatan oturum bulunamadı. Aynı tarayıcıdan yeniden deneyin." },
  forbidden: { ok: false, text: "GitHub bağlamak için organizasyonda yönetici olmalısınız." },
  oauth: {
    ok: false,
    text: "GitHub kullanıcı doğrulaması gelmedi. App ayarlarında kurulum sırasında yetkilendirmeyi açın.",
  },
  invalid: { ok: false, text: "Kurulum bağlantısının süresi dolmuş. Yeniden başlatın." },
  error: { ok: false, text: "Kurulum kaydedilemedi. Yeniden deneyin." },
};

function useInvalidateIntegrations() {
  const org = useOrg();
  const qc = useQueryClient();
  return () =>
    Promise.all(
      [IntegrationService, ProjectService].map((schema) =>
        qc.invalidateQueries({
          queryKey: createConnectQueryKey({ schema, transport: org.transport, cardinality: undefined }),
        }),
      ),
    );
}

/** GitHub connections of the organization. */
export function Integrations({ result }: { result?: string }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const navigate = useNavigate();
  const list = useQuery(IntegrationService.method.listIntegrations, {});
  const status = useQuery(IntegrationService.method.getGitHubStatus, {});
  const [connecting, setConnecting] = useState(false);
  const invalidate = useInvalidateIntegrations();
  const install = useMutation(IntegrationService.method.startGitHubAppInstallation, {
    onSuccess: (res) => {
      window.location.assign(res.installUrl);
    },
    onError: (err) => toast.error(errorMessage(err)),
  });

  // The installation callback redirects here with ?github=<result>.
  useEffect(() => {
    if (!result) return;
    const r = installResults[result] ?? installResults.error;
    if (r) (r.ok ? toast.success : toast.error)(r.text, { duration: 8000 });
    void invalidate();
    void navigate({
      to: ".",
      search: (p: Record<string, unknown>) => ({ ...p, github: undefined }),
      replace: true,
    });
  }, [result, navigate, invalidate]);

  const items = list.data?.integrations ?? [];
  return (
    <div className="grid max-w-3xl grid-cols-1 gap-6 py-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="max-w-xl">
          <h2 className="font-semibold text-[15px]">GitHub</h2>
          <p className="mt-0.5 text-[13px] text-muted-foreground leading-relaxed">
            İzci, bağlı GitHub hesapları üzerinden depoları okur; yapay zekânın önerdiği düzeltmeleri dal ve
            çekme isteği olarak gönderir. Erişim anahtarları şifrelenerek saklanır.
          </p>
        </div>
        {admin ? (
          <div className="flex flex-wrap gap-2">
            {status.data?.appConfigured ? (
              <Button
                variant="outline"
                size="sm"
                disabled={install.isPending}
                onClick={() => install.mutate({})}
              >
                <GitHubMark className="size-4" />
                GitHub App'i kur
              </Button>
            ) : null}
            <Button size="sm" onClick={() => setConnecting(true)}>
              <KeyRoundIcon className="size-4" />
              Erişim anahtarı ekle
            </Button>
          </div>
        ) : null}
      </div>

      {list.isPending ? (
        <div className="grid gap-2">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={PlugIcon}
          title="Bağlı GitHub hesabı yok"
          className="rounded-lg border border-dashed"
        >
          Depoları yapay zekâya açmak ve düzeltmeleri çekme isteği olarak göndermek için bir hesap bağlayın.
          {status.data?.appConfigured
            ? ""
            : " Sunucuda GitHub App yapılandırılmadığı için erişim anahtarı kullanılır."}
        </EmptyState>
      ) : (
        <ul className="divide-y overflow-hidden rounded-lg border">
          {items.map((i) => (
            <IntegrationRow key={i.id} integration={i} admin={admin} onChanged={() => void invalidate()} />
          ))}
        </ul>
      )}

      <ConnectTokenDialog open={connecting} onOpenChange={setConnecting} onDone={() => void invalidate()} />
    </div>
  );
}

function IntegrationRow({
  integration: i,
  admin,
  onChanged,
}: {
  integration: Integration;
  admin: boolean;
  onChanged: () => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const remove = useMutation(IntegrationService.method.deleteIntegration, {
    onSuccess: () => {
      toast.success("GitHub bağlantısı kaldırıldı");
      setConfirm(false);
      onChanged();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const app = i.auth === IntegrationAuth.APP;
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
      <UserAvatar name={i.accountLogin || i.name} src={i.accountAvatarUrl} className="size-8" />
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-2 text-[13.5px]">
          <span className="font-medium">{i.accountLogin || i.name}</span>
          <span className="rounded-full border px-1.5 text-[11px] text-muted-foreground">
            {app ? "GitHub App" : "Erişim anahtarı"}
          </span>
        </p>
        <p className="mt-0.5 truncate text-[12px] text-muted-foreground">
          {i.name}
          {i.createdBy ? ` · ${i.createdBy.name || i.createdBy.email}` : ""} ·{" "}
          <RelativeTime value={i.createdAt} /> bağlandı
        </p>
        {!i.healthy ? (
          <p className="mt-1 flex items-center gap-1.5 text-[12px] text-level-error">
            <CircleAlertIcon className="size-3.5" />
            GitHub kimlik bilgilerini reddetti{i.statusMessage ? `: ${i.statusMessage}` : ""}
          </p>
        ) : null}
      </div>
      {admin ? (
        <div className="flex items-center gap-2">
          {app ? (
            <Button variant="ghost" size="sm" asChild>
              <a
                href={`https://github.com/settings/installations/${i.installationId}`}
                target="_blank"
                rel="noreferrer"
              >
                Ayarlar
                <ExternalLinkIcon className="size-3.5" />
              </a>
            </Button>
          ) : null}
          <Button variant="ghost" size="sm" className="text-destructive" onClick={() => setConfirm(true)}>
            Kaldır
          </Button>
        </div>
      ) : null}
      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Bağlantı kaldırılsın mı?</DialogTitle>
            <DialogDescription>
              {i.accountLogin} bağlantısını kullanan projeler kaynak bağlantılarını korur, ancak yapay zekâ bu
              depoları okuyamaz ve düzeltme gönderemez.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirm(false)}>
              Vazgeç
            </Button>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => remove.mutate({ id: i.id })}
            >
              Bağlantıyı kaldır
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </li>
  );
}

function ConnectTokenDialog({
  open,
  onOpenChange,
  onDone,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDone: () => void;
}) {
  const [name, setName] = useState("");
  const [token, setToken] = useState("");
  const connect = useMutation(IntegrationService.method.connectGitHubToken, {
    onSuccess: (res) => {
      toast.success(`${res.integration?.accountLogin ?? "GitHub"} hesabı bağlandı`);
      setName("");
      setToken("");
      onOpenChange(false);
      onDone();
    },
  });
  const tokenError =
    connect.error && errorField(connect.error) === "token" ? errorMessage(connect.error) : undefined;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            connect.mutate({ name: name.trim(), token: token.trim() });
          }}
        >
          <DialogHeader>
            <DialogTitle>GitHub erişim anahtarı ekle</DialogTitle>
            <DialogDescription>
              GitHub'da{" "}
              <a
                href="https://github.com/settings/personal-access-tokens/new"
                target="_blank"
                rel="noreferrer"
                className="text-primary hover:underline"
              >
                ince ayarlı bir erişim anahtarı
              </a>{" "}
              oluşturun. İzci'nin kullanacağı depoları seçip şu izinleri verin: Contents ve Pull requests için
              okuma ve yazma, Metadata için okuma.
            </DialogDescription>
          </DialogHeader>
          <Field label="Ad" htmlFor="gh-name" hint="Bağlantıyı listede tanımak için, örneğin “KentOS botu”.">
            <Input
              id="gh-name"
              required
              maxLength={100}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <Field label="Erişim anahtarı" htmlFor="gh-token" error={tokenError}>
            <Input
              id="gh-token"
              type="password"
              required
              autoComplete="off"
              spellCheck={false}
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="github_pat_…"
              className={cn("font-mono text-[13px]")}
            />
          </Field>
          <FormError message={connect.error && !tokenError ? errorMessage(connect.error) : undefined} />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Vazgeç
            </Button>
            <Button type="submit" disabled={connect.isPending}>
              Doğrula ve bağla
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
