import { createClient } from "@connectrpc/connect";
import { PageAction } from "@kentos/components/layout/page";
import { cn } from "@kentos/components/lib/utils";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { createFileRoute, Link } from "@tanstack/react-router";
import { PauseIcon, PlayIcon, RadioIcon } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { ProjectFilter } from "@/components/app/filters";
import { gutter, Page } from "@/components/app/page";
import { EmptyState } from "@/components/common/states";
import { IssueTitle, LevelBlaze, pill, tones } from "@/components/issue/badges";
import { DashboardService, LiveEventKind, type WatchLiveResponse } from "@/gen/izci/api/v1/dashboard_pb";
import { Level } from "@/gen/izci/ingest/v1/event_pb";
import { formatTime, toDate } from "@/lib/format";
import { useOrg } from "@/lib/org";

export const Route = createFileRoute("/o/$org/live")({
  component: LivePage,
});

type Row = WatchLiveResponse & { key: string };

const kindLabels: Partial<Record<LiveEventKind, { label: string; className: string }>> = {
  [LiveEventKind.ISSUE_CREATED]: { label: "Yeni hata", className: tones.info },
  [LiveEventKind.ISSUE_REGRESSED]: {
    label: "Geri döndü",
    className: tones.warning,
  },
  [LiveEventKind.ISSUE_UPDATED]: { label: "Güncellendi", className: tones.neutral },
};

const minLevels: { value: string; label: string; level: Level }[] = [
  { value: "all", label: "Tüm seviyeler", level: Level.UNSPECIFIED },
  { value: "warning", label: "Uyarı ve üstü", level: Level.WARNING },
  { value: "error", label: "Hata ve üstü", level: Level.ERROR },
  { value: "fatal", label: "Yalnız ölümcül", level: Level.FATAL },
];

type Status = "connecting" | "live" | "retrying";

function LivePage() {
  const org = useOrg();
  const [projects, setProjects] = useState<string[]>([]);
  const [min, setMin] = useState("all");
  const [paused, setPaused] = useState(false);
  const [rows, setRows] = useState<Row[]>([]);
  const [status, setStatus] = useState<Status>("connecting");
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const minLevel = minLevels.find((m) => m.value === min)?.level ?? Level.UNSPECIFIED;

  useEffect(() => {
    const client = createClient(DashboardService, org.transport);
    const abort = new AbortController();
    let attempt = 0;
    let seq = 0;
    (async () => {
      while (!abort.signal.aborted) {
        try {
          setStatus(attempt === 0 ? "connecting" : "retrying");
          for await (const msg of client.watchLive({ projects, minLevel }, { signal: abort.signal })) {
            attempt = 0;
            setStatus("live");
            if (msg.kind === LiveEventKind.HEARTBEAT || pausedRef.current) continue;
            seq++;
            const row = Object.assign(msg, { key: `${seq}-${msg.event?.id ?? msg.issue?.id ?? ""}` });
            setRows((r) => [row, ...r].slice(0, 200));
          }
        } catch {
          // Reconnect below unless the page went away.
        }
        if (abort.signal.aborted) return;
        attempt++;
        setStatus("retrying");
        await new Promise((r) => setTimeout(r, Math.min(30_000, 1000 * 2 ** Math.min(attempt, 5))));
      }
    })();
    return () => abort.abort();
  }, [org.transport, projects, minLevel]);

  const perMinute = useMemo(() => {
    const since = Date.now() - 60_000;
    return rows.filter(
      (r) => (toDate(r.time)?.getTime() ?? 0) >= since && r.kind !== LiveEventKind.ISSUE_UPDATED,
    ).length;
  }, [rows]);

  return (
    <Page
      icon={RadioIcon}
      title="Canlı akış"
      description={
        <span className="inline-flex items-center gap-2">
          <span
            className={cn(
              "size-2 rounded-full",
              status === "live"
                ? "animate-pulse bg-primary"
                : status === "retrying"
                  ? "bg-level-warning"
                  : "bg-muted-foreground",
            )}
          />
          {status === "live"
            ? `Bağlı, son dakikada ${perMinute} olay`
            : status === "retrying"
              ? "Bağlantı koptu, yeniden bağlanılıyor…"
              : "Bağlanıyor…"}
        </span>
      }
      filters={
        <>
          <ProjectFilter value={projects} onChange={setProjects} />
          <Select value={min} onValueChange={setMin}>
            <SelectTrigger size="sm" className="text-sm" aria-label="En düşük seviye">
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              {minLevels.map((m) => (
                <SelectItem key={m.value} value={m.value}>
                  {m.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </>
      }
      actions={
        <PageAction
          icon={paused ? PlayIcon : PauseIcon}
          label={paused ? "Sürdür" : "Duraklat"}
          description={paused ? "Yeni olayları yeniden göster" : "Akışı durdurup olayları incele"}
          onClick={() => setPaused(!paused)}
        />
      }
    >
      <div className={gutter}>
        {rows.length === 0 ? (
          <EmptyState icon={RadioIcon} title="Olay bekleniyor" className="card py-16">
            Uygulamalarınızdan gelen her olay ve hata güncellemesi burada anında görünür. Denemek için bir
            projeden test olayı gönderin.
          </EmptyState>
        ) : (
          <ol className="card overflow-hidden max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0 [&>li:last-child]:border-b-0">
            {rows.map((r) => {
              const issue = r.issue;
              const kind = kindLabels[r.kind];
              const t = toDate(r.time);
              return (
                <li key={r.key} className="animate-arrive relative border-b">
                  <Link
                    to="/o/$org/issues/$issueId"
                    params={{ org: org.slug, issueId: issue?.shortId ?? "" }}
                    search={r.event ? { event: r.event.id } : {}}
                    className="flex items-stretch gap-3 px-4 py-2.5 transition-colors duration-(--duration-fast) hover:bg-(--table-row-hover) sm:px-(--card-p)"
                  >
                    <LevelBlaze level={r.event?.level ?? issue?.level ?? Level.ERROR} />
                    <time className="w-16 shrink-0 pt-0.5 font-mono text-xs text-muted-foreground tabular">
                      {t ? `${formatTime(t)}:${String(t.getSeconds()).padStart(2, "0")}` : ""}
                    </time>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm">
                        {issue ? (
                          <IssueTitle type={issue.type} value={issue.value} title={issue.title} />
                        ) : null}
                      </p>
                      <p className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                        <span className="font-mono text-xs text-foreground/80">{issue?.shortId}</span>
                        {kind ? <span className={cn(pill, kind.className)}>{kind.label}</span> : null}
                        <span>{issue?.project?.slug}</span>
                        {r.event?.customer ? (
                          <span>{r.event.customer.name || r.event.customer.key}</span>
                        ) : null}
                        {r.event?.environment ? <span>{r.event.environment}</span> : null}
                        {r.event?.release ? <span className="font-mono">{r.event.release}</span> : null}
                        {r.event?.user ? <span>{r.event.user}</span> : null}
                      </p>
                    </div>
                  </Link>
                </li>
              );
            })}
          </ol>
        )}
      </div>
    </Page>
  );
}
