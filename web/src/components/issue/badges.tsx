import { cn } from "@kentos/components/lib/utils";
import { IssueStatus, IssueSubstatus, Priority } from "@/gen/izci/api/v1/common_pb";
import type { Level } from "@/gen/izci/ingest/v1/event_pb";
import { levels, priorityLabels, statusLabels, substatusLabels } from "@/lib/labels";

/**
 * The level "blaze": a short vertical mark in the level's color, like the
 * paint marks that guide walkers along a trail.
 */
export function LevelBlaze({ level, className }: { level: Level; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn("block w-1 shrink-0 rounded-full", className)}
      style={{ background: levels[level].color }}
    />
  );
}

/** Level name with a colored dot. */
export function LevelLabel({ level, className }: { level: Level; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 whitespace-nowrap", className)}>
      <span className="size-2.5 rounded-full" style={{ background: levels[level].color }} />
      {levels[level].label}
    </span>
  );
}

/** The design system's badge: a 20px soft pill in a status tone. */
export const pill =
  "inline-flex h-5 items-center gap-1 whitespace-nowrap rounded-full border px-2 font-semibold text-2xs tracking-[0.01em]";

/** Badge tones: fg / bg / border triplets from the status tokens. */
export const tones = {
  neutral: "border-status-neutral-border bg-status-neutral-bg text-status-neutral-fg",
  info: "border-status-info-border bg-status-info-bg text-status-info-fg",
  success: "border-status-success-border bg-status-success-bg text-status-success-fg",
  warning: "border-status-warning-border bg-status-warning-bg text-status-warning-fg",
  danger: "border-status-danger-border bg-status-danger-bg text-status-danger-fg",
  fatal: "border-level-fatal/35 bg-level-fatal/10 text-level-fatal-ink",
} as const;

export function SubstatusBadge({ substatus }: { substatus: IssueSubstatus }) {
  if (substatus === IssueSubstatus.UNSPECIFIED || substatus === IssueSubstatus.ONGOING) return null;
  const tone =
    substatus === IssueSubstatus.NEW
      ? tones.info
      : substatus === IssueSubstatus.REGRESSED
        ? tones.warning
        : tones.danger;
  return <span className={cn(pill, tone)}>{substatusLabels[substatus]}</span>;
}

export function StatusBadge({ status }: { status: IssueStatus }) {
  const tone =
    status === IssueStatus.RESOLVED
      ? tones.success
      : status === IssueStatus.IGNORED
        ? tones.neutral
        : tones.danger;
  return <span className={cn(pill, tone)}>{statusLabels[status]}</span>;
}

export function UnhandledBadge() {
  return <span className={cn(pill, tones.fatal)}>İşlenmemiş</span>;
}

export function PriorityBadge({ priority }: { priority: Priority }) {
  if (priority !== Priority.HIGH && priority !== Priority.CRITICAL) return null;
  return (
    <span className={cn(pill, priority === Priority.CRITICAL ? tones.danger : tones.warning)}>
      {priorityLabels[priority]}
    </span>
  );
}

/** "Type: value" split so the exception type reads first. */
export function IssueTitle({
  type,
  value,
  title,
  className,
}: {
  type: string;
  value: string;
  title: string;
  className?: string;
}) {
  if (!type) return <span className={cn("font-medium", className)}>{title}</span>;
  return (
    <span className={className}>
      <span className="font-semibold">{type}</span>
      {value ? <span className="text-muted-foreground"> {value}</span> : null}
    </span>
  );
}
