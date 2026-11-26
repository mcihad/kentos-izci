import type { Point } from "@kentos/components/charts/stacked-bars";
import type { Series } from "@/gen/izci/api/v1/common_pb";
import { toDate } from "./format";

export type { Point };

/** Expands a Series into timestamped points. */
export function seriesPoints(s: Series | undefined): Point[] {
  if (!s) return [];
  const start = toDate(s.start)?.getTime() ?? 0;
  const step = s.interval ? Number(s.interval.seconds) * 1000 + s.interval.nanos / 1e6 : 3_600_000;
  return s.counts.map((c, i) => ({ t: new Date(start + i * step), v: Number(c) }));
}

export function seriesStep(s: Series | undefined): number {
  return s?.interval ? Number(s.interval.seconds) * 1000 : 3_600_000;
}

export function seriesTotal(s: Series | undefined): number {
  return s ? s.counts.reduce((a, c) => a + Number(c), 0) : 0;
}
