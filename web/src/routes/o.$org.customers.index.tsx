import { useInfiniteQuery } from "@connectrpc/connect-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Building2Icon, SearchIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { PeriodSelect, parsePeriod } from "@/components/app/filters";
import { gutter, PageHeader } from "@/components/app/page";
import { Sparkline } from "@/components/charts/sparkline";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { HealthLabel } from "@/components/customer/health";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { CustomerService, CustomerSort } from "@/gen/izci/api/v1/customer_pb";
import { formatCompact, formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { cn } from "@/lib/utils";

// Plain keys for URL validation: route definitions stay in the entry chunk,
// so they must not pull in generated message code.
const sortKeys = ["events", "issues", "last_seen", "name"] as const;
type SortKey = (typeof sortKeys)[number];

const sorts: Record<SortKey, { value: CustomerSort; label: string }> = {
  events: { value: CustomerSort.EVENTS, label: "Olay sayısı" },
  issues: { value: CustomerSort.ISSUES, label: "Açık hata sayısı" },
  last_seen: { value: CustomerSort.LAST_SEEN, label: "Son görülme" },
  name: { value: CustomerSort.NAME, label: "Ad" },
};
type CustomersSearch = { query?: string; sort?: SortKey; period?: string };

export const Route = createFileRoute("/o/$org/customers/")({
  validateSearch: (s: Record<string, unknown>): CustomersSearch => ({
    query: typeof s.query === "string" && s.query ? s.query : undefined,
    sort: sortKeys.find((k) => k === s.sort),
    period: typeof s.period === "string" ? s.period : undefined,
  }),
  component: CustomersPage,
});

function CustomersPage() {
  const org = useOrg();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const period = parsePeriod(search.period, "7d");
  const sort = search.sort ?? "events";
  const [text, setText] = useState(search.query ?? "");
  useEffect(() => setText(search.query ?? ""), [search.query]);
  const list = useInfiniteQuery(
    CustomerService.method.listCustomers,
    { query: search.query ?? "", period: period.value, sort: sorts[sort].value, pageSize: 50, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const rows = list.data?.pages.flatMap((p) => p.customers) ?? [];
  const setSearch = (patch: Partial<CustomersSearch>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });

  return (
    <div className="pb-12">
      <PageHeader
        title="Müşteriler"
        description="Uygulamalarınızı kullanan kurumlar ve hatalardan ne kadar etkilendikleri"
      />
      <div className={cn("flex flex-col gap-2 border-b py-3 sm:flex-row sm:items-center", gutter)}>
        <form
          className="relative flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            setSearch({ query: text.trim() || undefined });
          }}
        >
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            onBlur={() => setSearch({ query: text.trim() || undefined })}
            placeholder="Müşteri adı ya da anahtarı"
            aria-label="Müşteri ara"
            className="h-9 w-full rounded-md border bg-card pr-3 pl-8 text-[13px] outline-none placeholder:text-muted-foreground/70 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/30"
          />
        </form>
        <div className="flex gap-2">
          <Select
            value={sort}
            onValueChange={(v) => setSearch({ sort: v === "events" ? undefined : (v as SortKey) })}
          >
            <SelectTrigger size="sm" className="h-8 text-[13px]" aria-label="Sıralama">
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              {Object.entries(sorts).map(([k, s]) => (
                <SelectItem key={k} value={k}>
                  {s.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <PeriodSelect
            value={period.key}
            onChange={(k) => setSearch({ period: k === "7d" ? undefined : k })}
          />
        </div>
      </div>
      {list.isPending ? (
        <div className={cn("space-y-2 py-4", gutter)}>
          {Array.from({ length: 6 }, (_, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: placeholders
            <Skeleton key={i} className="h-11 w-full" />
          ))}
        </div>
      ) : list.error ? (
        <ErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : rows.length === 0 ? (
        <EmptyState icon={Building2Icon} title={search.query ? "Eşleşen müşteri yok" : "Henüz müşteri yok"}>
          {search.query
            ? "Farklı bir ad ya da anahtar deneyin."
            : "Uygulamalarınız olaylarla birlikte müşteri bilgisini (customer.id, customer.name) gönderdiğinde müşteriler burada listelenir."}
        </EmptyState>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-[13px]">
            <thead className="border-b text-left text-[12px] text-muted-foreground">
              <tr className={gutter}>
                <th className="py-2 pr-3 pl-4 font-medium sm:pl-6 lg:pl-8">Müşteri</th>
                <th className="px-3 py-2 font-medium">Durum</th>
                <th className="px-3 py-2 font-medium">Eğilim</th>
                <th className="px-3 py-2 text-right font-medium">Olay</th>
                <th className="px-3 py-2 text-right font-medium">Açık hata</th>
                <th className="px-3 py-2 text-right font-medium">Yeni</th>
                <th className="px-3 py-2 text-right font-medium">Proje</th>
                <th className="py-2 pr-4 pl-3 text-right font-medium sm:pr-6 lg:pr-8">Son görülme</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((c) => (
                <tr key={c.customer?.id} className="group relative hover:bg-accent/40">
                  <td className="min-w-56 py-2.5 pr-3 pl-4 sm:pl-6 lg:pl-8">
                    <Link
                      to="/o/$org/customers/$key"
                      params={{ org: org.slug, key: c.customer?.key ?? "" }}
                      className="block after:absolute after:inset-0"
                    >
                      <span className="block truncate font-medium">
                        {c.customer?.name || c.customer?.key}
                      </span>
                      <span className="block truncate font-mono text-[11.5px] text-muted-foreground">
                        {c.customer?.key}
                      </span>
                    </Link>
                  </td>
                  <td className="px-3 py-2.5">
                    <HealthLabel health={c.health} />
                  </td>
                  <td className="px-3 py-2.5">
                    <Sparkline series={c.series} className="h-5 w-24" />
                  </td>
                  <td className="px-3 py-2.5 text-right tabular">{formatCompact(c.periodEvents)}</td>
                  <td className="px-3 py-2.5 text-right tabular">{formatNumber(c.unresolvedIssues)}</td>
                  <td className="px-3 py-2.5 text-right tabular">{formatNumber(c.newIssues)}</td>
                  <td className="px-3 py-2.5 text-right tabular">{formatNumber(c.affectedProjects)}</td>
                  <td className="py-2.5 pr-4 pl-3 text-right text-muted-foreground sm:pr-6 lg:pr-8">
                    <RelativeTime value={c.customer?.lastSeen} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage ? (
        <div className="flex justify-center py-4">
          <Button
            variant="outline"
            size="sm"
            onClick={() => void list.fetchNextPage()}
            disabled={list.isFetchingNextPage}
          >
            Daha fazla müşteri
          </Button>
        </div>
      ) : null}
    </div>
  );
}
