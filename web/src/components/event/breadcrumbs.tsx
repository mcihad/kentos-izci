import type { JsonValue } from "@bufbuild/protobuf";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import type { Breadcrumb } from "@/gen/izci/ingest/v1/event_pb";
import { Level } from "@/gen/izci/ingest/v1/event_pb";
import { formatDateTime, formatTime, toDate } from "@/lib/format";
import { levels } from "@/lib/labels";
import { cn } from "@/lib/utils";

const categoryLabels: Record<string, string> = {
  navigation: "gezinme",
  http: "http",
  "http.client": "http",
  query: "sorgu",
  "db.sql": "sql",
  console: "konsol",
  "ui.click": "tıklama",
  "ui.input": "giriş",
  user: "kullanıcı",
  log: "log",
  error: "hata",
};

function offset(t: Date | undefined, at: Date | undefined): string {
  if (!t || !at) return "";
  const s = (t.getTime() - at.getTime()) / 1000;
  if (Math.abs(s) < 0.05) return "0 sn";
  const v = Math.abs(s) >= 100 ? Math.round(s) : Math.round(s * 10) / 10;
  return `${v > 0 ? "+" : "−"}${Math.abs(v).toLocaleString("tr-TR")} sn`;
}

/**
 * The trail the application walked before the error: breadcrumbs in time
 * order, ending with the error itself.
 */
export function BreadcrumbTrail({
  crumbs,
  eventTime,
  errorTitle,
  levelColor,
}: {
  crumbs: Breadcrumb[];
  eventTime: Date | undefined;
  errorTitle: string;
  levelColor: string;
}) {
  const [all, setAll] = useState(false);
  const shown = all ? crumbs : crumbs.slice(-10);
  const hidden = crumbs.length - shown.length;
  return (
    <div>
      {hidden > 0 ? (
        <Button
          variant="ghost"
          size="sm"
          className="mb-1 -ml-2 h-7 text-muted-foreground"
          onClick={() => setAll(true)}
        >
          Önceki {hidden} adımı göster
        </Button>
      ) : null}
      <ol className="relative">
        {shown.map((c, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: breadcrumbs have no identity
          <Crumb key={i} crumb={c} first={i === 0} eventTime={eventTime} />
        ))}
        <li className="relative grid grid-cols-[4.5rem_5rem_1fr] items-baseline gap-3 py-1.5 pl-8 text-[12.5px]">
          <span aria-hidden="true" className="absolute top-0 left-[11px] h-3.5 border-trail border-l" />
          <span
            aria-hidden="true"
            className="absolute top-[9px] left-[6.5px] size-[10px] rotate-45 rounded-[2px]"
            style={{ background: levelColor }}
          />
          <span className="text-right font-medium tabular" style={{ color: levelColor }}>
            0 sn
          </span>
          <span className="font-medium" style={{ color: levelColor }}>
            hata
          </span>
          <span className="truncate font-medium">{errorTitle}</span>
        </li>
      </ol>
    </div>
  );
}

function Crumb({
  crumb: c,
  first,
  eventTime,
}: {
  crumb: Breadcrumb;
  first: boolean;
  eventTime: Date | undefined;
}) {
  const t = toDate(c.timestamp);
  const data: JsonValue = c.data ?? null;
  const color =
    c.level === Level.ERROR || c.level === Level.FATAL || c.level === Level.WARNING
      ? levels[c.level].color
      : undefined;
  return (
    <li className="relative grid grid-cols-[4.5rem_5rem_1fr] items-baseline gap-3 py-1.5 pl-8 text-[12.5px]">
      <span
        aria-hidden="true"
        className={cn("absolute left-[11px] border-trail border-l", first ? "top-3.5" : "top-0", "bottom-0")}
      />
      <span
        aria-hidden="true"
        className="absolute top-[11px] left-[8.5px] size-1.5 rounded-full border border-muted-foreground/60 bg-background"
        style={color ? { background: color, borderColor: color } : undefined}
      />
      <time
        title={t ? formatDateTime(t) : undefined}
        className="text-right text-muted-foreground tabular"
        dateTime={t?.toISOString()}
      >
        {offset(t, eventTime) || (t ? formatTime(t) : "")}
      </time>
      <span className="truncate text-muted-foreground">
        {categoryLabels[c.category] ?? (c.category || c.type)}
      </span>
      <span className="min-w-0">
        <span className="block break-words font-mono text-[12px]">{c.message}</span>
        {data && typeof data === "object" && !Array.isArray(data) && Object.keys(data).length > 0 ? (
          <span className="mt-0.5 block font-mono text-[11.5px] text-muted-foreground">
            {Object.entries(data)
              .map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`)
              .join("  ")}
          </span>
        ) : null}
      </span>
    </li>
  );
}
