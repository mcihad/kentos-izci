import type * as React from "react";
import { cn } from "@/lib/utils";

export type Stat = { label: string; value: React.ReactNode; note?: React.ReactNode };

/**
 * A row of headline numbers separated by hairlines. Every cell draws its
 * right and bottom edge; the grid reaches one pixel past the rounded frame,
 * which clips the outermost edges, so the lines stay continuous in any column
 * count and an unfilled last row stays empty.
 */
export function StatStrip({ stats, className }: { stats: Stat[]; className?: string }) {
  return (
    <div className={cn("overflow-hidden rounded-lg border", className)}>
      <dl
        className={cn(
          "-mr-px -mb-px grid grid-cols-2 sm:grid-cols-3",
          stats.length >= 6 ? "xl:grid-cols-6" : stats.length === 5 ? "xl:grid-cols-5" : "lg:grid-cols-4",
        )}
      >
        {stats.map((s) => (
          <div key={s.label} className="flex min-w-0 flex-col border-r border-b px-4 py-4">
            <dt className="text-sm text-muted-foreground">{s.label}</dt>
            <dd className="mt-1 font-semibold text-2xl leading-tight tabular lg:text-3xl">{s.value}</dd>
            {s.note ? <dd className="mt-1 text-sm text-muted-foreground">{s.note}</dd> : null}
          </div>
        ))}
      </dl>
    </div>
  );
}
