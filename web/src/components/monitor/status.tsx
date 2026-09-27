import { pill, tones } from "@/components/issue/badges";
import {
  type CheckIn,
  CheckInStatus,
  type Monitor,
  type MonitorEnvironment,
  MonitorStatus,
} from "@/gen/izci/api/v1/monitor_pb";
import { formatDateTime, formatDuration, toDate } from "@/lib/format";
import { cn } from "@/lib/utils";

type Look = { label: string; tone: string; color: string };

/** How the state of a monitor environment is shown. */
export const monitorStatus: Record<MonitorStatus, Look> = {
  [MonitorStatus.UNSPECIFIED]: { label: "Bilinmiyor", tone: tones.neutral, color: "var(--level-debug)" },
  [MonitorStatus.WAITING]: {
    label: "İlk çalışma bekleniyor",
    tone: tones.neutral,
    color: "var(--level-debug)",
  },
  [MonitorStatus.OK]: { label: "Başarılı", tone: tones.success, color: "var(--health-good)" },
  [MonitorStatus.ERROR]: { label: "Hata verdi", tone: tones.danger, color: "var(--level-error)" },
  [MonitorStatus.MISSED]: { label: "Kaçırıldı", tone: tones.warning, color: "var(--level-warning)" },
  [MonitorStatus.TIMEOUT]: { label: "Zaman aşımı", tone: tones.fatal, color: "var(--level-fatal)" },
};

/** How a run is shown. */
export const checkInStatus: Record<CheckInStatus, Look> = {
  [CheckInStatus.UNSPECIFIED]: { label: "Bilinmiyor", tone: tones.neutral, color: "var(--level-debug)" },
  [CheckInStatus.IN_PROGRESS]: { label: "Çalışıyor", tone: tones.info, color: "var(--level-info)" },
  [CheckInStatus.OK]: { label: "Başarılı", tone: tones.success, color: "var(--health-good)" },
  [CheckInStatus.ERROR]: { label: "Hata", tone: tones.danger, color: "var(--level-error)" },
  [CheckInStatus.MISSED]: { label: "Kaçırıldı", tone: tones.warning, color: "var(--level-warning)" },
  [CheckInStatus.TIMEOUT]: { label: "Zaman aşımı", tone: tones.fatal, color: "var(--level-fatal)" },
};

const severity: Record<MonitorStatus, number> = {
  [MonitorStatus.UNSPECIFIED]: 0,
  [MonitorStatus.WAITING]: 1,
  [MonitorStatus.OK]: 2,
  [MonitorStatus.MISSED]: 3,
  [MonitorStatus.TIMEOUT]: 4,
  [MonitorStatus.ERROR]: 5,
};

/** The environment whose state matters most: the worst one, production first on ties. */
export function headlineEnv(m: Monitor): MonitorEnvironment | undefined {
  let best: MonitorEnvironment | undefined;
  for (const e of m.environments) {
    const s = severity[e.status];
    const b = best ? severity[best.status] : -1;
    if (s > b || (s === b && e.environment === "production")) best = e;
  }
  return best;
}

/** Status badge of a monitor: its worst environment, or paused. */
export function MonitorBadge({
  monitor,
  env,
  className,
}: {
  monitor: Monitor;
  env?: MonitorEnvironment;
  className?: string;
}) {
  if (monitor.paused) return <span className={cn(pill, tones.neutral, className)}>Duraklatıldı</span>;
  const e = env ?? headlineEnv(monitor);
  const look = monitorStatus[e?.status ?? MonitorStatus.WAITING];
  return <span className={cn(pill, look.tone, className)}>{look.label}</span>;
}

export function CheckInBadge({ status }: { status: CheckInStatus }) {
  const look = checkInStatus[status];
  return <span className={cn(pill, look.tone)}>{look.label}</span>;
}

/**
 * The latest runs as a strip of ticks, oldest on the left, so a pattern of
 * failures reads at a glance. Runs arrive newest first.
 */
export function RunStrip({
  runs,
  slots = 30,
  className,
}: {
  runs: CheckIn[];
  slots?: number;
  className?: string;
}) {
  const shown = runs.slice(0, slots).reverse();
  const failed = shown.filter(
    (r) => r.status !== CheckInStatus.OK && r.status !== CheckInStatus.IN_PROGRESS,
  ).length;
  const summary =
    shown.length === 0 ? "Henüz çalışma yok" : `Son ${shown.length} çalışmadan ${failed} tanesi başarısız`;
  return (
    <div role="img" aria-label={summary} className={cn("flex h-6 items-end gap-[3px]", className)}>
      {Array.from({ length: slots - shown.length }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: empty placeholders
        <span key={`empty-${i}`} className="h-2 w-1.5 shrink-0 rounded-[2px] bg-(--surface-active)" />
      ))}
      {shown.map((r) => {
        const look = checkInStatus[r.status];
        const started = toDate(r.startedAt);
        const d = durationMs(r);
        return (
          <span
            key={r.id}
            title={[
              look.label,
              started ? formatDateTime(started) : "",
              d !== undefined ? formatDuration(d) : "",
            ]
              .filter(Boolean)
              .join(", ")}
            className={cn(
              "w-1.5 shrink-0 rounded-[2px]",
              r.status === CheckInStatus.OK || r.status === CheckInStatus.IN_PROGRESS ? "h-4" : "h-6",
            )}
            style={{ background: look.color }}
          />
        );
      })}
    </div>
  );
}

/** Duration of a run in milliseconds, when known. */
export function durationMs(r: CheckIn): number | undefined {
  return r.duration ? Number(r.duration.seconds) * 1000 + r.duration.nanos / 1e6 : undefined;
}
