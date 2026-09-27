import type * as React from "react";
import { cn } from "@/lib/utils";

export type Stat = { label: string; value: React.ReactNode; note?: React.ReactNode };

/** Headline numbers, each on its own card (the design system's StatCard). */
export function StatStrip({ stats, className }: { stats: Stat[]; className?: string }) {
  return (
    <dl
      className={cn(
        "grid grid-cols-2 gap-3 sm:grid-cols-3",
        stats.length >= 6 ? "xl:grid-cols-6" : stats.length === 5 ? "xl:grid-cols-5" : "lg:grid-cols-4",
        className,
      )}
    >
      {stats.map((s) => (
        <div key={s.label} className="card flex min-w-0 flex-col px-4 py-3.5 sm:p-(--card-p)">
          <dt className="font-medium text-muted-foreground text-sm">{s.label}</dt>
          <dd className="mt-2 font-semibold text-2xl text-strong leading-none tracking-(--tracking-tight) tabular lg:text-3xl">
            {s.value}
          </dd>
          {s.note ? <dd className="mt-2 text-muted-foreground text-sm">{s.note}</dd> : null}
        </div>
      ))}
    </dl>
  );
}
