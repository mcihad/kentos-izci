import { createConnectQueryKey, useInfiniteQuery, useMutation, useQuery } from "@connectrpc/connect-query";
import { StatStrip } from "@kentos/components/common/stat-strip";
import { tdClass, thClass, theadClass, trClass } from "@kentos/components/common/table";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { ChevronRightIcon, PauseIcon, PencilIcon, PlayIcon, Trash2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { gutter, Section, useDocumentTitle } from "@/components/app/page";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { MonitorDialog } from "@/components/monitor/monitor-dialog";
import { MonitorSetup } from "@/components/monitor/setup";
import {
  CheckInBadge,
  durationMs,
  headlineEnv,
  MonitorBadge,
  monitorStatus,
} from "@/components/monitor/status";
import { type Monitor, MonitorService } from "@/gen/izci/api/v1/monitor_pb";
import { crontabOf, describeSchedule } from "@/lib/cron";
import { errorMessage } from "@/lib/errors";
import { formatDateTime, formatDuration, formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";

export const Route = createFileRoute("/o/$org/monitors/$monitorId")({
  component: MonitorPage,
});

function MonitorPage() {
  const org = useOrg();
  const { monitorId } = Route.useParams();
  const get = useQuery(MonitorService.method.getMonitor, { id: monitorId }, { refetchInterval: 30_000 });
  const m = get.data?.monitor;
  useDocumentTitle(m?.name);
  if (get.error) {
    return (
      <div className={cn("py-10", gutter)}>
        <ErrorState error={get.error} onRetry={() => void get.refetch()} className="card" />
      </div>
    );
  }
  if (!m) {
    return (
      <div className={cn("space-y-4 py-6", gutter)}>
        <Skeleton className="h-10 w-1/3" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  return <MonitorView key={m.id} monitor={m} orgSlug={org.slug} />;
}

function MonitorView({ monitor: m, orgSlug }: { monitor: Monitor; orgSlug: string }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const refresh = () =>
    qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: MonitorService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
  const update = useMutation(MonitorService.method.updateMonitor, {
    onSuccess: (res) => {
      toast.success(res.monitor?.paused ? "İzleyici duraklatıldı" : "İzleyici sürdürülüyor");
      void refresh();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const remove = useMutation(MonitorService.method.deleteMonitor, {
    onSuccess: () => {
      toast.success("İzleyici silindi");
      void refresh();
      void navigate({ to: "/o/$org/monitors", params: { org: orgSlug } });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const env = headlineEnv(m);
  const cron = crontabOf(m);

  return (
    <div className="pb-12">
      <header className={cn("pt-4 pb-4 sm:pt-5 sm:pb-5", gutter)}>
        <nav className="flex items-center gap-1.5 text-muted-foreground text-sm" aria-label="Konum">
          <Link to="/o/$org/monitors" params={{ org: orgSlug }} className="-my-2 py-2 hover:text-foreground">
            Zamanlanmış görevler
          </Link>
          <ChevronRightIcon className="size-3.5" />
          <span className="font-mono text-foreground">{m.slug}</span>
        </nav>
        <div className="mt-2 flex flex-wrap items-end gap-x-6 gap-y-3">
          <div className="min-w-0 flex-[1_1_18rem]">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
              <h1 className="min-w-0 font-semibold text-xl leading-tight sm:text-2xl">{m.name}</h1>
              <MonitorBadge monitor={m} />
            </div>
            <p className="mt-1.5 text-md text-muted-foreground">
              {describeSchedule(m.schedule)}
              {cron && describeSchedule(m.schedule) !== cron ? (
                <>
                  {" "}
                  (<span className="font-mono">{cron}</span>)
                </>
              ) : null}
              , {m.timezone}. Proje:{" "}
              <Link
                to="/o/$org/projects/$slug"
                params={{ org: orgSlug, slug: m.project?.slug ?? "" }}
                className="text-link hover:underline"
              >
                {m.project?.name}
              </Link>
            </p>
          </div>
          {admin ? (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={update.isPending}
                onClick={() => update.mutate({ id: m.id, paused: !m.paused })}
              >
                {m.paused ? <PlayIcon /> : <PauseIcon />}
                {m.paused ? "Sürdür" : "Duraklat"}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                <PencilIcon />
                Düzenle
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="text-destructive"
                onClick={() => setDeleting(true)}
              >
                <Trash2Icon />
                Sil
              </Button>
            </div>
          ) : null}
        </div>
      </header>

      <div className={cn("space-y-4", gutter)}>
        <StatStrip
          stats={[
            {
              label: "Son çalışma",
              value: env?.lastCheckInAt ? <RelativeTime value={env.lastCheckInAt} /> : "Henüz yok",
              note: env ? monitorStatus[env.status].label : undefined,
            },
            {
              label: "Sonraki çalışma",
              value: m.paused ? (
                "Duraklatıldı"
              ) : env?.nextCheckInAt ? (
                <RelativeTime value={env.nextCheckInAt} />
              ) : (
                "İlk çalışmadan sonra"
              ),
              note:
                env?.nextCheckInLatest && !m.paused
                  ? `En geç ${formatDateTime(env.nextCheckInLatest)}`
                  : undefined,
            },
            {
              label: "Art arda sorun",
              value: formatNumber(env?.failures ?? 0),
              note: `Eşik ${m.failureThreshold}`,
            },
            {
              label: "Açık hata kaydı",
              value: env?.issue ? (
                <Link
                  to="/o/$org/issues/$issueId"
                  params={{ org: orgSlug, issueId: env.issue.shortId }}
                  className="text-link hover:underline"
                >
                  {env.issue.shortId}
                </Link>
              ) : (
                "Yok"
              ),
              note: env?.issue ? `${m.recoveryThreshold} başarılı çalışmada kapanır` : undefined,
            },
          ]}
        />

        {m.environments.length > 1 ? <Environments monitor={m} orgSlug={orgSlug} /> : null}
        <Runs monitor={m} />

        <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
          <MonitorSetup monitor={m} />
          <Section title="Ayarlar">
            <dl className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-2.5 text-md">
              <dt className="text-muted-foreground">Kısa ad</dt>
              <dd className="font-mono text-sm">{m.slug}</dd>
              <dt className="text-muted-foreground">Gecikme payı</dt>
              <dd className="tabular">{m.checkinMarginMinutes} dk</dd>
              <dt className="text-muted-foreground">En uzun çalışma</dt>
              <dd className="tabular">{m.maxRuntimeMinutes} dk</dd>
              <dt className="text-muted-foreground">Hata eşiği</dt>
              <dd className="tabular">{m.failureThreshold} sorun</dd>
              <dt className="text-muted-foreground">Toparlanma eşiği</dt>
              <dd className="tabular">{m.recoveryThreshold} başarı</dd>
            </dl>
          </Section>
        </div>
      </div>

      <MonitorDialog
        open={editing}
        onOpenChange={setEditing}
        monitor={m}
        onSaved={() => {
          setEditing(false);
          toast.success("İzleyici güncellendi");
          void refresh();
        }}
      />
      <Dialog open={deleting} onOpenChange={setDeleting}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>İzleyiciyi sil</DialogTitle>
            <DialogDescription>
              {m.name} ve çalışma geçmişi silinir. Açtığı hata kayıtları kalır. Görev check-in göndermeyi
              sürdürürse izleyici yeniden oluşmaz; monitor_config gönderiyorsa oluşur.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleting(false)}>
              Vazgeç
            </Button>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => remove.mutate({ id: m.id })}
            >
              Sil
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Environments({ monitor: m, orgSlug }: { monitor: Monitor; orgSlug: string }) {
  return (
    <Section title="Ortamlar" description="Görevin çalıştığı her ortamın durumu ayrı izlenir." bleed>
      <ul className="divide-y">
        {m.environments.map((e) => (
          <li
            key={e.environment}
            className="flex flex-wrap items-center gap-x-5 gap-y-1 px-4 py-3 sm:px-(--card-p)"
          >
            <span className="min-w-28 font-medium">{e.environment}</span>
            <MonitorBadge monitor={m} env={e} />
            <span className="text-muted-foreground text-sm">
              Son {e.lastCheckInAt ? <RelativeTime value={e.lastCheckInAt} /> : "yok"}
            </span>
            {e.issue ? (
              <Link
                to="/o/$org/issues/$issueId"
                params={{ org: orgSlug, issueId: e.issue.shortId }}
                className="ml-auto font-mono text-link text-sm hover:underline"
              >
                {e.issue.shortId}
              </Link>
            ) : null}
          </li>
        ))}
      </ul>
    </Section>
  );
}

function Runs({ monitor: m }: { monitor: Monitor }) {
  const runs = useInfiniteQuery(
    MonitorService.method.listCheckIns,
    { monitorId: m.id, pageSize: 50, pageToken: "" },
    {
      pageParamKey: "pageToken",
      getNextPageParam: (last) => last.nextPageToken || undefined,
      refetchInterval: 30_000,
    },
  );
  const rows = runs.data?.pages.flatMap((p) => p.checkIns) ?? [];
  const multiEnv = m.environments.length > 1;
  return (
    <Section title="Çalışmalar" description="En yeni çalışma en üstte." bleed>
      {runs.isPending ? (
        <div className="space-y-2 p-4">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : rows.length === 0 ? (
        <EmptyState title="Henüz çalışma yok" className="py-10">
          Görev ilk check-in'ini gönderdiğinde burada görünür.
        </EmptyState>
      ) : (
        <>
          <ul className="divide-y md:hidden">
            {rows.map((r) => {
              const d = durationMs(r);
              return (
                <li key={r.id} className="px-4 py-3">
                  <div className="flex items-center justify-between gap-3">
                    <CheckInBadge status={r.status} />
                    <span className="text-muted-foreground text-sm">
                      <RelativeTime value={r.startedAt} />
                    </span>
                  </div>
                  <p className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-muted-foreground text-sm">
                    {d !== undefined ? (
                      <span className="text-foreground tabular">{formatDuration(d)}</span>
                    ) : null}
                    {multiEnv ? <span>{r.environment}</span> : null}
                    {r.expectedAt ? <span>Beklenen {formatDateTime(r.expectedAt)}</span> : null}
                    {r.release ? <span className="font-mono">{r.release}</span> : null}
                  </p>
                </li>
              );
            })}
          </ul>
          <div className="hidden overflow-x-auto md:block">
            <table className="w-full text-sm">
              <thead className={theadClass}>
                <tr>
                  <th className={cn(thClass, "pl-(--card-p)")}>Durum</th>
                  <th className={thClass}>Başladı</th>
                  <th className={thClass}>Beklenen zaman</th>
                  <th className={cn(thClass, "text-right")}>Süre</th>
                  {multiEnv ? <th className={thClass}>Ortam</th> : null}
                  <th className={cn(thClass, "pr-(--card-p)")}>Sürüm</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((r) => {
                  const d = durationMs(r);
                  return (
                    <tr key={r.id} className={trClass}>
                      <td className={cn(tdClass, "pl-(--card-p)")}>
                        <CheckInBadge status={r.status} />
                      </td>
                      <td className={cn(tdClass, "whitespace-nowrap")}>
                        {formatDateTime(r.startedAt)}{" "}
                        <span className="text-muted-foreground">
                          (<RelativeTime value={r.startedAt} />)
                        </span>
                      </td>
                      <td className={cn(tdClass, "whitespace-nowrap text-muted-foreground")}>
                        {r.expectedAt ? formatDateTime(r.expectedAt) : "—"}
                      </td>
                      <td className={cn(tdClass, "text-right tabular")}>
                        {d !== undefined ? formatDuration(d) : "—"}
                      </td>
                      {multiEnv ? <td className={tdClass}>{r.environment}</td> : null}
                      <td className={cn(tdClass, "pr-(--card-p) font-mono text-muted-foreground")}>
                        {r.release || "—"}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {runs.hasNextPage ? (
            <div className="flex justify-center border-t py-3">
              <Button
                variant="outline"
                size="sm"
                onClick={() => void runs.fetchNextPage()}
                disabled={runs.isFetchingNextPage}
              >
                Daha eski çalışmalar
              </Button>
            </div>
          ) : null}
        </>
      )}
    </Section>
  );
}
