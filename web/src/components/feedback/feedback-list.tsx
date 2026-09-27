import { useInfiniteQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { MessageSquareTextIcon } from "lucide-react";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { FeedbackService, type UserFeedback } from "@/gen/izci/api/v1/feedback_pb";
import { initials } from "@/lib/format";
import { useOrg } from "@/lib/org";

/** Where a feedback message was sent from, in the words of the UI. */
const sources: Record<string, string> = {
  widget: "Geri bildirim penceresi",
  api: "Uygulama",
  user_report: "Hata raporu",
  crash_report_embed_form: "Hata raporu",
};

/** One end-user message, with what is known about where it came from. */
export function FeedbackItem({ fb, showIssue = true }: { fb: UserFeedback; showIssue?: boolean }) {
  const org = useOrg();
  const who = fb.name || fb.email || fb.user || "Adsız kullanıcı";
  const meta = [fb.release, fb.environment, fb.browser, fb.os].filter(Boolean);
  return (
    <li className="flex gap-3 py-4">
      <span
        aria-hidden
        className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-full bg-muted font-medium text-[11.5px] text-muted-foreground"
      >
        {initials(who)}
      </span>
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-baseline gap-x-2 text-[13px]">
          <span className="font-medium">{who}</span>
          {fb.email && fb.email !== who ? (
            <a
              href={`mailto:${fb.email}`}
              className="text-muted-foreground hover:text-foreground hover:underline"
            >
              {fb.email}
            </a>
          ) : null}
          <span className="text-[12px] text-muted-foreground">
            <RelativeTime value={fb.createdAt} />
          </span>
        </p>
        <blockquote className="mt-1.5 whitespace-pre-wrap break-words border-primary/60 border-l-2 pl-3 text-[14px] leading-relaxed">
          {fb.message}
        </blockquote>
        <p className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[12px] text-muted-foreground">
          {showIssue && fb.issue ? (
            <Link
              to="/o/$org/issues/$issueId"
              params={{ org: org.slug, issueId: fb.issue.shortId }}
              className="font-mono text-foreground hover:underline"
              title={fb.issue.title}
            >
              {fb.issue.shortId}
            </Link>
          ) : null}
          {fb.customer ? (
            <Link
              to="/o/$org/customers/$key"
              params={{ org: org.slug, key: fb.customer.key }}
              className="hover:text-foreground hover:underline"
            >
              {fb.customer.name || fb.customer.key}
            </Link>
          ) : null}
          {fb.project ? <span>{fb.project.name}</span> : null}
          {meta.map((m) => (
            <span key={m} className="font-mono text-[11.5px]">
              {m}
            </span>
          ))}
          {fb.url ? (
            <a
              href={fb.url}
              target="_blank"
              rel="noreferrer noopener"
              className="max-w-[28rem] truncate hover:text-foreground hover:underline"
            >
              {fb.url}
            </a>
          ) : null}
          {sources[fb.source] ? <span>{sources[fb.source]}</span> : null}
        </p>
      </div>
    </li>
  );
}

/** Feedback of the organization, a project or an issue, newest first. */
export function FeedbackList({
  project = "",
  issueId = "",
  query = "",
  showIssue = true,
}: {
  project?: string;
  issueId?: string;
  query?: string;
  showIssue?: boolean;
}) {
  const list = useInfiniteQuery(
    FeedbackService.method.listFeedback,
    { project, issueId, query, pageSize: 50, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const rows = list.data?.pages.flatMap((p) => p.feedback) ?? [];
  if (list.error) return <ErrorState error={list.error} onRetry={() => void list.refetch()} />;
  if (list.isPending) {
    return (
      <div className="space-y-4 py-4">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  }
  if (rows.length === 0) {
    return (
      <EmptyState
        icon={MessageSquareTextIcon}
        title={query ? "Eşleşen geri bildirim yok" : "Henüz geri bildirim yok"}
      >
        {query ? (
          "Aramayı değiştirin ya da temizleyin."
        ) : (
          <>
            Kullanıcılar Sentry SDK'larının geri bildirim penceresiyle ya da uygulamanın{" "}
            <code className="font-mono">captureFeedback</code> çağrısıyla yazdıklarını gönderdiğinde burada
            görünür.
          </>
        )}
      </EmptyState>
    );
  }
  return (
    <div>
      <ul className="divide-y">
        {rows.map((fb) => (
          <FeedbackItem key={fb.id} fb={fb} showIssue={showIssue} />
        ))}
      </ul>
      {list.hasNextPage ? (
        <Button variant="ghost" size="sm" className="mt-2" onClick={() => void list.fetchNextPage()}>
          Daha eski geri bildirimler
        </Button>
      ) : null}
    </div>
  );
}
