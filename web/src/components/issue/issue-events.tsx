import { useInfiniteQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { SearchIcon } from "lucide-react";
import { useState } from "react";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
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
          className="h-9 w-full rounded-md border bg-card pr-3 pl-8 font-mono text-[12.5px] outline-none placeholder:font-sans placeholder:text-muted-foreground/70 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/30"
        />
      </form>
      {events.error ? (
        <p className="py-6 text-center text-[13px] text-destructive">{errorMessage(events.error)}</p>
      ) : events.isPending ? (
        <p className="py-6 text-center text-[13px] text-muted-foreground">Yükleniyor…</p>
      ) : rows.length === 0 ? (
        <EmptyState title="Eşleşen olay yok">Arama ifadesini değiştirin ya da temizleyin.</EmptyState>
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[720px] text-[12.5px]">
            <thead className="bg-muted/40 text-left text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">Zaman</th>
                <th className="px-3 py-2 font-medium">Müşteri</th>
                <th className="px-3 py-2 font-medium">Kullanıcı</th>
                <th className="px-3 py-2 font-medium">Ortam</th>
                <th className="px-3 py-2 font-medium">Sürüm</th>
                <th className="px-3 py-2 font-medium">Tarayıcı / OS</th>
                <th className="px-3 py-2 font-medium">Sunucu</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((ev) => (
                <tr key={ev.id} className="group hover:bg-accent/45">
                  <td className="whitespace-nowrap px-3 py-2">
                    <Link
                      to="/o/$org/issues/$issueId"
                      params={{ org: org.slug, issueId: shortId }}
                      search={{ event: ev.id }}
                      className="underline-offset-4 group-hover:underline"
                    >
                      <RelativeTime value={ev.receivedAt} />
                    </Link>
                  </td>
                  <td className="max-w-40 truncate px-3 py-2">
                    {ev.customer?.name || ev.customer?.key || "—"}
                  </td>
                  <td className="max-w-44 truncate px-3 py-2">{ev.user || "—"}</td>
                  <td className="px-3 py-2">{ev.environment || "—"}</td>
                  <td className="px-3 py-2 font-mono text-[12px]">{ev.release || "—"}</td>
                  <td className="max-w-44 truncate px-3 py-2 text-muted-foreground">
                    {[ev.browser, ev.os].filter(Boolean).join(", ") || "—"}
                  </td>
                  <td className="max-w-36 truncate px-3 py-2 font-mono text-[12px] text-muted-foreground">
                    {ev.serverName || "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
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
