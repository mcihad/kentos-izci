import { useEffect, useRef, useState } from "react";
import { formatCompact, formatNumber, formatShortDate, formatTime } from "@/lib/format";
import type { Point } from "@/lib/series";
import { cn } from "@/lib/utils";

export type BarLayer = { key: string; label: string; color: string; points: Point[] };

function bucketLabel(t: Date, step: number): string {
  if (step >= 86_400_000) return formatShortDate(t);
  if (step >= 6 * 3_600_000) return `${formatShortDate(t)} ${formatTime(t)}`;
  return formatTime(t);
}

function tickLabel(t: Date, step: number, span: number): string {
  if (step >= 86_400_000 || span > 2 * 86_400_000) return formatShortDate(t);
  return formatTime(t);
}

/**
 * Stacked bar chart over uniform time buckets (the first layer sits at the
 * bottom). Hovering a bucket shows its values.
 */
export function StackedBars({
  layers,
  step,
  height = 180,
  className,
}: {
  layers: BarLayer[];
  step: number;
  height?: number;
  className?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [hover, setHover] = useState<number | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => setWidth(entry?.contentRect.width ?? 0));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const buckets = Math.max(0, ...layers.map((l) => l.points.length));
  const times = layers.find((l) => l.points.length === buckets)?.points.map((p) => p.t) ?? [];
  const totals = Array.from({ length: buckets }, (_, i) =>
    layers.reduce((a, l) => a + (l.points[i]?.v ?? 0), 0),
  );
  const max = Math.max(1, ...totals);
  const top = niceCeil(max);
  // Room for the top gridline label and the time axis.
  const padTop = 14;
  const plotH = height - 22 - padTop;
  const band = buckets > 0 ? width / buckets : 0;
  const gap = band > 8 ? 2 : band > 4 ? 1 : 0.5;
  const span = buckets * step;
  const tickEvery = Math.max(1, Math.ceil(buckets / Math.max(2, Math.floor(width / 90))));

  return (
    <div ref={ref} className={cn("relative select-none", className)} style={{ height }}>
      {width > 0 && buckets > 0 ? (
        <svg
          width={width}
          height={height}
          role="img"
          aria-label={`Toplam ${formatNumber(totals.reduce((a, b) => a + b, 0))} olay`}
          onPointerMove={(e) => {
            const x = e.clientX - e.currentTarget.getBoundingClientRect().left;
            setHover(Math.min(buckets - 1, Math.max(0, Math.floor(x / band))));
          }}
          onPointerLeave={() => setHover(null)}
          className="overflow-visible"
        >
          <g transform={`translate(0 ${padTop})`}>
            {[0.5, 1].map((f) => (
              <g key={f}>
                <line
                  x1={0}
                  x2={width}
                  y1={plotH - plotH * f}
                  y2={plotH - plotH * f}
                  className="stroke-border"
                  strokeDasharray="2 4"
                />
                <text x={0} y={plotH - plotH * f - 4} className="fill-muted-foreground text-[10.5px] tabular">
                  {formatCompact(top * f)}
                </text>
              </g>
            ))}
            <line x1={0} x2={width} y1={plotH + 0.5} y2={plotH + 0.5} className="stroke-border" />
            {hover !== null ? (
              <rect
                x={hover * band}
                y={-padTop}
                width={band}
                height={plotH + padTop}
                className="fill-foreground/5"
              />
            ) : null}
            {times.map((t, i) => {
              let y = plotH;
              return (
                <g key={t.getTime()}>
                  {layers.map((l) => {
                    const v = l.points[i]?.v ?? 0;
                    if (v <= 0) return null;
                    const h = Math.max(1, (v / top) * plotH);
                    y -= h;
                    return (
                      <rect
                        key={l.key}
                        x={i * band + gap / 2}
                        y={y}
                        width={Math.max(0.5, band - gap)}
                        height={h}
                        fill={l.color}
                        opacity={hover === null || hover === i ? 0.9 : 0.55}
                      />
                    );
                  })}
                  {i % tickEvery === 0 ? (
                    <text
                      x={i * band + band / 2}
                      y={height - padTop - 4}
                      textAnchor={i === 0 ? "start" : "middle"}
                      className="fill-muted-foreground text-[10.5px] tabular"
                    >
                      {tickLabel(t, step, span)}
                    </text>
                  ) : null}
                </g>
              );
            })}
          </g>
        </svg>
      ) : null}
      {hover !== null && times[hover] ? (
        <div
          className="pointer-events-none absolute top-1 z-10 min-w-40 rounded-md border bg-popover px-2.5 py-2 text-[12px] shadow-md"
          style={
            hover * band + band / 2 > width / 2
              ? { right: width - hover * band + 8 }
              : { left: hover * band + band + 8 }
          }
        >
          <div className="mb-1.5 font-medium">{bucketLabel(times[hover], step)}</div>
          {layers.map((l) => (
            <div key={l.key} className="flex items-center gap-2 py-0.5">
              <span className="size-2 rounded-[2px]" style={{ background: l.color }} />
              <span className="flex-1 text-muted-foreground">{l.label}</span>
              <span className="tabular">{formatNumber(l.points[hover]?.v ?? 0)}</span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

/** Rounds up to 1, 2 or 5 times a power of ten. */
function niceCeil(v: number): number {
  const p = 10 ** Math.floor(Math.log10(v));
  for (const m of [1, 2, 5, 10]) if (m * p >= v) return m * p;
  return 10 * p;
}
