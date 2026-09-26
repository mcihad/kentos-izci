import type * as React from "react";
import { cn } from "@/lib/utils";

export type Stat = { label: string; value: React.ReactNode; note?: React.ReactNode };

/** A row of headline numbers separated by hairlines. */
export function StatStrip({ stats, className }: { stats: Stat[]; className?: string }) {
  return (
    <dl
      className={cn(
        "grid grid-cols-2 divide-x divide-y overflow-hidden rounded-lg border sm:grid-cols-3 xl:divide-y-0",
        stats.length >= 6 ? "xl:grid-cols-6" : stats.length === 5 ? "xl:grid-cols-5" : "lg:grid-cols-4",
        className,
      )}
    >
      {stats.map((s) => (
        <div key={s.label} className="min-w-0 px-4 py-3.5">
          <dt className="truncate text-[12.5px] text-muted-foreground">{s.label}</dt>
          <dd className="mt-1 font-semibold text-[22px] leading-tight tracking-tight tabular">{s.value}</dd>
          {s.note ? <dd className="mt-0.5 truncate text-[12px] text-muted-foreground">{s.note}</dd> : null}
        </div>
      ))}
    </dl>
  );
}
