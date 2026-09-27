import { useMutation, useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import {
  ChevronDownIcon,
  CircleStopIcon,
  ExternalLinkIcon,
  GitBranchIcon,
  GitPullRequestDraftIcon,
  GitPullRequestIcon,
} from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { type AiRun, AiRunMode, AiRunStatus, AiService } from "@/gen/izci/api/v1/ai_pb";
import type { ProjectRepository } from "@/gen/izci/api/v1/project_pb";
import { errorMessage } from "@/lib/errors";
import { formatCompact, formatDuration } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { cn } from "@/lib/utils";
import { DiffView } from "./diff-view";
import { changeStats, formatCost, isActive, riskLabels, runDuration, runStatus, runTrigger } from "./labels";
import { RunLog } from "./run-log";
import { useInvalidateRuns, useRunWatch } from "./use-run-watch";

const Markdown = lazy(() => import("@/components/common/markdown"));

/** Status name with a pulse while the run works. */
export function RunStatusPill({ run, className }: { run: AiRun; className?: string }) {
  const s = runStatus[run.status];
  return (
    <span
      className={cn(
        "inline-flex h-5 items-center gap-1.5 whitespace-nowrap rounded-full border px-2 font-medium text-[11px]",
        s.tone,
        className,
      )}
    >
      {s.active ? (
        <span className="relative flex size-1.5">
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-current opacity-60 motion-reduce:animate-none" />
          <span className="relative inline-flex size-1.5 rounded-full bg-current" />
        </span>
      ) : null}
      {s.label}
    </span>
  );
}

/** Seconds since the run started, refreshed every second while it works. */
function useElapsed(run: AiRun) {
  const [now, setNow] = useState(Date.now());
  const active = isActive(run);
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [active]);
  return runDuration(run, now);
}

function RunMeta({ run, repository }: { run: AiRun; repository?: ProjectRepository }) {
  const elapsed = useElapsed(run);
  const tokens = Number(run.usage?.inputTokens ?? 0n) + Number(run.usage?.outputTokens ?? 0n);
  const parts: React.ReactNode[] = [
    <span key="who">
      {runTrigger[run.trigger]}
      {run.requestedBy ? `, ${run.requestedBy.name || run.requestedBy.email}` : ""}
    </span>,
    <RelativeTime key="at" value={run.createdAt} />,
  ];
  if (elapsed !== undefined) parts.push(<span key="dur">{formatDuration(elapsed)}</span>);
  if (run.model)
    parts.push(
      <span key="model" className="font-mono text-[11.5px]">
        {run.model}
      </span>,
    );
  if (tokens > 0)
    parts.push(
      <span key="tok">
        {formatCompact(tokens)} token{run.usage?.costUsd ? `, ${formatCost(run.usage.costUsd)}` : ""}
      </span>,
    );
  if (run.baseCommit) {
    const short = run.baseCommit.slice(0, 10);
    parts.push(
      repository?.url.startsWith("https://") ? (
        <a
          key="commit"
          href={`${repository.url}/commit/${run.baseCommit}`}
          target="_blank"
          rel="noreferrer"
          className="font-mono text-[11.5px] hover:text-foreground hover:underline"
        >
          {short}
        </a>
      ) : (
        <span key="commit" className="font-mono text-[11.5px]">
          {short}
        </span>
      ),
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-muted-foreground">
      {parts.map((p, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: fixed order
        <span key={i} className="inline-flex items-center gap-2">
          {i > 0 ? (
            <span aria-hidden="true" className="size-0.5 rounded-full bg-muted-foreground/60" />
          ) : null}
          {p}
        </span>
      ))}
    </div>
  );
}

function Confidence({ value }: { value: number }) {
  const pct = Math.round(value * 100);
  return (
    <div className="flex items-center gap-2" title="Ajanın kök neden ve düzeltmeye güveni">
      <div className="h-1.5 w-20 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
      </div>
      <span className="tabular text-[12px] text-muted-foreground">%{pct} güven</span>
    </div>
  );
}

function ResultSummary({ run }: { run: AiRun }) {
  const r = run.result;
  if (!r) return null;
  const risk = riskLabels[r.risk];
  const stats = changeStats(run);
  return (
    <div className="grid gap-3">
      {r.summary ? <p className="text-[15px] leading-relaxed">{r.summary}</p> : null}
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
        {r.confidence > 0 ? <Confidence value={r.confidence} /> : null}
        {risk ? <span className={cn("font-medium text-[12px]", risk.tone)}>{risk.label}</span> : null}
        {stats.files > 0 ? (
          <span className="tabular text-[12px] text-muted-foreground">
            {stats.files} dosya <span className="font-mono text-health-good">+{stats.additions}</span>{" "}
            <span className="font-mono text-level-error">−{stats.deletions}</span>
          </span>
        ) : run.mode === AiRunMode.FIX ? (
          <span className="text-[12px] text-muted-foreground">Kod değişikliği önerilmedi</span>
        ) : null}
      </div>
      {r.rootCause ? (
        <div className="grid gap-1 border-l-2 border-primary/50 pl-3">
          <p className="font-medium text-[12px] text-muted-foreground">Kök neden</p>
          <p className="text-[13.5px] leading-relaxed [overflow-wrap:anywhere]">{r.rootCause}</p>
        </div>
      ) : null}
    </div>
  );
}

function PublishBar({
  run,
  repository,
  writable,
}: {
  run: AiRun;
  repository?: ProjectRepository;
  writable: boolean;
}) {
  const org = useOrg();
  const invalidate = useInvalidateRuns();
  const onError = (err: unknown) => toast.error(errorMessage(err));
  const branch = useMutation(AiService.method.createBranch, {
    onSuccess: (res) => {
      toast.success(`Dal oluşturuldu: ${res.pullRequest?.branch ?? ""}`);
      void invalidate();
    },
    onError,
  });
  const pr = useMutation(AiService.method.createPullRequest, {
    onSuccess: (res) => {
      toast.success(`Çekme isteği açıldı: #${res.pullRequest?.number ?? ""}`);
      void invalidate();
    },
    onError,
  });
  const published = run.pullRequest;
  const busy = branch.isPending || pr.isPending;

  if (published?.number) {
    return (
      <a
        href={published.url}
        target="_blank"
        rel="noreferrer"
        className="flex items-center gap-3 rounded-lg border px-4 py-3 transition-colors hover:bg-accent/40"
      >
        {published.draft ? (
          <GitPullRequestDraftIcon className="size-4 text-muted-foreground" />
        ) : (
          <GitPullRequestIcon className="size-4 text-health-good" />
        )}
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium text-[13px]">
            #{published.number} {published.title}
          </p>
          <p className="truncate font-mono text-[11.5px] text-muted-foreground">
            {published.draft ? "Taslak · " : ""}
            {published.branch}
          </p>
        </div>
        <ExternalLinkIcon className="size-3.5 text-muted-foreground" />
      </a>
    );
  }

  const canPublish = !!repository?.integrationId;
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border bg-muted/30 px-4 py-3">
      <div className="min-w-0 flex-[1_1_16rem]">
        {published ? (
          <p className="text-[13px]">
            Düzeltme{" "}
            <a
              href={published.url}
              target="_blank"
              rel="noreferrer"
              className="font-mono text-[12.5px] hover:underline"
            >
              {published.branch}
            </a>{" "}
            dalında.
          </p>
        ) : (
          <p className="text-[13px]">Önerilen düzeltmeyi inceleyin; uygunsa GitHub'a gönderin.</p>
        )}
        {!canPublish ? (
          <p className="mt-0.5 text-[12px] text-muted-foreground">
            Göndermek için projenin deposunu bir GitHub bağlantısıyla bağlayın:{" "}
            <Link
              to="/o/$org/projects/$slug"
              params={{ org: org.slug, slug: run.project?.slug ?? "" }}
              search={{ tab: "repository" }}
              className="text-primary hover:underline"
            >
              proje ayarları
            </Link>
          </p>
        ) : null}
      </div>
      {writable && canPublish ? (
        <div className="flex items-center gap-2">
          {!published ? (
            <Button
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => branch.mutate({ runId: run.id })}
            >
              <GitBranchIcon className="size-4" />
              Dal oluştur
            </Button>
          ) : null}
          <div className="flex">
            <Button
              size="sm"
              className="rounded-r-none"
              disabled={busy}
              onClick={() => pr.mutate({ runId: run.id, draft: true })}
            >
              <GitPullRequestDraftIcon className="size-4" />
              Taslak çekme isteği aç
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  size="sm"
                  className="rounded-l-none border-l border-primary-foreground/20 px-2"
                  disabled={busy}
                  aria-label="Diğer seçenekler"
                >
                  <ChevronDownIcon className="size-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => pr.mutate({ runId: run.id, draft: false })}>
                  <GitPullRequestIcon className="size-4" />
                  İncelemeye hazır çekme isteği aç
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function FinishedLog({ runId }: { runId: string }) {
  const q = useQuery(AiService.method.getRun, { id: runId, includeLog: true });
  if (q.isPending) return <Skeleton className="h-40 w-full" />;
  return <RunLog entries={q.data?.log ?? []} />;
}

/** One AI run: live progress while it works, its result when done. */
export function RunView({
  run: initial,
  repository,
  writable,
}: {
  run: AiRun;
  repository?: ProjectRepository;
  writable: boolean;
}) {
  const invalidate = useInvalidateRuns();
  // Lists carry runs without their analysis and diff.
  const detail = useQuery(AiService.method.getRun, { id: initial.id });
  const base = detail.data?.run ?? initial;
  const watch = useRunWatch(base);
  const run = watch.run ?? base;
  const active = isActive(run);
  const cancel = useMutation(AiService.method.cancelRun, {
    onSuccess: () => {
      toast.success("Analiz durduruldu");
      void invalidate();
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const hasDiff = run.diff !== "";
  const [tab, setTab] = useState<string>("analysis");

  return (
    <div className="grid grid-cols-1 gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid gap-1.5">
          <div className="flex items-center gap-2">
            <RunStatusPill run={run} />
            <span className="text-[13px] text-muted-foreground">
              {run.mode === AiRunMode.FIX ? "Kök neden ve düzeltme" : "Kök neden analizi"}
            </span>
          </div>
          <RunMeta run={run} repository={repository} />
        </div>
        {active && writable ? (
          <Button
            variant="outline"
            size="sm"
            disabled={cancel.isPending}
            onClick={() => cancel.mutate({ id: run.id })}
          >
            <CircleStopIcon className="size-4" />
            Durdur
          </Button>
        ) : null}
      </div>

      {active ? (
        <section className="rounded-lg border px-4 py-4" aria-label="Canlı günlük">
          {watch.log.length === 0 ? (
            <p className="text-[13px] text-muted-foreground">
              {run.status === AiRunStatus.QUEUED
                ? "Analiz sırada; boş bir çalışan bekleniyor."
                : "Kod hazırlanıyor…"}
            </p>
          ) : (
            <RunLog entries={watch.log} live />
          )}
        </section>
      ) : detail.isPending && !watch.run ? (
        <div className="grid gap-3">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      ) : run.status === AiRunStatus.SUCCEEDED ? (
        <>
          <section className="rounded-lg border px-5 py-4" aria-label="Sonuç">
            <ResultSummary run={run} />
          </section>
          {hasDiff ? <PublishBar run={run} repository={repository} writable={writable} /> : null}
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList variant="line" className="h-8 px-0">
              <TabsTrigger value="analysis" className="px-0 pr-4 text-[13px]">
                Analiz
              </TabsTrigger>
              {hasDiff ? (
                <TabsTrigger value="diff" className="px-0 pr-4 text-[13px]">
                  Değişiklik
                  <span className="tabular rounded bg-muted px-1 text-[11px] text-muted-foreground">
                    {run.files.length}
                  </span>
                </TabsTrigger>
              ) : null}
              <TabsTrigger value="log" className="px-0 text-[13px]">
                Günlük
              </TabsTrigger>
            </TabsList>
            <TabsContent value="analysis" className="pt-3">
              <Suspense fallback={<Skeleton className="h-32 w-full" />}>
                <Markdown text={run.analysisMarkdown} />
              </Suspense>
              {run.result?.testSuggestions.length ? (
                <div className="mt-5 rounded-lg border px-4 py-3">
                  <p className="mb-1.5 font-medium text-[13px]">Doğrulama önerileri</p>
                  <ul className="list-disc space-y-1 pl-5 text-[13px] marker:text-muted-foreground">
                    {run.result.testSuggestions.map((s) => (
                      <li key={s}>{s}</li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </TabsContent>
            {hasDiff ? (
              <TabsContent value="diff" className="pt-3">
                <DiffView diff={run.diff} />
              </TabsContent>
            ) : null}
            <TabsContent value="log" className="pt-3">
              <FinishedLog runId={run.id} />
            </TabsContent>
          </Tabs>
        </>
      ) : (
        <>
          <section
            className={cn(
              "rounded-lg border px-4 py-3 text-[13px]",
              run.status === AiRunStatus.FAILED && "border-level-error/40",
            )}
          >
            {run.status === AiRunStatus.FAILED ? (
              <>
                <p className="font-medium text-level-error">Analiz tamamlanamadı</p>
                <p className="mt-1 font-mono text-[12px] text-muted-foreground [overflow-wrap:anywhere]">
                  {run.error}
                </p>
              </>
            ) : (
              <p className="text-muted-foreground">Analiz tamamlanmadan durduruldu.</p>
            )}
          </section>
          <FinishedLog runId={run.id} />
        </>
      )}
    </div>
  );
}
