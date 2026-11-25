import { clone, create } from "@bufbuild/protobuf";
import { createConnectQueryKey, useInfiniteQuery, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { toast } from "sonner";
import { StackedBars } from "@/components/charts/stacked-bars";
import { Field } from "@/components/common/field";
import { RelativeTime } from "@/components/common/relative-time";
import { StatStrip } from "@/components/common/stat-strip";
import { EmptyState } from "@/components/common/states";
import { thClass, theadClass, trClass } from "@/components/common/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { AiService } from "@/gen/izci/api/v1/ai_pb";
import { Role } from "@/gen/izci/api/v1/common_pb";
import {
  type DailyUsage,
  OrganizationService,
  type OrganizationSettings,
  OrganizationSettingsSchema,
} from "@/gen/izci/api/v1/organization_pb";
import { errorMessage } from "@/lib/errors";
import { formatCompact, formatNumber } from "@/lib/format";
import { roleLabels } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { useSensitiveMutation } from "@/lib/reauth";
import { sessionQuery, useSession } from "@/lib/session";

const timezones = (() => {
  try {
    return Intl.supportedValuesOf("timeZone");
  } catch {
    return ["Europe/Istanbul", "UTC"];
  }
})();

export function OrganizationGeneral() {
  const org = useOrg();
  const qc = useQueryClient();
  const q = useQuery(OrganizationService.method.getOrganization, {});
  const o = q.data?.organization;
  if (!o) return <Skeleton className="my-6 h-56 w-full max-w-2xl" />;
  const onSaved = () => {
    void qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: OrganizationService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
    void qc.invalidateQueries({ queryKey: sessionQuery.queryKey });
  };
  return (
    <GeneralForm
      key={o.id}
      initial={{
        name: o.name,
        tz: o.settings?.timezone || "Europe/Istanbul",
        domains: o.settings?.autoJoinDomains.join(", ") ?? "",
        role: o.settings?.autoJoinRole || Role.MEMBER,
        requireTwoFactor: o.settings?.requireTwoFactor ?? false,
        aiEnabled: o.settings?.aiEnabled ?? false,
        tokenMaxDays: o.settings?.tokenMaxDays ?? 0,
        tokenIdleDays: o.settings?.tokenIdleDays ?? 0,
        settings: o.settings,
      }}
      plan={o.plan}
      onSaved={onSaved}
    />
  );
}

function GeneralForm({
  initial,
  plan,
  onSaved,
}: {
  initial: {
    name: string;
    tz: string;
    domains: string;
    role: Role;
    requireTwoFactor: boolean;
    aiEnabled: boolean;
    tokenMaxDays: number;
    tokenIdleDays: number;
    // Settings this form does not show are kept as they are.
    settings?: OrganizationSettings;
  };
  plan: string;
  onSaved: () => void;
}) {
  const org = useOrg();
  const myTwoFactor = useSession()?.user?.twoFactorEnabled ?? false;
  const [name, setName] = useState(initial.name);
  const [tz, setTz] = useState(initial.tz);
  const [domains, setDomains] = useState(initial.domains);
  const [role, setRole] = useState(initial.role);
  const [requireTwoFactor, setRequireTwoFactor] = useState(initial.requireTwoFactor);
  const [aiEnabled, setAiEnabled] = useState(initial.aiEnabled);
  const [tokenMaxDays, setTokenMaxDays] = useState(String(initial.tokenMaxDays));
  const [tokenIdleDays, setTokenIdleDays] = useState(String(initial.tokenIdleDays));
  // The choice matters only where the server runs AI analyses.
  const ai = useQuery(AiService.method.getAiStatus, {}, { staleTime: 60_000 }).data;
  const update = useSensitiveMutation(OrganizationService.method.updateOrganization, {
    onSuccess: () => {
      toast.success("Organizasyon ayarları kaydedildi");
      onSaved();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <form
      className="grid max-w-2xl grid-cols-1 *:min-w-0 gap-5 py-6"
      onSubmit={(e) => {
        e.preventDefault();
        const settings = clone(
          OrganizationSettingsSchema,
          initial.settings ?? create(OrganizationSettingsSchema),
        );
        settings.timezone = tz;
        settings.autoJoinDomains = domains
          .split(",")
          .map((d) => d.trim().toLowerCase())
          .filter(Boolean);
        settings.autoJoinRole = role;
        settings.requireTwoFactor = requireTwoFactor;
        settings.aiEnabled = aiEnabled;
        settings.tokenMaxDays = Math.max(0, Math.trunc(Number(tokenMaxDays) || 0));
        settings.tokenIdleDays = Math.max(0, Math.trunc(Number(tokenIdleDays) || 0));
        update.mutate({ name, settings });
      }}
    >
      <Field label="Organizasyon adı" htmlFor="org-name">
        <Input id="org-name" required value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label="Kısa ad" hint="Adreslerde görünür; değiştirilemez.">
        <Input value={org.slug} disabled className="font-mono text-sm" />
      </Field>
      <Field
        label="Saat dilimi"
        htmlFor="org-tz"
        hint="Günlük grafikler ve bildirimler bu saate göre hesaplanır."
      >
        <Select value={tz} onValueChange={setTz}>
          <SelectTrigger id="org-tz" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent className="max-h-72">
            {timezones.map((z) => (
              <SelectItem key={z} value={z}>
                {z}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Field
        label="Otomatik katılım alan adları"
        htmlFor="org-domains"
        hint="Bu alan adlarından kimlik sağlayıcısıyla giriş yapanlar organizasyona kendiliğinden katılır. Virgülle ayırın."
      >
        <Input
          id="org-domains"
          value={domains}
          onChange={(e) => setDomains(e.target.value)}
          placeholder="kentos.com.tr"
        />
      </Field>
      <Field label="Otomatik katılanların rolü" htmlFor="org-role">
        <Select value={String(role)} onValueChange={(v) => setRole(Number(v) as Role)}>
          <SelectTrigger id="org-role" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {[Role.VIEWER, Role.MEMBER, Role.ADMIN].map((r) => (
              <SelectItem key={r} value={String(r)}>
                {roleLabels[r]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <div className="flex items-start gap-3">
        <Switch
          id="org-2fa"
          checked={requireTwoFactor}
          disabled={!requireTwoFactor && !myTwoFactor}
          onCheckedChange={setRequireTwoFactor}
          className="mt-0.5"
        />
        <div className="grid gap-0.5">
          <Label htmlFor="org-2fa" className="text-md">
            İki adımlı doğrulama zorunlu
          </Label>
          <p className="text-sm text-muted-foreground leading-relaxed">
            Açmamış üyeler, açana kadar organizasyona erişemez; girişte kurulum ekranını görür. Erişim
            anahtarları da sahibinin iki adımlı doğrulamasına bağlıdır.
            {!requireTwoFactor && !myTwoFactor ? (
              <>
                {" "}
                Zorunlu tutmak için önce{" "}
                <Link
                  to="/o/$org/account"
                  params={{ org: org.slug }}
                  search={{ tab: "two-factor" }}
                  className="font-medium text-foreground underline-offset-4 hover:underline"
                >
                  kendi hesabınızda açın
                </Link>
                .
              </>
            ) : null}
          </p>
        </div>
      </div>
      {ai?.enabled || initial.aiEnabled ? (
        <div className="flex items-start gap-3">
          <Switch id="org-ai" checked={aiEnabled} onCheckedChange={setAiEnabled} className="mt-0.5" />
          <div className="grid gap-0.5">
            <Label htmlFor="org-ai" className="text-md">
              Yapay zekâ ile analize izin ver
            </Label>
            <p className="text-sm text-muted-foreground leading-relaxed">
              Açıkken üyeler hataları sunucunun kullandığı yapay zekâ sağlayıcısına analiz ettirebilir;
              projeler yeni hatalar için otomatik analiz de açabilir. Sağlayıcıya hata kaydı gider
              (kullanıcının e-posta ve IP adresi, çerezler ve yetki başlıkları çıkarılır), bağlı depo varsa
              kod da gider. Sağlayıcı yurt dışında olabilir; KVKK açısından değerlendirip açın. Kapalıyken
              analiz başlatılamaz.
              {ai?.defaultModel ? (
                <>
                  {" "}
                  Sunucunun varsayılan modeli: <code className="font-mono text-xs">{ai.defaultModel}</code>.
                </>
              ) : null}
            </p>
          </div>
        </div>
      ) : null}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Erişim anahtarlarının en uzun ömrü (gün)"
          htmlFor="org-token-max"
          hint="Anahtarlar oluşturulduktan bu kadar gün sonra bu organizasyonda çalışmaz; buraya bağlı anahtarlar daha uzun süreli oluşturulamaz. 0: sınır yok."
        >
          <Input
            id="org-token-max"
            type="number"
            min={0}
            max={3650}
            value={tokenMaxDays}
            onChange={(e) => setTokenMaxDays(e.target.value)}
            className="w-32"
          />
        </Field>
        <Field
          label="Kullanılmayan anahtarların iptali (gün)"
          htmlFor="org-token-idle"
          hint="Bu kadar gün kullanılmayan bir anahtar, organizasyona eriştiği ilk istekte iptal edilir. 0: hiçbir zaman."
        >
          <Input
            id="org-token-idle"
            type="number"
            min={0}
            max={3650}
            value={tokenIdleDays}
            onChange={(e) => setTokenIdleDays(e.target.value)}
            className="w-32"
          />
        </Field>
      </div>
      <p className="text-sm text-muted-foreground">
        Plan: <span className="text-foreground">{plan || "varsayılan"}</span>
      </p>
      <div>
        <Button type="submit" disabled={update.isPending}>
          Kaydet
        </Button>
      </div>
    </form>
  );
}

export function Usage() {
  const q = useQuery(OrganizationService.method.getUsage, {});
  const u = q.data;
  if (!u) return <Skeleton className="my-6 h-72 w-full max-w-4xl" />;
  const limit = u.limits?.monthlyEvents ?? 0n;
  const used = limit > 0n ? Number(u.eventsAccepted) / Number(limit) : 0;
  // Every day of the month up to today, including days without events.
  const [year, month] = u.month.split("-").map(Number);
  const byDate = new Map(u.days.map((d) => [d.date, d]));
  const today = new Date();
  const lastDay =
    year === today.getFullYear() && month === today.getMonth() + 1
      ? today.getDate()
      : new Date(year ?? 0, month ?? 0, 0).getDate();
  const dates = Array.from({ length: lastDay }, (_, i) => `${u.month}-${String(i + 1).padStart(2, "0")}`);
  const points = (pick: (d: DailyUsage) => bigint) =>
    dates.map((date) => {
      const d = byDate.get(date);
      return { t: new Date(`${date}T00:00:00`), v: d ? Number(pick(d)) : 0 };
    });
  const monthName = new Intl.DateTimeFormat("tr-TR", { month: "long", year: "numeric" }).format(
    new Date(year ?? 2000, (month ?? 1) - 1, 1),
  );
  return (
    <div className="grid max-w-4xl grid-cols-1 *:min-w-0 gap-8 py-6">
      <StatStrip
        stats={[
          {
            label: "Kabul edilen olay",
            value: formatCompact(u.eventsAccepted),
            note:
              limit > 0n
                ? `${monthName}, kotanın %${Math.round(used * 100)} kadarı`
                : `${monthName}, kota sınırı yok`,
          },
          { label: "Sınıra takılan", value: formatCompact(u.eventsRateLimited) },
          { label: "Filtrelenen", value: formatCompact(u.eventsFiltered), note: "kayıt dışı ortamlar" },
          { label: "Yapay zekâ analizi", value: formatNumber(u.aiRuns) },
        ]}
      />
      <section>
        <h2 className="mb-3 font-semibold text-lg">Günlük dağılım</h2>
        <StackedBars
          height={180}
          step={86_400_000}
          layers={[
            {
              key: "accepted",
              label: "Kabul edilen",
              color: "var(--primary)",
              points: points((d) => d.accepted),
            },
            {
              key: "limited",
              label: "Reddedilen",
              color: "var(--level-warning)",
              points: points((d) => d.rateLimited),
            },
            {
              key: "filtered",
              label: "Filtrelenen",
              color: "var(--level-debug)",
              points: points((d) => d.filtered),
            },
          ]}
        />
      </section>
      {u.limits ? (
        <section>
          <h2 className="mb-3 font-semibold text-lg">Plan sınırları</h2>
          <dl className="grid max-w-md grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-1.5 text-sm">
            <dt className="text-muted-foreground">Aylık olay</dt>
            <dd className="tabular">
              {u.limits.monthlyEvents ? formatNumber(u.limits.monthlyEvents) : "Sınırsız"}
            </dd>
            <dt className="text-muted-foreground">Saklama süresi</dt>
            <dd className="tabular">
              {u.limits.retentionDays ? `${u.limits.retentionDays} gün` : "Sunucu varsayılanı"}
            </dd>
            <dt className="text-muted-foreground">Proje</dt>
            <dd className="tabular">{u.limits.maxProjects || "Sınırsız"}</dd>
            <dt className="text-muted-foreground">Üye</dt>
            <dd className="tabular">{u.limits.maxMembers || "Sınırsız"}</dd>
            <dt className="text-muted-foreground">Aylık yapay zekâ analizi</dt>
            <dd className="tabular">{u.limits.monthlyAiRuns || "Sınırsız"}</dd>
          </dl>
        </section>
      ) : null}
    </div>
  );
}

const actionLabels: Record<string, string> = {
  "alert_rule.create": "Uyarı kuralı oluşturdu",
  "alert_rule.delete": "Uyarı kuralını sildi",
  "alert_rule.update": "Uyarı kuralını değiştirdi",
  "api_key.create": "API anahtarı oluşturdu",
  "api_key.revoke": "API anahtarını iptal etti",
  "api_key.update": "API anahtarını değiştirdi",
  "channel.create": "Bildirim kanalı ekledi",
  "channel.delete": "Bildirim kanalını kaldırdı",
  "channel.update": "Bildirim kanalını değiştirdi",
  "customer.update": "Müşteri bilgilerini değiştirdi",
  "instance.setup": "İzci kurulumunu tamamladı",
  "invitation.create": "Üye davet etti",
  "invitation.revoke": "Daveti geri aldı",
  "member.join": "Organizasyona katıldı",
  "member.remove": "Üyeyi çıkardı",
  "member.update": "Üyenin rolünü değiştirdi",
  "monitor.create": "Zamanlanmış görev izleyicisi oluşturdu",
  "monitor.delete": "Zamanlanmış görev izleyicisini sildi",
  "monitor.update": "Zamanlanmış görev izleyicisini değiştirdi",
  "organization.plan_update": "Planı değiştirdi",
  "organization.update": "Organizasyon ayarlarını değiştirdi",
  "project.create": "Proje oluşturdu",
  "project.delete": "Projeyi sildi",
  "project.link_repository": "Depo bağladı",
  "project.unlink_repository": "Depo bağlantısını kaldırdı",
  "project.update": "Proje ayarlarını değiştirdi",
  "symbols.bundle.delete": "Kaynak eşleme paketini sildi",
  "symbols.debug_file.delete": "Sembol dosyasını sildi",
  "user.two_factor.disable": "İki adımlı doğrulamayı kapattı",
  "user.two_factor.enable": "İki adımlı doğrulamayı açtı",
  "user.two_factor.recovery_codes": "Yeni kurtarma kodları oluşturdu",
};

export function AuditLog() {
  const q = useInfiniteQuery(
    OrganizationService.method.listAuditLog,
    { pageSize: 50, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const rows = q.data?.pages.flatMap((p) => p.entries) ?? [];
  if (q.isPending) return <Skeleton className="my-6 h-72 w-full max-w-4xl" />;
  if (rows.length === 0)
    return (
      <EmptyState title="Henüz kayıt yok" className="my-6 max-w-4xl rounded-lg border py-10">
        Üyeler, projeler ve anahtarlar değiştikçe kimin neyi ne zaman yaptığı burada görünür.
      </EmptyState>
    );
  return (
    <div className="max-w-4xl py-6">
      <ul className="divide-y rounded-lg border md:hidden">
        {rows.map((e) => (
          <li key={e.id} className="px-4 py-3">
            <div className="flex items-baseline justify-between gap-3">
              <p className="font-medium text-md">{actionLabels[e.action] ?? e.action}</p>
              <span className="shrink-0 text-sm text-muted-foreground">
                <RelativeTime value={e.createdAt} />
              </span>
            </div>
            <p className="mt-0.5 truncate text-sm text-muted-foreground">
              {e.actor?.name || e.actor?.email || "Sistem"}
              {e.ipAddress ? `, ${e.ipAddress}` : ""}
            </p>
            {e.targetName || e.targetId ? (
              <p className="mt-1 truncate font-mono text-sm">{e.targetName || e.targetId}</p>
            ) : null}
          </li>
        ))}
      </ul>
      <div className="hidden overflow-x-auto rounded-lg border md:block">
        <table className="w-full text-sm">
          <thead className={theadClass}>
            <tr>
              <th className={thClass}>Zaman</th>
              <th className={thClass}>Kişi</th>
              <th className={thClass}>İşlem</th>
              <th className={thClass}>Hedef</th>
              <th className={thClass}>IP</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {rows.map((e) => (
              <tr key={e.id} className={trClass}>
                <td className="whitespace-nowrap px-3.5 py-2 text-muted-foreground">
                  <RelativeTime value={e.createdAt} />
                </td>
                <td className="max-w-44 truncate px-3.5 py-2">
                  {e.actor?.name || e.actor?.email || "Sistem"}
                </td>
                <td className="px-3.5 py-2">{actionLabels[e.action] ?? e.action}</td>
                <td className="max-w-52 truncate px-3.5 py-2 font-mono text-sm text-muted-foreground">
                  {e.targetName || e.targetId}
                </td>
                <td className="px-3.5 py-2 font-mono text-sm text-muted-foreground">{e.ipAddress || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {q.hasNextPage ? (
        <Button variant="ghost" size="sm" className="mt-2" onClick={() => void q.fetchNextPage()}>
          Daha eski kayıtlar
        </Button>
      ) : null}
    </div>
  );
}
