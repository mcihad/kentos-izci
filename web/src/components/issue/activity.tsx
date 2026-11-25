import { createConnectQueryKey, useInfiniteQuery, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { toast } from "sonner";
import { RelativeTime } from "@/components/common/relative-time";
import { UserAvatar } from "@/components/common/user-avatar";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ActivityKind, type IssueActivity, IssueService } from "@/gen/izci/api/v1/issue_pb";
import { type Member, OrganizationService } from "@/gen/izci/api/v1/organization_pb";
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
      if (a.data.reason === "monitor_recovered")
        return "Zamanlanmış görev yeniden başarıyla çalıştı; hata kendiliğinden çözüldü";
      return `${statusText[a.data.to ?? ""] ?? "durumu değiştirdi"}${a.data.release ? ` (${a.data.release} sürümünde)` : ""}`;
    case ActivityKind.ASSIGNED:
      if (a.data.auto === "rule")
        return `${a.data.rule ? `${a.data.rule} sahiplik kuralıyla` : "Sahiplik kuralıyla"} ${a.data.assignee} kişisine otomatik atandı`;
      if (a.data.auto === "suspect_commit")
        return `Şüpheli commit'in (${(a.data.commit ?? "").slice(0, 7)}) yazarı ${a.data.assignee} kişisine otomatik atandı`;
      return a.data.assignee ? `hatayı ${a.data.assignee} kişisine atadı` : "atamayı kaldırdı";
    case ActivityKind.PRIORITY_CHANGED:
      return `önceliği ${priorityText[a.data.to ?? ""] ?? a.data.to} yaptı`;
    case ActivityKind.AI_ANALYSIS: {
      const files = a.data.files ? `; ${a.data.files} dosyada düzeltme önerildi` : "";
      if (a.data.status === "failed")
        return a.actor
          ? "yapay zekâ analizi başlattı; analiz tamamlanamadı"
          : "Otomatik yapay zekâ analizi tamamlanamadı";
      return a.actor
        ? `yapay zekâ ile analiz etti${files}`
        : `Otomatik yapay zekâ analizi tamamlandı${files}`;
    }
    case ActivityKind.BRANCH_CREATED: {
      const branch = a.data.branch ? `: ${a.data.branch}` : "";
      return a.actor
        ? `düzeltmeyi bir dala gönderdi${branch}`
        : `Düzeltme otomatik olarak bir dala gönderildi${branch}`;
    }
    case ActivityKind.PULL_REQUEST_OPENED: {
      const number = a.data.number ? ` (#${a.data.number})` : "";
      return a.actor ? `çekme isteği açtı${number}` : `Otomatik olarak çekme isteği açıldı${number}`;
    }
    case ActivityKind.NOTIFIED:
      return `bildirim gönderildi${a.data.channel ? ` (${a.data.channel})` : ""}`;
    case ActivityKind.TICKET_CREATED: {
      const where = a.data.channel ? ` (${a.data.channel})` : "";
      return a.actor
        ? `Jira'da ${a.data.key} iş kaydını açtı${where}`
        : `Jira'da otomatik olarak ${a.data.key} iş kaydı açıldı${where}`;
    }
    default:
      return "";
  }
}

/** How a member is named in a comment: the part of their address before
 * "@", or the whole address when another member shares it. */
function handles(members: Member[]): { handle: string; name: string; email: string }[] {
  const locals = new Map<string, number>();
  for (const m of members) {
    const local = (m.user?.email ?? "").split("@")[0]?.toLowerCase() ?? "";
    locals.set(local, (locals.get(local) ?? 0) + 1);
  }
  return members.map((m) => {
    const email = m.user?.email ?? "";
    const local = email.split("@")[0]?.toLowerCase() ?? "";
    return { handle: locals.get(local) === 1 ? local : email.toLowerCase(), name: m.user?.name ?? "", email };
  });
}

/** The issue's history and comments, newest first. */
export function Activity({ issueId, writable }: { issueId: string; writable: boolean }) {
  const org = useOrg();
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const [caret, setCaret] = useState(0);
  const input = useRef<HTMLTextAreaElement>(null);
  const members = useQuery(
    OrganizationService.method.listMembers,
    {},
    { enabled: writable, staleTime: 5 * 60_000 },
  );
  // Suggest members while an "@name" is typed right before the caret.
  const typed = /(^|[\s(])@([\p{L}\p{N}._%+-]*)$/u.exec(text.slice(0, caret));
  const query = typed?.[2]?.toLowerCase();
  const suggestions =
    query === undefined
      ? []
      : handles(members.data?.members ?? [])
          .filter((h) => h.handle.startsWith(query) || h.name.toLowerCase().includes(query))
          .slice(0, 5);
  const mention = (handle: string) => {
    const start = caret - (query?.length ?? 0) - 1;
    const next = `${text.slice(0, start)}@${handle} ${text.slice(caret)}`;
    const at = start + handle.length + 2;
    setText(next);
    setCaret(at);
    requestAnimationFrame(() => {
      input.current?.focus();
      input.current?.setSelectionRange(at, at);
    });
  };
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
            ref={input}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setCaret(e.target.selectionStart);
            }}
            onSelect={(e) => setCaret(e.currentTarget.selectionStart)}
            placeholder="Ekibe not bırakın: nasıl yeniden üretildiği, kimin baktığı, geçici çözüm…"
            className="min-h-20 text-sm"
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && text.trim())
                comment.mutate({ issueId, text });
            }}
          />
          {suggestions.length > 0 ? (
            <fieldset className="flex flex-wrap gap-1.5">
              <legend className="sr-only">Anılacak üye</legend>
              {suggestions.map((s) => (
                <Button
                  key={s.handle}
                  type="button"
                  size="xs"
                  variant="outline"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => mention(s.handle)}
                >
                  @{s.handle}
                  {s.name ? <span className="text-muted-foreground">{s.name}</span> : null}
                </Button>
              ))}
            </fieldset>
          ) : null}
          <div className="flex items-center justify-between gap-3">
            <p className="text-muted-foreground text-xs">
              Bir üyeyi anmak için @ yazın; anılan üye bildirim alır ve hatayı takip eder.
            </p>
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
              <div className="min-w-0 flex-1 pt-0.5 text-sm">
                <p>
                  {who ? <span className="font-medium">{who} </span> : null}
                  {isComment ? (
                    <span className="text-muted-foreground">yorum yaptı</span>
                  ) : (
                    <span>{describe(a)}</span>
                  )}
                  <span className="ml-2 text-xs text-muted-foreground">
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
