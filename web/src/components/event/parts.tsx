import type { JsonValue } from "@bufbuild/protobuf";
import { cn } from "@kentos/components/lib/utils";
import { ChevronRightIcon } from "lucide-react";
import type * as React from "react";
import { useState } from "react";

/** A titled block of the event view. */
export function EventSection({
  title,
  aside,
  children,
  className,
}: {
  title: React.ReactNode;
  aside?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("border-b py-6 last:border-b-0", className)}>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-2.5">
        <h3 className="font-semibold text-lg leading-snug">{title}</h3>
        {aside ? <div className="flex flex-wrap items-center gap-3">{aside}</div> : null}
      </div>
      {children}
    </section>
  );
}

export type KV = [label: string, value: React.ReactNode, opts?: { mono?: boolean }];

/** Label/value pairs; empty values are skipped. */
export function KVList({ items, className }: { items: KV[]; className?: string }) {
  const visible = items.filter(([, v]) => v !== undefined && v !== null && v !== "" && v !== false);
  if (visible.length === 0) return null;
  return (
    <dl
      className={cn(
        "grid grid-cols-[minmax(6.5rem,max-content)_1fr] gap-x-5 gap-y-2 text-md sm:grid-cols-[minmax(8rem,max-content)_1fr]",
        className,
      )}
    >
      {visible.map(([label, value, opts]) => (
        <div key={label} className="contents">
          <dt className="text-sm text-muted-foreground leading-[1.4375rem]">{label}</dt>
          <dd className={cn("min-w-0 break-words", opts?.mono && "font-mono text-sm")}>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** A bordered group inside the context grid. */
export function Panel({
  title,
  children,
  className,
}: {
  title: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("min-w-0 rounded-lg border px-4 py-4", className)}>
      <h4 className="mb-3 font-semibold text-md">{title}</h4>
      {children}
    </div>
  );
}

/** Horizontal meter for percentages (CPU, memory). */
export function Meter({ value, label }: { value: number; label: string }) {
  const v = Math.max(0, Math.min(100, value));
  const color = v >= 90 ? "var(--level-error)" : v >= 70 ? "var(--level-warning)" : "var(--primary)";
  return (
    <div className="grid gap-1">
      <div className="flex items-baseline justify-between text-sm">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-medium tabular">
          %{v.toLocaleString("tr-TR", { maximumFractionDigits: 1 })}
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full" style={{ width: `${v}%`, background: color }} />
      </div>
    </div>
  );
}

/** Collapsible JSON view. */
export function JsonTree({ value, depth = 0 }: { value: JsonValue; depth?: number }) {
  if (value === null) return <span className="text-level-warning-ink">null</span>;
  if (typeof value === "boolean") return <span className="text-level-warning-ink">{String(value)}</span>;
  if (typeof value === "number") return <span className="text-level-info-ink tabular">{value}</span>;
  if (typeof value === "string") return <span className="break-all text-foreground">"{value}"</span>;
  const entries: [string, JsonValue][] = Array.isArray(value)
    ? value.map((v, i) => [String(i), v])
    : Object.entries(value as Record<string, JsonValue>);
  return <JsonNode entries={entries} array={Array.isArray(value)} depth={depth} />;
}

function JsonNode({
  entries,
  array,
  depth,
}: {
  entries: [string, JsonValue][];
  array: boolean;
  depth: number;
}) {
  const [open, setOpen] = useState(depth < 2);
  if (entries.length === 0) return <span className="text-muted-foreground">{array ? "[]" : "{}"}</span>;
  return (
    <span>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="-my-0.5 inline-flex items-center py-0.5 text-muted-foreground hover:text-foreground"
      >
        <ChevronRightIcon className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        {array ? `[${entries.length}]` : `{${entries.length}}`}
      </button>
      {open ? (
        <span className="block border-trail border-l pl-3" style={{ marginLeft: 5 }}>
          {entries.map(([k, v]) => (
            <span key={k} className="block">
              <span className="text-muted-foreground">{array ? `${k}: ` : `${k}: `}</span>
              <JsonTree value={v} depth={depth + 1} />
            </span>
          ))}
        </span>
      ) : null}
    </span>
  );
}
