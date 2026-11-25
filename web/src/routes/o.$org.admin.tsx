import { create } from "@bufbuild/protobuf";
import { createConnectQueryKey, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { ShieldAlertIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { gutter, PageHeader } from "@/components/app/page";
import { Field } from "@/components/common/field";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import { TabbedCard, TabNav } from "@/components/common/tab-nav";
import { thClass, theadClass, trClass } from "@/components/common/table";
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
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { type AdminOrganization, AdminService, type AdminUser } from "@/gen/izci/api/v1/admin_pb";
import { OrganizationLimitsSchema } from "@/gen/izci/api/v1/organization_pb";
import { baseTransport } from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatCompact, formatDateTime, formatNumber } from "@/lib/format";
import { useSession } from "@/lib/session";
import { cn } from "@/lib/utils";

const tabs = [
  { id: "instance", label: "Sunucu" },
  { id: "organizations", label: "Organizasyonlar" },
  { id: "users", label: "Kullanıcılar" },
] as const;
type Tab = (typeof tabs)[number]["id"];

export const Route = createFileRoute("/o/$org/admin")({
  validateSearch: (s: Record<string, unknown>): { tab?: Tab } => ({
    tab: typeof s.tab === "string" && tabs.some((t) => t.id === s.tab) ? (s.tab as Tab) : undefined,
  }),
  component: AdminPage,
});

function AdminPage() {
  const tab = Route.useSearch().tab ?? "instance";
  const { org } = Route.useParams();
  const session = useSession();
  if (!session?.user?.superadmin) {
    return <EmptyState title="Bu sayfa sunucu yöneticileri içindir" className="py-24" />;
  }
  if (session.superadminNeedsTwoFactor) {
    return (
      <EmptyState
        icon={ShieldAlertIcon}
        title="Sunucu yönetimi için iki adımlı doğrulama gerekiyor"
        className="py-24"
        action={
          <Button asChild>
            <Link to="/o/$org/account" params={{ org }} search={{ tab: "two-factor" }}>
              İki adımlı doğrulamayı aç
            </Link>
          </Button>
        }
      >
        Bu sunucu, yöneticilerinden iki adımlı doğrulama istiyor. Hesabınızda açtığınızda sunucu yönetimine
        erişirsiniz.
      </EmptyState>
    );
  }
  return (
    <div className="pb-12">
      <PageHeader
        title="Sunucu yönetimi"
        description="Tüm organizasyonları ve kullanıcıları kapsayan ayarlar"
      />
      <div className={gutter}>
        <TabbedCard
          tabs={
            <TabNav
              tabs={tabs}
              active={tab}
              from={Route.fullPath}
              label="Yönetim bölümleri"
              className="px-2 sm:px-3"
            />
          }
        >
          {tab === "instance" ? <Instance /> : null}
          {tab === "organizations" ? <Organizations /> : null}
          {tab === "users" ? <Users /> : null}
        </TabbedCard>
      </div>
    </div>
  );
}

function Instance() {
  const q = useQuery(AdminService.method.getInstanceInfo, {}, { transport: baseTransport });
  const i = q.data;
  if (!i) return <Skeleton className="my-6 h-64 w-full max-w-2xl" />;
  const feature = (on: boolean) => (on ? "Açık" : "Kapalı");
  return (
    <dl className="grid max-w-2xl grid-cols-[auto_minmax(0,1fr)] gap-x-8 gap-y-2 py-6 text-sm">
      <dt className="text-muted-foreground">Sürüm</dt>
      <dd className="font-mono text-sm">
        {i.version} {i.commit ? `(${i.commit.slice(0, 12)})` : ""}
      </dd>
      <dt className="text-muted-foreground">Go</dt>
      <dd className="font-mono text-sm">{i.goVersion}</dd>
      <dt className="text-muted-foreground">PostgreSQL</dt>
      <dd className="font-mono text-sm">{i.postgresVersion}</dd>
      <dt className="text-muted-foreground">Genel adres</dt>
      <dd className="font-mono text-sm">{i.publicUrl}</dd>
      <dt className="text-muted-foreground">Çalışma başlangıcı</dt>
      <dd>{formatDateTime(i.startedAt)}</dd>
      <dt className="text-muted-foreground">Organizasyon</dt>
      <dd className="tabular">{formatNumber(i.organizations)}</dd>
      <dt className="text-muted-foreground">Kullanıcı</dt>
      <dd className="tabular">{formatNumber(i.users)}</dd>
      <dt className="text-muted-foreground">Son 24 saatte olay</dt>
      <dd className="tabular">{formatNumber(i.events24h)}</dd>
      <dt className="text-muted-foreground">Telegram</dt>
      <dd>{feature(!!i.features?.telegram)}</dd>
      <dt className="text-muted-foreground">Yapay zekâ</dt>
      <dd>{feature(!!i.features?.ai)}</dd>
      <dt className="text-muted-foreground">GitHub App</dt>
      <dd>{feature(!!i.features?.githubApp)}</dd>
      <dt className="text-muted-foreground">MCP</dt>
      <dd>{feature(!!i.features?.mcp)}</dd>
      <dt className="text-muted-foreground">OpenID sağlayıcıları</dt>
      <dd>{i.features?.oidcProviders.length ? i.features.oidcProviders.join(", ") : "Yok"}</dd>
    </dl>
  );
}

function Organizations() {
  const [query, setQuery] = useState("");
  const q = useQuery(
    AdminService.method.listOrganizations,
    { query, pageSize: 100 },
    { transport: baseTransport, placeholderData: (p) => p },
  );
  const [editing, setEditing] = useState<AdminOrganization>();
  return (
    <div className="max-w-5xl py-6">
      <Input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Ad ya da kısa ad"
        className="mb-3 max-w-sm"
        aria-label="Organizasyon ara"
      />
      <ul className="divide-y rounded-lg border md:hidden">
        {q.data?.organizations.map((o) => (
          <li key={o.organization?.id} className="flex items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium text-md">{o.organization?.name}</p>
              <p className="truncate font-mono text-sm text-muted-foreground">{o.organization?.slug}</p>
              <p className="mt-1 text-sm text-muted-foreground">
                {o.organization?.plan || "Plan yok"}, {formatNumber(o.organization?.projectCount ?? 0)} proje,{" "}
                {formatNumber(o.organization?.memberCount ?? 0)} üye, bu ay {formatCompact(o.monthEvents)}{" "}
                olay
              </p>
            </div>
            <Button variant="outline" size="sm" onClick={() => setEditing(o)}>
              Plan
            </Button>
          </li>
        ))}
      </ul>
      <div className="hidden overflow-x-auto rounded-lg border md:block">
        <table className="w-full text-sm">
          <thead className={theadClass}>
            <tr>
              <th className={thClass}>Organizasyon</th>
              <th className={thClass}>Plan</th>
              <th className={cn(thClass, "text-right")}>Proje</th>
              <th className={cn(thClass, "text-right")}>Üye</th>
              <th className={cn(thClass, "text-right")}>Bu ay olay</th>
              <th className={thClass}>Son olay</th>
              <th className={thClass} />
            </tr>
          </thead>
          <tbody className="divide-y">
            {q.data?.organizations.map((o) => (
              <tr key={o.organization?.id} className={trClass}>
                <td className="px-3.5 py-2">
                  <p className="font-medium">{o.organization?.name}</p>
                  <p className="font-mono text-sm text-muted-foreground">{o.organization?.slug}</p>
                </td>
                <td className="px-3.5 py-2">{o.organization?.plan || "—"}</td>
                <td className="px-3.5 py-2 text-right tabular">
                  {formatNumber(o.organization?.projectCount ?? 0)}
                </td>
                <td className="px-3.5 py-2 text-right tabular">
                  {formatNumber(o.organization?.memberCount ?? 0)}
                </td>
                <td className="px-3.5 py-2 text-right tabular">{formatCompact(o.monthEvents)}</td>
                <td className="px-3.5 py-2 text-muted-foreground">
                  <RelativeTime value={o.lastEventAt} />
                </td>
                <td className="px-3.5 py-2 text-right">
                  <Button variant="ghost" size="sm" onClick={() => setEditing(o)}>
                    Plan
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {editing ? <PlanDialog org={editing} onClose={() => setEditing(undefined)} /> : null}
    </div>
  );
}

function PlanDialog({ org, onClose }: { org: AdminOrganization; onClose: () => void }) {
  const qc = useQueryClient();
  const o = org.organization;
  const l = o?.limits;
  const [plan, setPlan] = useState(o?.plan || "default");
  const [monthly, setMonthly] = useState(l?.monthlyEvents ? String(l.monthlyEvents) : "");
  const [retention, setRetention] = useState(l?.retentionDays ? String(l.retentionDays) : "");
  const [projects, setProjects] = useState(l?.maxProjects ? String(l.maxProjects) : "");
  const [members, setMembers] = useState(l?.maxMembers ? String(l.maxMembers) : "");
  const [aiRuns, setAiRuns] = useState(l?.monthlyAiRuns ? String(l.monthlyAiRuns) : "");
  const update = useMutation(AdminService.method.updateOrganizationPlan, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Plan güncellendi");
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: AdminService,
          transport: baseTransport,
          cardinality: undefined,
        }),
      });
      onClose();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const num = (v: string) => Math.max(0, Math.floor(Number(v) || 0));
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{o?.name} planı</DialogTitle>
          <DialogDescription>Boş ya da 0 bırakılan sınırlar uygulanmaz.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            update.mutate({
              organizationId: o?.id ?? "",
              plan,
              limits: create(OrganizationLimitsSchema, {
                monthlyEvents: BigInt(num(monthly)),
                retentionDays: num(retention),
                maxProjects: num(projects),
                maxMembers: num(members),
                monthlyAiRuns: num(aiRuns),
              }),
            });
          }}
        >
          <Field label="Plan adı" htmlFor="plan-name">
            <Input id="plan-name" required value={plan} onChange={(e) => setPlan(e.target.value)} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Aylık olay" htmlFor="plan-events">
              <Input
                id="plan-events"
                type="number"
                min={0}
                value={monthly}
                onChange={(e) => setMonthly(e.target.value)}
              />
            </Field>
            <Field label="Saklama (gün)" htmlFor="plan-retention">
              <Input
                id="plan-retention"
                type="number"
                min={0}
                value={retention}
                onChange={(e) => setRetention(e.target.value)}
              />
            </Field>
            <Field label="Proje" htmlFor="plan-projects">
              <Input
                id="plan-projects"
                type="number"
                min={0}
                value={projects}
                onChange={(e) => setProjects(e.target.value)}
              />
            </Field>
            <Field label="Üye" htmlFor="plan-members">
              <Input
                id="plan-members"
                type="number"
                min={0}
                value={members}
                onChange={(e) => setMembers(e.target.value)}
              />
            </Field>
            <Field label="Aylık analiz" htmlFor="plan-ai">
              <Input
                id="plan-ai"
                type="number"
                min={0}
                value={aiRuns}
                onChange={(e) => setAiRuns(e.target.value)}
              />
            </Field>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Vazgeç
            </Button>
            <Button type="submit" disabled={update.isPending}>
              Kaydet
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Users() {
  const qc = useQueryClient();
  const me = useSession()?.user?.id;
  const [query, setQuery] = useState("");
  const [resetting, setResetting] = useState<AdminUser>();
  const q = useQuery(
    AdminService.method.listUsers,
    { query, pageSize: 100 },
    { transport: baseTransport, placeholderData: (p) => p },
  );
  const update = useMutation(AdminService.method.updateUser, {
    transport: baseTransport,
    onSuccess: () => {
      toast.success("Kullanıcı güncellendi");
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: AdminService,
          transport: baseTransport,
          cardinality: undefined,
        }),
      });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  return (
    <div className="max-w-5xl py-6">
      <Input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="E-posta ya da ad"
        className="mb-3 max-w-sm"
        aria-label="Kullanıcı ara"
      />
      <ul className="divide-y rounded-lg border">
        {q.data?.users.map((u) => {
          const self = u.user?.id === me;
          return (
            <li key={u.user?.id} className="flex flex-wrap items-center gap-x-5 gap-y-2 px-4 py-3">
              <UserAvatar
                name={u.user?.name || u.user?.email || ""}
                src={u.user?.avatarUrl}
                className="size-8"
              />
              <div className="min-w-0 flex-[1_1_14rem]">
                <p className="truncate font-medium text-md">{u.user?.name || u.user?.email}</p>
                <p className="truncate text-xs text-muted-foreground">
                  {u.user?.email}, {u.organizationCount} organizasyon,{" "}
                  {u.lastLoginAt ? (
                    <>
                      son giriş <RelativeTime value={u.lastLoginAt} />
                    </>
                  ) : (
                    "hiç giriş yapmadı"
                  )}
                  {u.user?.twoFactorEnabled ? ", iki adımlı doğrulama açık" : null}
                </p>
              </div>
              {u.user?.twoFactorEnabled ? (
                <Button variant="ghost" size="sm" onClick={() => setResetting(u)}>
                  İki adımlı doğrulamayı sıfırla
                </Button>
              ) : null}
              <div className="flex items-center gap-2">
                <Switch
                  id={`sa-${u.user?.id}`}
                  checked={u.user?.superadmin ?? false}
                  disabled={self || update.isPending}
                  onCheckedChange={(v) => update.mutate({ userId: u.user?.id ?? "", superadmin: v })}
                />
                <Label htmlFor={`sa-${u.user?.id}`} className="font-normal text-sm text-muted-foreground">
                  Sunucu yöneticisi
                </Label>
              </div>
              <div className="flex items-center gap-2">
                <Switch
                  id={`off-${u.user?.id}`}
                  checked={u.disabled}
                  disabled={self || update.isPending}
                  onCheckedChange={(v) => update.mutate({ userId: u.user?.id ?? "", disabled: v })}
                />
                <Label htmlFor={`off-${u.user?.id}`} className="font-normal text-sm text-muted-foreground">
                  Devre dışı
                </Label>
              </div>
            </li>
          );
        })}
      </ul>
      <Dialog open={resetting !== undefined} onOpenChange={(v) => (v ? undefined : setResetting(undefined))}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>İki adımlı doğrulamayı sıfırla</DialogTitle>
            <DialogDescription>
              {resetting?.user?.email} artık yalnız parolasıyla giriş yapar ve kurtarma kodları silinir. Bunu
              yalnız telefonunu ve kurtarma kodlarını kaybeden, kimliğini başka yoldan doğruladığınız
              kullanıcılar için yapın.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setResetting(undefined)}>
              Vazgeç
            </Button>
            <Button
              variant="destructive"
              disabled={update.isPending}
              onClick={() =>
                update.mutate(
                  { userId: resetting?.user?.id ?? "", resetTwoFactor: true },
                  { onSuccess: () => setResetting(undefined) },
                )
              }
            >
              Sıfırla
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
