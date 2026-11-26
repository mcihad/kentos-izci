import { Sparkline as KitSparkline } from "@kentos/components/charts/sparkline";
import { formatNumber } from "@kentos/components/lib/format";
import type { Series } from "@/gen/izci/api/v1/common_pb";
import { seriesPoints, seriesTotal } from "@/lib/series";

/** The kit's sparkline for an API series of event counts. */
export function Sparkline({
  series,
  className,
  height,
}: {
  series: Series | undefined;
  className?: string;
  height?: number;
}) {
  return (
    <KitSparkline
      points={seriesPoints(series)}
      label={`Toplam ${formatNumber(seriesTotal(series))} olay`}
      className={className}
      height={height}
    />
  );
}
