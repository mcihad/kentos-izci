import { UserAvatar } from "@kentos/components/common/user-avatar";
import { cn } from "@kentos/components/lib/utils";
import { Checkbox } from "@kentos/components/ui/checkbox";
import { Link } from "@tanstack/react-router";
import { Sparkline } from "@/components/charts/sparkline";
import { RelativeTime } from "@/components/common/relative-time";
import type { IssueSummary } from "@/gen/izci/api/v1/issue_pb";
import { formatCompact } from "@/lib/format";
import { IssueTitle, LevelBlaze, PriorityBadge, SubstatusBadge, UnhandledBadge } from "./badges";
import { useIssuePreview } from "./issue-preview";

/** Whether first and last sighting are far enough apart to show both. */
function ageDiffers(issue: IssueSummary): boolean {
  const first = issue.firstSeen ? Number(issue.firstSeen.seconds) : 0;
  const last = issue.lastSeen ? Number(issue.lastSeen.seconds) : 0;
  return last - first > 90;
}

/** One issue in a list: level blaze, title, culprit, facts and activity. */
export function IssueRow({
  issue,
  org,
  selected,
  onSelect,
  focused,
  compact = false,
  showUsers = true,
  onPreview,
}: {
  issue: IssueSummary;
  org: string;
  selected?: boolean;
  onSelect?: (v: boolean) => void;
  focused?: boolean;
  compact?: boolean;
  showUsers?: boolean;
  /**
   * Opens a summary instead of navigating on a plain click; modified clicks
   * (⌘/Ctrl, Shift, middle button) still open the issue page as a link.
   * Inside an IssuePreviewHost rows do this without the prop.
   */
  onPreview?: () => void;
}) {
  const host = useIssuePreview();
  const preview = onPreview ?? (host ? () => host(issue.shortId) : undefined);
  const events = formatCompact(issue.periodCount || issue.timesSeen);
  const users = formatCompact(issue.usersCount);
  return (
    <div
      data-focused={focused || undefined}
      data-selected={selected || undefined}
      className={cn(
        "group relative flex items-stretch gap-3 border-b transition-colors hover:bg-accent/45",
        "data-[selected]:bg-highlight data-[focused]:bg-accent/60",
        compact ? "py-3 pr-3 pl-3" : "py-3.5 pr-4 pl-3 sm:pr-6 lg:pr-8",
      )}
    >
      <LevelBlaze level={issue.level} className="my-0.5" />
      {onSelect ? (
        <div className="hidden items-start pt-0.5 sm:flex">
          <Checkbox
            checked={selected}
            onCheckedChange={(v) => onSelect(v === true)}
            aria-label={`${issue.shortId} seç`}
            className="relative z-10"
          />
        </div>
      ) : null}
      <div className="min-w-0 flex-1">
        <Link
          to="/o/$org/issues/$issueId"
          params={{ org, issueId: issue.shortId }}
          onClick={(e) => {
            if (!preview || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
            e.preventDefault();
            preview();
          }}
          className="line-clamp-2 break-words text-base leading-snug after:absolute after:inset-0 focus-visible:outline-none sm:line-clamp-1"
        >
          <IssueTitle type={issue.type} value={issue.value} title={issue.title} />
        </Link>
        {issue.culprit ? (
          <p className="mt-1 truncate font-mono text-sm text-muted-foreground">{issue.culprit}</p>
        ) : null}
        <div className="mt-2 flex flex-wrap items-center gap-x-3.5 gap-y-1.5 text-sm text-muted-foreground">
          <span className="font-mono font-semibold text-foreground">{issue.shortId}</span>
          {!compact ? <span>{issue.project?.name}</span> : null}
          <SubstatusBadge substatus={issue.substatus} />
          {issue.unhandled ? <UnhandledBadge /> : null}
          <PriorityBadge priority={issue.priority} />
          <span className="inline-flex gap-1">
            <RelativeTime value={issue.lastSeen} />
            {!compact && ageDiffers(issue) ? (
              <span className="hidden md:inline">
                (ilk <RelativeTime value={issue.firstSeen} />)
              </span>
            ) : null}
          </span>
          <span className="tabular sm:hidden">
            <span className="font-semibold text-foreground">{events}</span> olay
            {showUsers ? (
              <>
                , <span className="font-semibold text-foreground">{users}</span> kullanıcı
              </>
            ) : null}
          </span>
          {issue.assignee ? (
            <span className="inline-flex items-center gap-1.5">
              <UserAvatar
                name={issue.assignee.name || issue.assignee.email}
                src={issue.assignee.avatarUrl}
                className="size-5"
              />
              <span className="hidden md:inline">{issue.assignee.name || issue.assignee.email}</span>
            </span>
          ) : null}
        </div>
      </div>
      {!compact ? (
        <div className="hidden items-center lg:flex">
          <Sparkline series={issue.series} className="w-32" />
        </div>
      ) : null}
      <div className="hidden w-16 flex-col items-end justify-center text-right sm:flex">
        <span className="font-semibold text-lg tabular">{events}</span>
        <span className="text-xs text-muted-foreground">olay</span>
      </div>
      {showUsers ? (
        <div className="hidden w-16 flex-col items-end justify-center text-right sm:flex">
          <span className="font-semibold text-lg tabular">{users}</span>
          <span className="text-xs text-muted-foreground">kullanıcı</span>
        </div>
      ) : null}
    </div>
  );
}
