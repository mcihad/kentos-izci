import { ChevronRightIcon, FileCodeIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { CodeLines } from "@/components/common/code-block";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { EventDetail } from "@/gen/izci/api/v1/event_pb";
import type { Frame } from "@/gen/izci/ingest/v1/event_pb";
import { languageOf } from "@/lib/highlight";
import { cn } from "@/lib/utils";
import { basename, sourceUrl } from "./source";

type Item =
  | { kind: "frame"; frame: Frame; index: number; crash: boolean }
  | { kind: "gap"; frames: Frame[]; key: string };

/**
 * A stack trace drawn as a trail: in-app frames are marked with the accent
 * "blaze", library frames with hollow dots, and the crash site with a mark
 * in the event's level color. Hidden library frames leave a dashed stretch.
 */
export function StackTrail({
  frames,
  detail,
  platform,
  levelColor,
  appOnly,
  newestFirst,
}: {
  frames: Frame[];
  detail?: EventDetail;
  platform: string;
  levelColor: string;
  appOnly: boolean;
  newestFirst: boolean;
}) {
  const hasApp = frames.some((f) => f.inApp === true);
  const items = useMemo<Item[]>(() => {
    const ordered = frames.map((frame, index) => ({ frame, index }));
    if (newestFirst) ordered.reverse();
    const crashIndex = frames.length - 1;
    const out: Item[] = [];
    let gap: Frame[] = [];
    const flush = (key: string) => {
      if (gap.length) out.push({ kind: "gap", frames: gap, key });
      gap = [];
    };
    for (const { frame, index } of ordered) {
      const hide = appOnly && hasApp && frame.inApp !== true && index !== crashIndex;
      if (hide) {
        gap.push(frame);
        continue;
      }
      flush(`gap-${index}`);
      out.push({ kind: "frame", frame, index, crash: index === crashIndex });
    }
    flush("gap-end");
    return out;
  }, [frames, appOnly, hasApp, newestFirst]);

  // The crash site (or, without code, the newest in-app frame) starts open.
  const firstOpen = useMemo(() => {
    const withCode = items.find(
      (i) => i.kind === "frame" && i.frame.contextLine && (i.crash || i.frame.inApp),
    );
    return withCode?.kind === "frame" ? withCode.index : -1;
  }, [items]);
  const [open, setOpen] = useState<Set<number>>(() => new Set(firstOpen >= 0 ? [firstOpen] : []));
  const [revealed, setRevealed] = useState<Set<string>>(new Set());

  if (frames.length === 0) {
    return <p className="text-sm text-muted-foreground">Bu istisna için yığın izi gönderilmedi.</p>;
  }

  return (
    <ol className="animate-trail relative">
      {items.map((item, i) => {
        const first = i === 0;
        const last = i === items.length - 1;
        if (item.kind === "gap" && !revealed.has(item.key)) {
          return (
            <li key={item.key} className="relative pl-8">
              <Rail first={first} last={last} dashed />
              <button
                type="button"
                onClick={() => setRevealed((s) => new Set(s).add(item.key))}
                className="flex h-10 items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
              >
                <ChevronRightIcon className="size-4" />
                {item.frames.length} kütüphane çerçevesi
              </button>
            </li>
          );
        }
        const rows =
          item.kind === "gap"
            ? item.frames.map((frame) => ({ frame, index: frames.indexOf(frame), crash: false }))
            : [{ frame: item.frame, index: item.index, crash: item.crash }];
        return rows.map((r, j) => (
          <FrameRow
            key={r.index}
            frame={r.frame}
            crash={r.crash}
            first={first && j === 0}
            last={last && j === rows.length - 1}
            open={open.has(r.index)}
            onToggle={() =>
              setOpen((s) => {
                const n = new Set(s);
                if (n.has(r.index)) n.delete(r.index);
                else n.add(r.index);
                return n;
              })
            }
            detail={detail}
            platform={platform}
            levelColor={levelColor}
          />
        ));
      })}
    </ol>
  );
}

function Rail({ first, last, dashed = false }: { first: boolean; last: boolean; dashed?: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "absolute left-[11px] w-0 border-l",
        dashed ? "border-trail border-dashed" : "border-trail",
        first ? "top-[17px]" : "top-0",
        last ? "h-[17px]" : "bottom-0",
      )}
    />
  );
}

function Marker({ kind, color }: { kind: "crash" | "app" | "lib"; color: string }) {
  if (kind === "crash") {
    return (
      <span
        aria-hidden="true"
        className="absolute top-[12px] left-[6.5px] size-[10px] rotate-45 rounded-[2px]"
        style={{ background: color, boxShadow: `0 0 0 3px color-mix(in oklch, ${color} 22%, transparent)` }}
      />
    );
  }
  if (kind === "app") {
    return (
      <span aria-hidden="true" className="absolute top-[13px] left-[7.5px] size-2 rounded-full bg-primary" />
    );
  }
  return (
    <span
      aria-hidden="true"
      className="absolute top-[14px] left-[8.5px] size-1.5 rounded-full border border-muted-foreground/60 bg-background"
    />
  );
}

function FrameRow({
  frame,
  crash,
  first,
  last,
  open,
  onToggle,
  detail,
  platform,
  levelColor,
}: {
  frame: Frame;
  crash: boolean;
  first: boolean;
  last: boolean;
  open: boolean;
  onToggle: () => void;
  detail?: EventDetail;
  platform: string;
  levelColor: string;
}) {
  const app = frame.inApp === true;
  const hasCode = !!frame.contextLine;
  const hasVars = Object.keys(frame.vars).length > 0;
  const expandable = hasCode || hasVars;
  const file = frame.filename || frame.absPath;
  const where = file
    ? `${file}${frame.lineno ? `:${frame.lineno}` : ""}${frame.colno ? `:${frame.colno}` : ""}`
    : "";
  const link = sourceUrl(frame, detail);
  const fn = frame.function || frame.rawFunction || frame.symbol || "?";
  const lines = [...frame.preContext, frame.contextLine, ...frame.postContext];

  return (
    <li className="relative pl-8">
      <Rail first={first} last={last} />
      <Marker kind={crash ? "crash" : app ? "app" : "lib"} color={levelColor} />
      <div className="flex min-w-0 items-start gap-2 py-2">
        <button
          type="button"
          disabled={!expandable}
          onClick={onToggle}
          aria-expanded={expandable ? open : undefined}
          className="group flex min-w-0 flex-1 items-start gap-1.5 text-left disabled:cursor-default"
        >
          <ChevronRightIcon
            className={cn(
              "mt-[3px] size-4 shrink-0 text-muted-foreground transition-transform",
              open && "rotate-90",
              !expandable && "invisible",
            )}
          />
          <span className="min-w-0 flex-1">
            <span
              className={cn(
                "block font-mono text-sm [overflow-wrap:anywhere] sm:truncate",
                app || crash ? "font-medium text-foreground" : "text-muted-foreground",
              )}
            >
              {fn}
            </span>
            {where || frame.module || frame.package ? (
              <span className="mt-0.5 block font-mono text-sm text-muted-foreground [overflow-wrap:anywhere] sm:truncate">
                {where || frame.module || frame.package}
              </span>
            ) : null}
          </span>
        </button>
        {file ? (
          <span className="hidden shrink-0 pt-px font-mono text-sm text-muted-foreground xl:inline">
            {basename(file)}
            {frame.lineno ? `:${frame.lineno}` : ""}
          </span>
        ) : null}
        {link ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <a
                href={link}
                target="_blank"
                rel="noreferrer"
                aria-label="Kaynağı depoda aç"
                className="-my-1.5 grid size-8 shrink-0 place-items-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                <FileCodeIcon className="size-4" />
              </a>
            </TooltipTrigger>
            <TooltipContent>Kaynağı depoda aç</TooltipContent>
          </Tooltip>
        ) : null}
      </div>
      {open && expandable ? (
        <div className="mb-2 ml-5 space-y-2">
          {hasCode ? (
            <CodeLines
              lines={lines}
              start={frame.lineno ? frame.lineno - frame.preContext.length : 1}
              mark={frame.preContext.length}
              markColor={crash ? levelColor : "var(--primary)"}
              lang={languageOf(file, frame.platform || platform)}
            />
          ) : null}
          {hasVars ? (
            <dl className="grid grid-cols-[minmax(6rem,auto)_1fr] gap-x-4 gap-y-1.5 rounded-md border px-3 py-2.5 font-mono text-sm">
              {Object.entries(frame.vars).map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="text-muted-foreground">{k}</dt>
                  <dd className="break-all">{v}</dd>
                </div>
              ))}
            </dl>
          ) : null}
        </div>
      ) : null}
    </li>
  );
}
