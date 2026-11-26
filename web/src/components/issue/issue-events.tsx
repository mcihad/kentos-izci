import { useInfiniteQuery } from "@connectrpc/connect-query";
import { thClass, theadClass, trClass } from "@kentos/components/common/table";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import { Link } from "@tanstack/react-router";
import { SearchIcon } from "lucide-react";
import { useState } from "react";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { IssueService } from "@/gen/izci/api/v1/issue_pb";
import { errorMessage } from "@/lib/errors";
import { useOrg } from "@/lib/org";

/** The issue's events, newest first, filterable with event search keys. */
export function IssueEvents({ issueId, shortId }: { issueId: string; shortId: string }) {
  const org = useOrg();
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const events = useInfiniteQuery(
    IssueService.method.listIssueEvents,
    { issueId, query, pageSize: 50, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const rows = events.data?.pages.flatMap((p) => p.events) ?? [];
  return (
    <div className="py-4">
      <form
        className="relative mb-3"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(draft.trim());
        }}
      >
        <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="environment:production customer:sivas-belediyesi release:1.4.2 browser:Chrome*"
          aria-label="Olaylarda ara"
          className="h-[max(var(--control-h-md),2.5rem)] w-full rounded-md border border-(--input-border) bg-(--input-bg) text-base outline-none transition-[color,box-shadow,border-color] duration-(--duration-fast) placeholder:text-subtle hover:border-(--input-border-hover) focus-visible:border-(--input-border-focus) focus-visible:shadow-(--focus-shadow) focus-visible:outline-none md:h-(--control-h-md) md:text-sm pr-3 pl-8 font-mono placeholder:font-sans"
        />
      </form>
      {events.error ? (
        <p className="py-6 text-center text-sm text-destructive">{errorMessage(events.error)}</p>
      ) : events.isPending ? (
        <p className="py-6 text-center text-sm text-muted-foreground">Yükleniyor…</p>
      ) : rows.length === 0 ? (
        <EmptyState title="Eşleşen olay yok">Arama ifadesini değiştirin ya da temizleyin.</EmptyState>
      ) : (
        <>
          <ul className="divide-y rounded-md border md:hidden">
            {rows.map((ev) => (
              <li key={ev.id} className="relative px-4 py-3 hover:bg-accent/45">
                <div className="flex items-baseline justify-between gap-3">
                  <Link
                    to="/o/$org/issues/$issueId"
                    params={{ org: org.slug, issueId: shortId }}
                    search={{ event: ev.id }}
                    className="font-semibold text-md after:absolute after:inset-0"
                  >
                    <RelativeTime value={ev.receivedAt} />
                  </Link>
                  <span className="truncate text-sm text-muted-foreground">{ev.environment}</span>
                </div>
                <p className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
                  {ev.customer ? (
                    <span className="text-foreground">{ev.customer.name || ev.customer.key}</span>
                  ) : null}
                  {ev.user ? <span className="max-w-full truncate">{ev.user}</span> : null}
                  {ev.release ? <span className="font-mono">{ev.release}</span> : null}
                  {ev.browser || ev.os ? <span>{[ev.browser, ev.os].filter(Boolean).join(", ")}</span> : null}
                </p>
              </li>
            ))}
          </ul>
          <div className="hidden overflow-x-auto rounded-md border md:block">
            <table className="w-full min-w-[720px] text-sm">
              <thead className={theadClass}>
                <tr>
                  <th className={thClass}>Zaman</th>
                  <th className={thClass}>Müşteri</th>
                  <th className={thClass}>Kullanıcı</th>
                  <th className={thClass}>Ortam</th>
                  <th className={thClass}>Sürüm</th>
                  <th className={thClass}>Tarayıcı / OS</th>
                  <th className={thClass}>Sunucu</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((ev) => (
                  <tr key={ev.id} className={cn("group", trClass)}>
                    <td className="whitespace-nowrap px-3.5 py-2">
                      <Link
                        to="/o/$org/issues/$issueId"
                        params={{ org: org.slug, issueId: shortId }}
                        search={{ event: ev.id }}
                        className="underline-offset-4 group-hover:underline"
                      >
                        <RelativeTime value={ev.receivedAt} />
                      </Link>
                    </td>
                    <td className="max-w-40 truncate px-3.5 py-2">
                      {ev.customer?.name || ev.customer?.key || "—"}
                    </td>
                    <td className="max-w-44 truncate px-3.5 py-2">{ev.user || "—"}</td>
                    <td className="px-3.5 py-2">{ev.environment || "—"}</td>
                    <td className="px-3.5 py-2 font-mono text-sm">{ev.release || "—"}</td>
                    <td className="max-w-44 truncate px-3.5 py-2 text-muted-foreground">
                      {[ev.browser, ev.os].filter(Boolean).join(", ") || "—"}
                    </td>
                    <td className="max-w-36 truncate px-3.5 py-2 font-mono text-sm text-muted-foreground">
                      {ev.serverName || "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      {events.hasNextPage ? (
        <div className="mt-3 flex justify-center">
          <Button
            variant="outline"
            size="sm"
            onClick={() => void events.fetchNextPage()}
            disabled={events.isFetchingNextPage}
          >
            Daha fazla olay
          </Button>
        </div>
      ) : null}
      {events.error && !events.data ? <ErrorState error={events.error} /> : null}
    </div>
  );
}
