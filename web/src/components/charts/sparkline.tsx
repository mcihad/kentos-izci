import type { Series } from "@/gen/izci/api/v1/common_pb";
import { formatNumber } from "@/lib/format";
import { seriesPoints, seriesTotal } from "@/lib/series";
import { cn } from "@/lib/utils";

/**
 * Tiny bar chart of a series. Empty buckets stay visible as faint ticks, so
 * the time axis reads as a dotted trail even when nothing happened.
 */
export function Sparkline({
  series,
  className,
  height = 24,
}: {
  series: Series | undefined;
  className?: string;
  height?: number;
}) {
  const points = seriesPoints(series);
  if (points.length === 0) return <div className={cn("h-6 w-24", className)} />;
  const max = Math.max(1, ...points.map((p) => p.v));
  const step = 4;
  return (
    <svg
      role="img"
      aria-label={`Toplam ${formatNumber(seriesTotal(series))} olay`}
      viewBox={`0 0 ${points.length * step} ${height}`}
      preserveAspectRatio="none"
      className={cn("h-6 w-24 text-muted-foreground", className)}
    >
      {points.map((p, i) => {
        const h = p.v > 0 ? Math.max(2, (p.v / max) * height) : 1;
        return (
          <rect
            key={p.t.getTime()}
            x={i * step}
            y={height - h}
            width={step - 1}
            height={h}
            className="fill-current"
            opacity={p.v > 0 ? 0.75 : 0.22}
          />
        );
      })}
    </svg>
  );
}
