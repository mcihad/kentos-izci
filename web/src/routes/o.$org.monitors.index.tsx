import { useQuery } from "@connectrpc/connect-query";
import { PageAction } from "@kentos/components/layout/page";
import { Button } from "@kentos/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { PlusIcon, TimerIcon } from "lucide-react";
import { useState } from "react";
import { gutter, Page } from "@/components/app/page";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { MonitorDialog } from "@/components/monitor/monitor-dialog";
import { headlineEnv, MonitorBadge, RunStrip } from "@/components/monitor/status";
import { type Monitor, MonitorService, MonitorStatus } from "@/gen/izci/api/v1/monitor_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { crontabOf, describeSchedule } from "@/lib/cron";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";

type MonitorsSearch = { project?: string };

export const Route = createFileRoute("/o/$org/monitors/")({
  validateSearch: (s: Record<string, unknown>): MonitorsSearch => ({
    project: typeof s.project === "string" && s.project ? s.project : undefined,
  }),
  component: MonitorsPage,
});

function MonitorsPage() {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const projects = useQuery(ProjectService.method.listProjects, {}, { staleTime: 60_000 });
  const list = useQuery(
    MonitorService.method.listMonitors,
    { project: search.project ?? "" },
    { refetchInterval: 30_000 },
  );
  const [creating, setCreating] = useState(false);
  const monitors = list.data?.monitors ?? [];
  const failing = monitors.filter((m) => {
    const s = headlineEnv(m)?.status;
    return (
      !m.paused && (s === MonitorStatus.ERROR || s === MonitorStatus.MISSED || s === MonitorStatus.TIMEOUT)
    );
  }).length;

  return (
    <Page
      icon={TimerIcon}
      title="Zamanlanmış görevler"
      description={
        list.data && monitors.length > 0
          ? failing > 0
            ? `${monitors.length} görev izleniyor, ${failing} tanesi sorunlu`
            : `${monitors.length} görev izleniyor, hepsi zamanında çalışıyor`
          : "Rapor, tahsilat ve aktarım gibi düzenli işlerin zamanında ve hatasız çalıştığını izleyin"
      }
      filters={
        <Select
          value={search.project ?? "all"}
          onValueChange={(v) =>
            void navigate({ search: { project: v === "all" ? undefined : v }, replace: true })
          }
        >
          <SelectTrigger size="sm" aria-label="Proje">
            <SelectValue />
          </SelectTrigger>
          <SelectContent align="end">
            <SelectItem value="all">Tüm projeler</SelectItem>
            {(projects.data?.projects ?? []).map((p) => (
              <SelectItem key={p.id} value={p.slug}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      }
      actions={
        admin ? (
          <PageAction
            icon={PlusIcon}
            label="Yeni izleyici"
            description="Düzenli bir işin zamanında çalıştığını izleyin"
            variant="default"
            onClick={() => setCreating(true)}
          />
        ) : null
      }
    >
      <div className={gutter}>
        {list.isPending ? (
          <div className="card space-y-3 p-4">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        ) : list.error ? (
          <ErrorState error={list.error} onRetry={() => void list.refetch()} className="card" />
        ) : monitors.length === 0 ? (
          <EmptyState
            icon={TimerIcon}
            title="Henüz izlenen görev yok"
            className="card"
            action={
              admin ? (
                <Button size="sm" onClick={() => setCreating(true)}>
                  <PlusIcon />
                  İzleyici oluştur
                </Button>
              ) : null
            }
          >
            Görev ilk check-in'ini gönderdiğinde izleyici kendiliğinden oluşur; isterseniz buradan da
            oluşturabilirsiniz. Sentry SDK'larındaki cron izleme olduğu gibi çalışır.
          </EmptyState>
        ) : (
          <ul className="card divide-y overflow-hidden max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0">
            {monitors.map((m) => (
              <MonitorRow key={m.id} monitor={m} />
            ))}
          </ul>
        )}
      </div>
      <MonitorDialog
        open={creating}
        onOpenChange={setCreating}
        defaultProject={search.project}
        onSaved={(m) => {
          setCreating(false);
          void navigate({ to: "/o/$org/monitors/$monitorId", params: { org: org.slug, monitorId: m.id } });
        }}
      />
    </Page>
  );
}

function MonitorRow({ monitor: m }: { monitor: Monitor }) {
  const org = useOrg();
  const env = headlineEnv(m);
  const cron = crontabOf(m);
  return (
    <li className="relative grid gap-x-6 gap-y-2.5 px-4 py-3.5 transition-colors duration-(--duration-fast) hover:bg-(--table-row-hover) sm:px-(--card-p) md:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)_auto_minmax(8rem,auto)] md:items-center">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <Link
            to="/o/$org/monitors/$monitorId"
            params={{ org: org.slug, monitorId: m.id }}
            className="min-w-0 truncate font-semibold text-md after:absolute after:inset-0"
          >
            {m.name}
          </Link>
          <MonitorBadge monitor={m} />
        </div>
        <p className="mt-0.5 truncate text-muted-foreground text-sm">
          <span className="font-mono">{m.slug}</span>, {m.project?.name}
        </p>
      </div>
      <div className="min-w-0 text-sm">
        <p className="truncate">{describeSchedule(m.schedule)}</p>
        <p className="truncate text-muted-foreground">
          {cron ? <span className="font-mono">{cron}</span> : null}
          {cron ? " " : null}
          {m.timezone}
        </p>
      </div>
      <RunStrip runs={m.recentCheckIns} slots={24} />
      <div className="grid gap-0.5 text-sm md:text-right">
        <p>
          <span className="text-muted-foreground">Son </span>
          {env?.lastCheckInAt ? <RelativeTime value={env.lastCheckInAt} /> : "henüz yok"}
        </p>
        {env?.nextCheckInAt && !m.paused ? (
          <p className="text-muted-foreground">
            Sonraki <RelativeTime value={env.nextCheckInAt} />
          </p>
        ) : null}
      </div>
    </li>
  );
}
