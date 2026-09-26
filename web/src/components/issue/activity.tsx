import { createConnectQueryKey, useInfiniteQuery, useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { UserAvatar } from "@/components/common/user-avatar";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ActivityKind, type IssueActivity, IssueService } from "@/gen/izci/api/v1/issue_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";
import { cn } from "@/lib/utils";

const statusText: Record<string, string> = {
  ISSUE_STATUS_RESOLVED: "hatayı çözüldü olarak işaretledi",
  ISSUE_STATUS_IGNORED: "hatayı yoksaydı",
  ISSUE_STATUS_UNRESOLVED: "hatayı yeniden açtı",
};
const priorityText: Record<string, string> = {
  PRIORITY_LOW: "düşük",
  PRIORITY_MEDIUM: "orta",
  PRIORITY_HIGH: "yüksek",
  PRIORITY_CRITICAL: "kritik",
};

function describe(a: IssueActivity): string {
  switch (a.kind) {
    case ActivityKind.CREATED:
      return `Hata ilk kez görüldü${a.data.release ? ` (${a.data.release} sürümü)` : ""}`;
    case ActivityKind.REGRESSED:
      return `Çözülmüş hata geri döndü${a.data.release ? ` (${a.data.release} sürümü)` : ""}`;
    case ActivityKind.STATUS_CHANGED:
      return `${statusText[a.data.to ?? ""] ?? "durumu değiştirdi"}${a.data.release ? ` (${a.data.release} sürümünde)` : ""}`;
    case ActivityKind.ASSIGNED:
      return a.data.assignee ? `hatayı ${a.data.assignee} kişisine atadı` : "atamayı kaldırdı";
    case ActivityKind.PRIORITY_CHANGED:
      return `önceliği ${priorityText[a.data.to ?? ""] ?? a.data.to} yaptı`;
    case ActivityKind.AI_ANALYSIS:
      return "yapay zekâ analizi başlattı";
    case ActivityKind.BRANCH_CREATED:
      return `dal oluşturdu${a.data.branch ? `: ${a.data.branch}` : ""}`;
    case ActivityKind.PULL_REQUEST_OPENED:
      return `çekme isteği açtı${a.data.url ? `: ${a.data.url}` : ""}`;
    case ActivityKind.NOTIFIED:
      return `bildirim gönderildi${a.data.channel ? ` (${a.data.channel})` : ""}`;
    default:
      return "";
  }
}

/** The issue's history and comments, newest first. */
export function Activity({ issueId, writable }: { issueId: string; writable: boolean }) {
  const org = useOrg();
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const list = useInfiniteQuery(
    IssueService.method.listIssueActivity,
    { issueId, pageSize: 30, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const comment = useMutation(IssueService.method.addIssueComment, {
    onSuccess: () => {
      setText("");
      void qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: IssueService.method.listIssueActivity,
          transport: org.transport,
          cardinality: "infinite",
        }),
      });
    },
    onError: (err) => toast.error(errorMessage(err)),
  });
  const items = list.data?.pages.flatMap((p) => p.activities) ?? [];

  return (
    <div className="py-5">
      {writable ? (
        <form
          className="mb-6 grid gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (text.trim()) comment.mutate({ issueId, text });
          }}
        >
          <Textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Ekibe not bırakın: nasıl yeniden üretildiği, kimin baktığı, geçici çözüm…"
            className="min-h-20 text-[13px]"
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && text.trim())
                comment.mutate({ issueId, text });
            }}
          />
          <div className="flex justify-end">
            <Button type="submit" size="sm" disabled={!text.trim() || comment.isPending}>
              Yorum ekle
            </Button>
          </div>
        </form>
      ) : null}
      <ol className="relative">
        {items.map((a, i) => {
          const isComment = a.kind === ActivityKind.COMMENT;
          const who = a.actor?.name || a.actor?.email;
          return (
            <li key={a.id} className="relative flex gap-3 pb-5 pl-8">
              <span
                aria-hidden="true"
                className={cn(
                  "absolute left-[11px] border-trail border-l",
                  i === 0 ? "top-3" : "top-0",
                  i === items.length - 1 ? "h-3" : "bottom-0",
                )}
              />
              <span className="absolute top-0.5 left-0 grid size-6 place-items-center rounded-full bg-background">
                {a.actor ? (
                  <UserAvatar name={who ?? ""} src={a.actor.avatarUrl} className="size-6" />
                ) : (
                  <span
                    className={cn(
                      "size-2 rounded-full",
                      a.kind === ActivityKind.REGRESSED ? "bg-level-warning" : "bg-muted-foreground/60",
                    )}
                  />
                )}
              </span>
              <div className="min-w-0 flex-1 pt-0.5 text-[13px]">
                <p>
                  {who ? <span className="font-medium">{who} </span> : null}
                  {isComment ? (
                    <span className="text-muted-foreground">yorum yaptı</span>
                  ) : (
                    <span>{describe(a)}</span>
                  )}
                  <span className="ml-2 text-[12px] text-muted-foreground">
                    <RelativeTime value={a.createdAt} />
                  </span>
                </p>
                {isComment ? (
                  <p className="mt-1.5 whitespace-pre-wrap break-words rounded-md border bg-card px-3 py-2">
                    {a.text}
                  </p>
                ) : null}
              </div>
            </li>
          );
        })}
      </ol>
      {list.hasNextPage ? (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => void list.fetchNextPage()}
          disabled={list.isFetchingNextPage}
        >
          Daha eski etkinlikler
        </Button>
      ) : null}
    </div>
  );
}
