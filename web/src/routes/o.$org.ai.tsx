import { useInfiniteQuery, useQuery } from "@connectrpc/connect-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { GitPullRequestDraftIcon, GitPullRequestIcon, SparklesIcon } from "lucide-react";
import { changeStats, runDuration, runTrigger } from "@/components/ai/labels";
import { RunStatusPill } from "@/components/ai/run-view";
import { gutter, PageHeader } from "@/components/app/page";
import { RelativeTime } from "@/components/common/relative-time";
import { StatStrip } from "@/components/common/stat-strip";
import { EmptyState, ErrorState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { type AiRun, AiRunStatus, AiService } from "@/gen/izci/api/v1/ai_pb";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { formatDuration, formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { cn } from "@/lib/utils";

// Search validation stays free of generated enums so that the route
// definition (which is part of the main bundle) does not pull them in.
const statusIds = ["all", "running", "queued", "succeeded", "failed"] as const;
type StatusId = (typeof statusIds)[number];

type Search = { status?: StatusId; project?: string };

export const Route = createFileRoute("/o/$org/ai")({
  validateSearch: (s: Record<string, unknown>): Search => ({
    status: statusIds.includes(s.status as StatusId) ? (s.status as StatusId) : undefined,
    project: typeof s.project === "string" ? s.project : undefined,
  }),
  component: AiPage,
});

const statuses: { id: StatusId; label: string; value: AiRunStatus }[] = [
  { id: "all", label: "Tüm durumlar", value: AiRunStatus.UNSPECIFIED },
  { id: "running", label: "Çalışanlar", value: AiRunStatus.RUNNING },
  { id: "queued", label: "Sıradakiler", value: AiRunStatus.QUEUED },
  { id: "succeeded", label: "Tamamlananlar", value: AiRunStatus.SUCCEEDED },
  { id: "failed", label: "Başarısızlar", value: AiRunStatus.FAILED },
];

function AiPage() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const status = useQuery(AiService.method.getAiStatus, {});
  const projects = useQuery(ProjectService.method.listProjects, {});
  const filter = statuses.find((s) => s.id === (search.status ?? "all")) ?? {
    id: "all",
    label: "",
    value: AiRunStatus.UNSPECIFIED,
  };
  const runs = useInfiniteQuery(
    AiService.method.listRuns,
    { status: filter.value, project: search.project ?? "", pageSize: 30, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const list = runs.data?.pages.flatMap((p) => p.runs) ?? [];
  const st = status.data;

  return (
    <div className="pb-12">
      <PageHeader
        title="Yapay zekâ"
        description="Hataların yapay zekâ analizleri ve önerilen düzeltmeler"
        actions={
          <>
            <Select
              value={search.project ?? "all"}
              onValueChange={(v) =>
                void navigate({ search: (p) => ({ ...p, project: v === "all" ? undefined : v }) })
              }
            >
              <SelectTrigger size="sm" className="w-44 text-sm" aria-label="Proje">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Tüm projeler</SelectItem>
                {projects.data?.projects.map((p) => (
                  <SelectItem key={p.slug} value={p.slug}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={filter.id}
              onValueChange={(v) =>
                void navigate({
                  search: (p) => ({ ...p, status: v === "all" ? undefined : (v as StatusId) }),
                })
              }
            >
              <SelectTrigger size="sm" className="w-40 text-sm" aria-label="Durum">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {statuses.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </>
        }
      />
      <div className={cn("grid grid-cols-1 gap-6 pt-6", gutter)}>
        {st ? (
          <StatStrip
            stats={[
              {
                label: "Durum",
                value: st.enabled ? "Açık" : "Kapalı",
                note: st.enabled ? `opencode ${st.opencodeVersion || "bulunamadı"}` : "AI_ENABLED=false",
              },
              {
                label: "Model",
                value: <span className="text-lg">{st.defaultModel || "Varsayılan"}</span>,
              },
              {
                label: "Çalışan",
                value: formatNumber(st.running),
                note: `En çok ${st.maxConcurrent} aynı anda`,
              },
              { label: "Sırada", value: formatNumber(st.queued) },
              {
                label: "Bu ay",
                value: formatNumber(st.monthlyRuns),
                note: st.monthlyLimit ? `Sınır ${formatNumber(st.monthlyLimit)}` : "Sınırsız",
              },
            ]}
          />
        ) : (
          <Skeleton className="h-[86px] w-full" />
        )}

        {runs.isPending ? (
          <div className="grid gap-2">
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
          </div>
        ) : runs.error ? (
          <ErrorState error={runs.error} onRetry={() => void runs.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState icon={SparklesIcon} title="Henüz analiz yok">
            Bir hatanın sayfasında <b className="font-medium text-foreground">Yapay zekâ</b> sekmesinden
            analiz başlatabilir ya da proje ayarlarından yeni hatalar için otomatik analizi açabilirsiniz.
          </EmptyState>
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <ul className="divide-y">
              {list.map((r) => (
                <RunRow key={r.id} run={r} />
              ))}
            </ul>
            {runs.hasNextPage ? (
              <div className="border-t px-4 py-2.5 text-center">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={runs.isFetchingNextPage}
                  onClick={() => void runs.fetchNextPage()}
                >
                  Daha fazla göster
                </Button>
              </div>
            ) : null}
          </div>
        )}
      </div>
    </div>
  );
}

function RunRow({ run }: { run: AiRun }) {
  const org = useOrg();
  const stats = changeStats(run);
  const took = runDuration(run);
  const pr = run.pullRequest;
  return (
    <li>
      <Link
        to="/o/$org/issues/$issueId"
        params={{ org: org.slug, issueId: run.issue?.shortId || run.issue?.id || "" }}
        search={{ tab: "ai", run: run.id }}
        className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-4 px-4 py-3 transition-colors hover:bg-accent/40"
      >
        <RunStatusPill run={run} className="w-[6.5rem] justify-center" />
        <div className="min-w-0">
          <p className="truncate text-md">
            <span className="mr-2 font-mono text-xs text-muted-foreground">{run.issue?.shortId}</span>
            {run.result?.summary || run.issue?.title}
          </p>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {run.project?.name} · {runTrigger[run.trigger]}
            {run.requestedBy ? `, ${run.requestedBy.name || run.requestedBy.email}` : ""} ·{" "}
            <RelativeTime value={run.createdAt} />
          </p>
        </div>
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          {stats.files > 0 ? (
            <span className="tabular hidden font-mono sm:inline">
              <span className="text-health-good-ink">+{stats.additions}</span>{" "}
              <span className="text-level-error-ink">−{stats.deletions}</span>
            </span>
          ) : null}
          {pr?.number ? (
            pr.draft ? (
              <GitPullRequestDraftIcon className="size-4" aria-label="Taslak çekme isteği" />
            ) : (
              <GitPullRequestIcon className="size-4 text-health-good-ink" aria-label="Çekme isteği" />
            )
          ) : null}
          {took !== undefined && run.finishedAt ? (
            <span className="tabular hidden w-16 text-right md:inline">{formatDuration(took)}</span>
          ) : null}
        </div>
      </Link>
    </li>
  );
}
