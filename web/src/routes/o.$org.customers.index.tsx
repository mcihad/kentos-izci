import { useInfiniteQuery } from "@connectrpc/connect-query";
import { tdClass, thClass, theadClass, trClass } from "@kentos/components/common/table";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import { Input } from "@kentos/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Building2Icon, SearchIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { PeriodSelect, parsePeriod } from "@/components/app/filters";
import { gutter, Page } from "@/components/app/page";
import { Sparkline } from "@/components/charts/sparkline";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { HealthLabel, healthLabels } from "@/components/customer/health";
import { CustomerService, CustomerSort } from "@/gen/izci/api/v1/customer_pb";
import { formatCompact, formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";

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
    <Page
      icon={Building2Icon}
      title="Müşteriler"
      description="Uygulamalarınızı kullanan kurumlar ve hatalardan ne kadar etkilendikleri"
    >
      <div className={gutter}>
        <div className="card overflow-hidden max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0">
          <div className="flex flex-col gap-2 border-b px-4 py-3 sm:flex-row sm:items-center">
            <form
              className="relative flex-1"
              onSubmit={(e) => {
                e.preventDefault();
                setSearch({ query: text.trim() || undefined });
              }}
            >
              <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={text}
                onChange={(e) => setText(e.target.value)}
                onBlur={() => setSearch({ query: text.trim() || undefined })}
                placeholder="Müşteri adı ya da anahtarı"
                aria-label="Müşteri ara"
                className="pl-8"
              />
            </form>
            <div className="flex gap-2">
              <Select
                value={sort}
                onValueChange={(v) => setSearch({ sort: v === "events" ? undefined : (v as SortKey) })}
              >
                <SelectTrigger size="sm" className="text-sm" aria-label="Sıralama">
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
            <div className="space-y-2 p-4">
              {Array.from({ length: 6 }, (_, i) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: placeholders
                <Skeleton key={i} className="h-11 w-full" />
              ))}
            </div>
          ) : list.error ? (
            <ErrorState error={list.error} onRetry={() => void list.refetch()} />
          ) : rows.length === 0 ? (
            <EmptyState
              icon={Building2Icon}
              title={search.query ? "Eşleşen müşteri yok" : "Henüz müşteri yok"}
            >
              {search.query
                ? "Farklı bir ad ya da anahtar deneyin."
                : "Uygulamalarınız olaylarla birlikte müşteri bilgisini (customer.id, customer.name) gönderdiğinde müşteriler burada listelenir."}
            </EmptyState>
          ) : (
            <>
              <ul className="divide-y md:hidden">
                {rows.map((c) => (
                  <li
                    key={c.customer?.id}
                    className="relative flex items-start gap-3 px-4 py-3.5 hover:bg-(--table-row-hover)"
                  >
                    <span
                      aria-hidden="true"
                      className="mt-2 size-2.5 shrink-0 rounded-full"
                      style={{ background: healthLabels[c.health].color }}
                    />
                    <div className="min-w-0 flex-1">
                      <Link
                        to="/o/$org/customers/$key"
                        params={{ org: org.slug, key: c.customer?.key ?? "" }}
                        className="block truncate font-semibold text-base after:absolute after:inset-0"
                      >
                        {c.customer?.name || c.customer?.key}
                      </Link>
                      <p className="truncate font-mono text-sm text-muted-foreground">{c.customer?.key}</p>
                      <p className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
                        <span>{healthLabels[c.health].label}</span>
                        <span className="tabular">
                          <span className="font-semibold text-foreground">
                            {formatNumber(c.unresolvedIssues)}
                          </span>{" "}
                          açık hata
                        </span>
                        {c.newIssues > 0 ? (
                          <span className="tabular">
                            <span className="font-semibold text-foreground">{formatNumber(c.newIssues)}</span>{" "}
                            yeni
                          </span>
                        ) : null}
                        <RelativeTime value={c.customer?.lastSeen} />
                      </p>
                    </div>
                    <div className="shrink-0 text-right">
                      <span className="block font-semibold text-lg tabular leading-tight">
                        {formatCompact(c.periodEvents)}
                      </span>
                      <span className="text-xs text-muted-foreground">olay</span>
                    </div>
                  </li>
                ))}
              </ul>
              <div className="hidden overflow-x-auto md:block">
                <table className="w-full min-w-[760px] text-sm">
                  <thead className={theadClass}>
                    <tr>
                      <th className={cn(thClass, "pl-4")}>Müşteri</th>
                      <th className={thClass}>Durum</th>
                      <th className={thClass}>Eğilim</th>
                      <th className={cn(thClass, "text-right")}>Olay</th>
                      <th className={cn(thClass, "text-right")}>Açık hata</th>
                      <th className={cn(thClass, "text-right")}>Yeni</th>
                      <th className={cn(thClass, "text-right")}>Proje</th>
                      <th className={cn(thClass, "pr-4 text-right")}>Son görülme</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {rows.map((c) => (
                      <tr key={c.customer?.id} className={cn("group relative", trClass)}>
                        <td className="min-w-56 py-2.5 pr-3.5 pl-4">
                          <Link
                            to="/o/$org/customers/$key"
                            params={{ org: org.slug, key: c.customer?.key ?? "" }}
                            className="block after:absolute after:inset-0"
                          >
                            <span className="block truncate font-medium">
                              {c.customer?.name || c.customer?.key}
                            </span>
                            <span className="block truncate font-mono text-sm text-muted-foreground">
                              {c.customer?.key}
                            </span>
                          </Link>
                        </td>
                        <td className={tdClass}>
                          <HealthLabel health={c.health} />
                        </td>
                        <td className={tdClass}>
                          <Sparkline series={c.series} className="h-5 w-24" />
                        </td>
                        <td className={cn(tdClass, "text-right tabular")}>{formatCompact(c.periodEvents)}</td>
                        <td className={cn(tdClass, "text-right tabular")}>
                          {formatNumber(c.unresolvedIssues)}
                        </td>
                        <td className={cn(tdClass, "text-right tabular")}>{formatNumber(c.newIssues)}</td>
                        <td className={cn(tdClass, "text-right tabular")}>
                          {formatNumber(c.affectedProjects)}
                        </td>
                        <td className="py-2.5 pr-4 pl-3.5 text-right text-muted-foreground">
                          <RelativeTime value={c.customer?.lastSeen} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
          {list.hasNextPage ? (
            <div className="flex justify-center border-t py-3">
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
      </div>
    </Page>
  );
}
