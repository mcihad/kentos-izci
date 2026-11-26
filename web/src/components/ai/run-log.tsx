import { cn } from "@kentos/components/lib/utils";
import {
  CircleAlertIcon,
  CompassIcon,
  FilePenLineIcon,
  FileTextIcon,
  FolderTreeIcon,
  ListChecksIcon,
  type LucideIcon,
  MessageSquareTextIcon,
  SearchIcon,
  SquareTerminalIcon,
  WorkflowIcon,
  WrenchIcon,
} from "lucide-react";
import { useEffect, useRef } from "react";
import { type AiLogEntry, AiLogKind } from "@/gen/izci/api/v1/ai_pb";
import { formatClock, toDate } from "@/lib/format";

const toolIcons: Record<string, LucideIcon> = {
  read: FileTextIcon,
  edit: FilePenLineIcon,
  write: FilePenLineIcon,
  patch: FilePenLineIcon,
  grep: SearchIcon,
  glob: SearchIcon,
  list: FolderTreeIcon,
  bash: SquareTerminalIcon,
  task: WorkflowIcon,
  todowrite: ListChecksIcon,
  todoread: ListChecksIcon,
};

const toolVerbs: Record<string, string> = {
  read: "Okudu",
  edit: "Düzenledi",
  write: "Yazdı",
  patch: "Düzenledi",
  grep: "Aradı",
  glob: "Dosya aradı",
  list: "Listeledi",
  bash: "Komut çalıştırdı",
  task: "Alt görev",
};

function EntryIcon({ entry }: { entry: AiLogEntry }) {
  let Icon: LucideIcon = WrenchIcon;
  let tone = "text-muted-foreground";
  switch (entry.kind) {
    case AiLogKind.SYSTEM:
      Icon = CompassIcon;
      tone = "text-brand-ink";
      break;
    case AiLogKind.TEXT:
      Icon = MessageSquareTextIcon;
      tone = "text-foreground/80";
      break;
    case AiLogKind.ERROR:
      Icon = CircleAlertIcon;
      tone = "text-level-error-ink";
      break;
    case AiLogKind.TOOL:
      Icon = toolIcons[entry.tool] ?? WrenchIcon;
      break;
  }
  return (
    <span className="relative z-10 flex size-6 shrink-0 items-center justify-center rounded-full border bg-background">
      <Icon className={cn("size-3.5", tone)} strokeWidth={1.75} />
    </span>
  );
}

/**
 * The run's log as a trail: what izci prepared, what the agent read,
 * searched and changed, and what it said.
 */
export function RunLog({ entries, live = false }: { entries: AiLogEntry[]; live?: boolean }) {
  const end = useRef<HTMLDivElement>(null);
  const follow = useRef(true);

  useEffect(() => {
    if (!live) return;
    const onScroll = () => {
      const doc = document.documentElement;
      follow.current = window.innerHeight + window.scrollY >= doc.scrollHeight - 160;
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, [live]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll when entries arrive
  useEffect(() => {
    if (live && follow.current) end.current?.scrollIntoView({ block: "nearest" });
  }, [entries.length, live]);

  return (
    <ol className="relative grid grid-cols-1 gap-0.5" aria-live={live ? "polite" : undefined}>
      <span aria-hidden="true" className="absolute top-3 bottom-3 left-[11.5px] w-px bg-trail" />
      {entries.map((e, i) => {
        if (e.kind === AiLogKind.STEP) {
          return (
            // biome-ignore lint/suspicious/noArrayIndexKey: log entries only append
            <li key={i} className="flex items-center gap-3 py-1 pl-[7px] text-xs text-muted-foreground">
              <span className="relative z-10 size-2.5 rounded-full border bg-background" />
              {e.message}
            </li>
          );
        }
        const time = toDate(e.time);
        const verb = e.kind === AiLogKind.TOOL ? (toolVerbs[e.tool] ?? e.tool) : "";
        return (
          <li
            // biome-ignore lint/suspicious/noArrayIndexKey: log entries only append
            key={i}
            className={cn("flex gap-3 py-1", i === entries.length - 1 && live && "animate-arrive rounded-md")}
          >
            <EntryIcon entry={e} />
            <div className="min-w-0 flex-1 pt-0.5">
              <p
                className={cn(
                  "text-sm leading-relaxed [overflow-wrap:anywhere]",
                  e.kind === AiLogKind.TEXT && "whitespace-pre-wrap",
                  e.kind === AiLogKind.ERROR && "text-level-error-ink",
                  e.kind === AiLogKind.SYSTEM && "font-medium",
                )}
              >
                {verb ? <span className="text-muted-foreground">{verb} </span> : null}
                <span className={cn(e.kind === AiLogKind.TOOL && "font-mono text-sm")}>{e.message}</span>
              </p>
            </div>
            <time className="tabular shrink-0 pt-0.5 text-xs text-muted-foreground">
              {time ? formatClock(time) : ""}
            </time>
          </li>
        );
      })}
      <div ref={end} />
    </ol>
  );
}
