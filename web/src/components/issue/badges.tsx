import { IssueStatus, IssueSubstatus, Priority } from "@/gen/izci/api/v1/common_pb";
import type { Level } from "@/gen/izci/ingest/v1/event_pb";
import { levels, priorityLabels, statusLabels, substatusLabels } from "@/lib/labels";
import { cn } from "@/lib/utils";

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

const pill = "inline-flex h-6 items-center whitespace-nowrap rounded-full border px-2 font-medium text-xs";

export function SubstatusBadge({ substatus }: { substatus: IssueSubstatus }) {
  if (substatus === IssueSubstatus.UNSPECIFIED || substatus === IssueSubstatus.ONGOING) return null;
  const tone =
    substatus === IssueSubstatus.NEW
      ? "border-primary/40 text-primary"
      : substatus === IssueSubstatus.REGRESSED
        ? "border-level-warning/45 text-level-warning-ink"
        : "border-level-error/45 text-level-error-ink";
  return <span className={cn(pill, tone)}>{substatusLabels[substatus]}</span>;
}

export function StatusBadge({ status }: { status: IssueStatus }) {
  const tone =
    status === IssueStatus.RESOLVED
      ? "border-health-good/40 text-health-good-ink"
      : status === IssueStatus.IGNORED
        ? "border-border text-muted-foreground"
        : "border-level-error/40 text-level-error-ink";
  return <span className={cn(pill, tone)}>{statusLabels[status]}</span>;
}

export function UnhandledBadge() {
  return <span className={cn(pill, "border-level-fatal/40 text-level-fatal-ink")}>İşlenmemiş</span>;
}

export function PriorityBadge({ priority }: { priority: Priority }) {
  if (priority !== Priority.HIGH && priority !== Priority.CRITICAL) return null;
  return (
    <span
      className={cn(
        pill,
        priority === Priority.CRITICAL
          ? "border-level-fatal/40 text-level-fatal-ink"
          : "border-level-warning/40 text-level-warning-ink",
      )}
    >
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
