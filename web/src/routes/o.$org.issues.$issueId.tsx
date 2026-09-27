import { skipToken, useQuery } from "@connectrpc/connect-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { BracesIcon, ChevronDownIcon, ChevronLeftIcon, ChevronRightIcon, DownloadIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { IssueAi } from "@/components/ai/issue-ai";
import { gutter } from "@/components/app/page";
import { CodeBlock } from "@/components/common/code-block";
import { CopyButton } from "@/components/common/copy-button";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { EventView } from "@/components/event/event-view";
import { FeedbackList } from "@/components/feedback/feedback-list";
import { Activity } from "@/components/issue/activity";
import {
  IssueTitle,
  LevelBlaze,
  LevelLabel,
  PriorityBadge,
  StatusBadge,
  SubstatusBadge,
  UnhandledBadge,
} from "@/components/issue/badges";
import { Impact } from "@/components/issue/impact";
import { IssueAside } from "@/components/issue/issue-aside";
import { IssueEvents } from "@/components/issue/issue-events";
import { TriageBar } from "@/components/issue/triage-bar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { IssueStatus, StatsPeriod } from "@/gen/izci/api/v1/common_pb";
import { type EventDetail, EventPick, EventService } from "@/gen/izci/api/v1/event_pb";
import { FeedbackService } from "@/gen/izci/api/v1/feedback_pb";
import { AiState, IssueService } from "@/gen/izci/api/v1/issue_pb";
import { isNotFound } from "@/lib/errors";
import { platformLabel } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { canWrite } from "@/lib/session";
import { useHotkeys } from "@/lib/shortcuts";
import { useTriage } from "@/lib/triage";
import { cn } from "@/lib/utils";

type Tab = "event" | "events" | "impact" | "feedback" | "activity" | "ai";
type IssueSearch = { event?: string; tab?: Tab; run?: string };

const tabs: { id: Tab; label: string }[] = [
  { id: "event", label: "Olay" },
  { id: "events", label: "Tüm olaylar" },
  { id: "impact", label: "Etki" },
  { id: "feedback", label: "Geri bildirim" },
  { id: "activity", label: "Etkinlik" },
  { id: "ai", label: "Yapay zekâ" },
];

const picks: Record<string, EventPick> = {
  latest: EventPick.LATEST,
  oldest: EventPick.OLDEST,
  recommended: EventPick.RECOMMENDED,
};

export const Route = createFileRoute("/o/$org/issues/$issueId")({
  validateSearch: (s: Record<string, unknown>): IssueSearch => ({
    event: typeof s.event === "string" ? s.event : undefined,
    tab: typeof s.tab === "string" && tabs.some((t) => t.id === s.tab) ? (s.tab as Tab) : undefined,
    run: typeof s.run === "string" ? s.run : undefined,
  }),
  component: IssuePage,
});

function IssuePage() {
  const org = useOrg();
  const { issueId } = Route.useParams();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const tab = search.tab ?? "event";
  const writable = canWrite(org.role);
  const triage = useTriage();

  const issueQ = useQuery(IssueService.method.getIssue, {
    issue: issueId,
    statsPeriod: StatsPeriod.LAST_24_HOURS,
  });
  const issue = issueQ.data?.issue;
  const s = issue?.summary;
  const pick = search.event ? picks[search.event] : EventPick.RECOMMENDED;
  const eventQ = useQuery(
    EventService.method.getEvent,
    !s ? skipToken : search.event && pick === undefined ? { id: search.event } : { issueId: s.id, pick },
    { staleTime: 5 * 60_000 },
  );
  const facets = useQuery(IssueService.method.getIssueTagFacets, s ? { issueId: s.id } : skipToken, {
    staleTime: 60_000,
  });
  const feedback = useQuery(
    FeedbackService.method.listFeedback,
    s ? { issueId: s.id, pageSize: 50 } : skipToken,
    {
      staleTime: 60_000,
    },
  );
  const feedbackCount = feedback.data?.feedback.length ?? 0;

  useEffect(() => {
    if (s) document.title = `${s.shortId} ${s.type || s.title} — İzci`;
    return () => {
      document.title = "İzci";
    };
  }, [s]);

  const goEvent = (id: string | undefined) =>
    id && void navigate({ search: (p) => ({ ...p, event: id, tab: undefined }) });
  useHotkeys({
    r: () =>
      writable &&
      s &&
      void (s.status === IssueStatus.UNRESOLVED ? triage.resolve([s.id]) : triage.reopen([s.id])),
    i: () => writable && s && s.status === IssueStatus.UNRESOLVED && void triage.ignore([s.id]),
    a: () => writable && s && void triage.assign([s.id], triage.me),
    "[": () => goEvent(eventQ.data?.previousId),
    "]": () => goEvent(eventQ.data?.nextId),
  });

  if (issueQ.isPending) return <IssueSkeleton />;
  if (issueQ.error || !issue || !s) {
    return isNotFound(issueQ.error) ? (
      <EmptyState
        title="Hata bulunamadı"
        className="py-24"
        action={
          <Button asChild variant="outline" size="sm">
            <Link to="/o/$org/issues" params={{ org: org.slug }}>
              Hata listesine dön
            </Link>
          </Button>
        }
      >
        <span className="font-mono">{issueId}</span> kimlikli bir hata yok ya da silinmiş.
      </EmptyState>
    ) : (
      <ErrorState error={issueQ.error} onRetry={() => void issueQ.refetch()} className="py-24" />
    );
  }

  return (
    <div>
      <header className="border-b">
        <div className={cn("pt-4 pb-4", gutter)}>
          <nav className="flex items-center gap-1.5 text-[12.5px] text-muted-foreground" aria-label="Konum">
            <Link to="/o/$org/issues" params={{ org: org.slug }} className="hover:text-foreground">
              Hatalar
            </Link>
            <ChevronRightIcon className="size-3.5" />
            <span className="font-medium font-mono text-foreground">{s.shortId}</span>
            <CopyButton value={s.permalink} label="Bağlantıyı kopyala" className="size-6" />
          </nav>
          <div className="mt-2 flex gap-3">
            <LevelBlaze level={s.level} className="w-1 self-stretch" />
            <div className="min-w-0 flex-1">
              <h1 className="break-words text-[19px] leading-snug tracking-[-0.01em]">
                <IssueTitle type={s.type} value={s.value} title={s.title} />
              </h1>
              {s.culprit ? (
                <p className="mt-1 truncate font-mono text-[12.5px] text-muted-foreground">{s.culprit}</p>
              ) : null}
              <div className="mt-2.5 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[12.5px] text-muted-foreground">
                <StatusBadge status={s.status} />
                <SubstatusBadge substatus={s.substatus} />
                {s.unhandled ? <UnhandledBadge /> : null}
                <PriorityBadge priority={s.priority} />
                <LevelLabel level={s.level} />
                <Link
                  to="/o/$org/projects/$slug"
                  params={{ org: org.slug, slug: s.project?.slug ?? "" }}
                  className="hover:text-foreground"
                >
                  {s.project?.name}
                </Link>
                <span>{platformLabel(s.platform)}</span>
              </div>
            </div>
          </div>
          <div className="mt-4">
            <TriageBar issue={issue} writable={writable} />
          </div>
        </div>
        <nav className={cn("-mb-px flex gap-5 overflow-x-auto text-[13px]", gutter)} aria-label="Bölümler">
          {tabs.map((t) => (
            <Link
              key={t.id}
              from={Route.fullPath}
              search={(p) => ({ ...p, tab: t.id === "event" ? undefined : t.id, run: undefined })}
              className={cn(
                "whitespace-nowrap border-b-2 pb-2.5 transition-colors",
                tab === t.id
                  ? "border-primary font-medium text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {t.label}
              {t.id === "feedback" && feedbackCount > 0 ? (
                <span className="ml-1.5 rounded-full bg-muted px-1.5 py-px text-[11px] text-muted-foreground tabular">
                  {feedbackCount >= 50 ? "50+" : feedbackCount}
                </span>
              ) : null}
              {t.id === "ai" && s.aiState === AiState.RUNNING ? (
                <span className="ml-1.5 inline-block size-1.5 animate-pulse rounded-full bg-primary align-middle" />
              ) : null}
            </Link>
          ))}
        </nav>
      </header>

      <div className="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_17rem] xl:grid-cols-[minmax(0,1fr)_20rem]">
        <div className={cn("min-w-0", gutter)}>
          {tab === "event" ? (
            <>
              <EventToolbar
                detail={eventQ.data?.event}
                previousId={eventQ.data?.previousId}
                nextId={eventQ.data?.nextId}
                current={search.event}
              />
              {eventQ.isPending ? (
                <div className="space-y-3 py-6">
                  <Skeleton className="h-5 w-1/2" />
                  <Skeleton className="h-40 w-full" />
                </div>
              ) : eventQ.error ? (
                <ErrorState error={eventQ.error} onRetry={() => void eventQ.refetch()} />
              ) : eventQ.data?.event ? (
                <EventView key={eventQ.data.event.id} detail={eventQ.data.event} />
              ) : null}
            </>
          ) : tab === "events" ? (
            <IssueEvents issueId={s.id} shortId={s.shortId} />
          ) : tab === "impact" ? (
            <Impact facets={facets.data?.facets} />
          ) : tab === "feedback" ? (
            <div className="py-2">
              <FeedbackList issueId={s.id} showIssue={false} />
            </div>
          ) : tab === "ai" ? (
            <IssueAi
              issue={s}
              eventId={search.event}
              runId={search.run}
              onSelectRun={(run) => void navigate({ search: (p) => ({ ...p, run }), replace: true })}
              writable={writable}
            />
          ) : (
            <Activity issueId={s.id} writable={writable} />
          )}
        </div>
        <aside className="border-t px-4 py-5 sm:px-6 lg:border-t-0 lg:border-l lg:px-5">
          <IssueAside issue={issue} facets={facets.data?.facets} />
        </aside>
      </div>
    </div>
  );
}

function EventToolbar({
  detail,
  previousId,
  nextId,
  current,
}: {
  detail: EventDetail | undefined;
  previousId: string | undefined;
  nextId: string | undefined;
  current: string | undefined;
}) {
  const [raw, setRaw] = useState(false);
  const ev = detail?.event;
  const which = current && current in picks ? current : current ? "" : "recommended";
  const labels: Record<string, string> = {
    recommended: "Önerilen olay",
    latest: "En yeni olay",
    oldest: "En eski olay",
  };
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b py-2.5 text-[12.5px]">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" className="-ml-2 h-7 gap-1 px-2 font-medium text-[13px]">
            {labels[which] ?? "Seçili olay"}
            <ChevronDownIcon className="size-3.5 text-muted-foreground" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          {Object.entries(labels).map(([k, label]) => (
            <DropdownMenuItem key={k} asChild>
              <Link
                from={Route.fullPath}
                search={(p) => ({ ...p, event: k === "recommended" ? undefined : k })}
              >
                {label}
              </Link>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      {detail ? (
        <>
          <RelativeTime value={detail.receivedAt} className="text-muted-foreground" />
          {ev?.environment ? <span className="text-muted-foreground">{ev.environment}</span> : null}
          {ev?.release ? <span className="font-mono text-muted-foreground">{ev.release}</span> : null}
        </>
      ) : null}
      <div className="ml-auto flex items-center gap-1">
        <Button variant="ghost" size="sm" className="h-7 px-2" disabled={!previousId} asChild={!!previousId}>
          {previousId ? (
            <Link
              from={Route.fullPath}
              search={(p) => ({ ...p, event: previousId })}
              aria-label="Önceki olay"
            >
              <ChevronLeftIcon className="size-4" />
              Önceki
            </Link>
          ) : (
            <span className="inline-flex items-center gap-1">
              <ChevronLeftIcon className="size-4" />
              Önceki
            </span>
          )}
        </Button>
        <Button variant="ghost" size="sm" className="h-7 px-2" disabled={!nextId} asChild={!!nextId}>
          {nextId ? (
            <Link from={Route.fullPath} search={(p) => ({ ...p, event: nextId })} aria-label="Sonraki olay">
              Sonraki
              <ChevronRightIcon className="size-4" />
            </Link>
          ) : (
            <span className="inline-flex items-center gap-1">
              Sonraki
              <ChevronRightIcon className="size-4" />
            </span>
          )}
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="ml-1 h-7 px-2"
          disabled={!detail}
          onClick={() => setRaw(true)}
        >
          <BracesIcon className="size-3.5" />
          Ham veri
        </Button>
      </div>
      {detail ? <RawEventDialog id={detail.id} open={raw} onOpenChange={setRaw} /> : null}
    </div>
  );
}

function RawEventDialog({
  id,
  open,
  onOpenChange,
}: {
  id: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const raw = useQuery(EventService.method.getRawEvent, open ? { id } : skipToken, {
    staleTime: Number.POSITIVE_INFINITY,
  });
  const download = () => {
    if (!raw.data) return;
    const url = URL.createObjectURL(new Blob([raw.data.json], { type: "application/json" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `izci-olay-${id}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Ham olay verisi</DialogTitle>
          <DialogDescription>Olayın normalize edilmiş ve hassas alanları gizlenmiş hâli.</DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1">
          {raw.data ? (
            <CodeBlock code={raw.data.json} lang="json" maxHeight="60vh" />
          ) : (
            <Skeleton className="h-64 w-full" />
          )}
        </div>
        <div className="flex justify-end">
          <Button variant="outline" size="sm" onClick={download} disabled={!raw.data}>
            <DownloadIcon className="size-4" />
            JSON olarak indir
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function IssueSkeleton() {
  return (
    <div className={cn("space-y-4 py-6", gutter)}>
      <Skeleton className="h-4 w-40" />
      <Skeleton className="h-7 w-3/4" />
      <Skeleton className="h-4 w-1/3" />
      <div className="flex gap-2">
        <Skeleton className="h-8 w-20" />
        <Skeleton className="h-8 w-24" />
        <Skeleton className="h-8 w-24" />
      </div>
      <Skeleton className="mt-6 h-64 w-full" />
    </div>
  );
}
