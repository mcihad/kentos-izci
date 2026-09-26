import { useQuery } from "@connectrpc/connect-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { ArrowDownRightIcon, ArrowUpRightIcon } from "lucide-react";
import { useMemo } from "react";
import { EnvironmentSelect } from "@/components/app/environment-select";
import { PeriodSelect, ProjectFilter, parsePeriod } from "@/components/app/filters";
import { gutter, PageHeader, Section } from "@/components/app/page";
import { Sparkline } from "@/components/charts/sparkline";
import { StackedBars } from "@/components/charts/stacked-bars";
import { StatStrip } from "@/components/common/stat-strip";
import { EmptyState, ErrorState } from "@/components/common/states";
import { healthLabels } from "@/components/customer/health";
import { IssueRow } from "@/components/issue/issue-row";
import { Skeleton } from "@/components/ui/skeleton";
import { DashboardService } from "@/gen/izci/api/v1/dashboard_pb";
import type { IssueSummary } from "@/gen/izci/api/v1/issue_pb";
import { formatCompact, formatNumber, formatPercent } from "@/lib/format";
import { levels, periods } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { seriesPoints, seriesStep } from "@/lib/series";
import { cn } from "@/lib/utils";

type OverviewSearch = { period?: string; projects?: string; env?: string };

export const Route = createFileRoute("/o/$org/")({
  validateSearch: (s: Record<string, unknown>): OverviewSearch => ({
    period: typeof s.period === "string" ? s.period : undefined,
    projects: typeof s.projects === "string" && s.projects ? s.projects : undefined,
    env: typeof s.env === "string" && s.env ? s.env : undefined,
  }),
  component: OverviewPage,
});

function OverviewPage() {
  const org = useOrg();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const period = parsePeriod(search.period, "24h");
  const projects = useMemo(() => (search.projects ? search.projects.split(",") : []), [search.projects]);
  const overview = useQuery(
    DashboardService.method.getOverview,
    { period: period.value, projects, environments: search.env ? [search.env] : [] },
    { refetchInterval: 60_000 },
  );
  const setSearch = (patch: Partial<OverviewSearch>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });
  const d = overview.data;
  const t = d?.totals;
  const periodLabel = periods.find((p) => p.value === period.value)?.label.toLocaleLowerCase("tr-TR") ?? "";

  const trend = (() => {
    if (!t || t.previousEvents === 0n) return undefined;
    const change = (Number(t.events) - Number(t.previousEvents)) / Number(t.previousEvents);
    if (Math.abs(change) < 0.005) return <span>önceki döneme göre aynı</span>;
    const up = change > 0;
    const Icon = up ? ArrowUpRightIcon : ArrowDownRightIcon;
    return (
      <span className={cn("inline-flex items-center gap-0.5", up ? "text-level-error" : "text-health-good")}>
        <Icon className="size-3.5" />
        {formatPercent(Math.abs(change))} {up ? "artış" : "azalış"}
      </span>
    );
  })();

  return (
    <div className="pb-12">
      <PageHeader
        title="Genel bakış"
        description={`${org.name} uygulamalarında ${periodLabel} içinde olanlar`}
        actions={
          <>
            <ProjectFilter
              value={projects}
              onChange={(p) => setSearch({ projects: p.length ? p.join(",") : undefined })}
            />
            <EnvironmentSelect
              value={search.env ?? ""}
              onChange={(e) => setSearch({ env: e || undefined })}
            />
            <PeriodSelect
              value={period.key}
              onChange={(k) => setSearch({ period: k === "24h" ? undefined : k })}
              exclude={["90d"]}
            />
          </>
        }
      />
      <div className={cn("space-y-9 pt-6", gutter)}>
        {overview.error ? (
          <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
        ) : !d || !t ? (
          <OverviewSkeleton />
        ) : (
          <>
            <StatStrip
              stats={[
                { label: "Olay", value: formatCompact(t.events), note: trend },
                {
                  label: "Dakikada olay",
                  value: t.eventsPerMinute.toLocaleString("tr-TR", { maximumFractionDigits: 1 }),
                  note: "son 15 dakika",
                },
                { label: "Yeni hata", value: formatNumber(t.newIssues) },
                { label: "Geri dönen hata", value: formatNumber(t.regressedIssues) },
                { label: "Etkilenen müşteri", value: formatNumber(t.affectedCustomers) },
                { label: "Etkilenen kullanıcı", value: formatNumber(t.affectedUsers) },
              ]}
            />

            <Section
              title="Olay hacmi"
              description={`${formatNumber(t.unresolvedIssues)} çözülmemiş hata`}
              actions={
                <ul className="flex flex-wrap gap-x-3 text-[12px] text-muted-foreground">
                  {d.eventsByLevel.map((l) => (
                    <li key={l.level} className="flex items-center gap-1.5">
                      <span className="size-2 rounded-[2px]" style={{ background: levels[l.level].color }} />
                      {levels[l.level].label}
                    </li>
                  ))}
                </ul>
              }
            >
              {d.eventsByLevel.length === 0 ? (
                <EmptyState title="Bu dönemde olay yok" className="rounded-lg border py-10" />
              ) : (
                <StackedBars
                  height={200}
                  step={seriesStep(d.eventsByLevel[0]?.series)}
                  layers={d.eventsByLevel.map((l) => ({
                    key: String(l.level),
                    label: levels[l.level].label,
                    color: levels[l.level].color,
                    points: seriesPoints(l.series),
                  }))}
                />
              )}
            </Section>

            <div className="grid grid-cols-1 gap-9 xl:grid-cols-2">
              <Section
                title="Hızla artanlar"
                description="Önceki döneme göre en çok artan çözülmemiş hatalar"
              >
                <IssueList issues={d.trendingIssues} empty="Artış gösteren hata yok." />
              </Section>
              <Section title="Yeni hatalar" description="Bu dönemde ilk kez görülenler">
                <IssueList issues={d.newIssues} empty="Bu dönemde yeni hata çıkmadı." />
              </Section>
            </div>

            <div className="grid grid-cols-1 gap-9 xl:grid-cols-2">
              <Section title="Projeler">
                <div className="overflow-hidden rounded-lg border">
                  <table className="w-full text-[13px]">
                    <thead className="bg-muted/40 text-left text-[12px] text-muted-foreground">
                      <tr>
                        <th className="px-3 py-2 font-medium">Proje</th>
                        <th className="hidden px-3 py-2 font-medium sm:table-cell">Eğilim</th>
                        <th className="px-3 py-2 text-right font-medium">Olay</th>
                        <th className="px-3 py-2 text-right font-medium">Açık</th>
                        <th className="px-3 py-2 text-right font-medium">Yeni</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y">
                      {d.projects.map((p) => (
                        <tr key={p.project?.id} className="hover:bg-accent/40">
                          <td className="max-w-0 px-3 py-2">
                            <Link
                              to="/o/$org/issues"
                              params={{ org: org.slug }}
                              search={{ projects: p.project?.slug }}
                              className="block truncate font-medium hover:underline"
                            >
                              {p.project?.name}
                            </Link>
                          </td>
                          <td className="hidden px-3 py-1.5 sm:table-cell">
                            <Sparkline series={p.series} className="h-5 w-24" />
                          </td>
                          <td className="px-3 py-2 text-right tabular">{formatCompact(p.events)}</td>
                          <td className="px-3 py-2 text-right tabular">{formatNumber(p.unresolvedIssues)}</td>
                          <td className="px-3 py-2 text-right tabular">{formatNumber(p.newIssues)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </Section>
              <Section
                title="En çok etkilenen müşteriler"
                actions={
                  <Link
                    to="/o/$org/customers"
                    params={{ org: org.slug }}
                    className="text-[12.5px] text-muted-foreground hover:text-foreground"
                  >
                    Tüm müşteriler
                  </Link>
                }
              >
                {d.customers.length === 0 ? (
                  <EmptyState title="Müşteri bilgisi gelmedi" className="rounded-lg border py-10">
                    Uygulamalarınız olaylarla birlikte müşteri bilgisini gönderdiğinde burada görünür.
                  </EmptyState>
                ) : (
                  <div className="overflow-hidden rounded-lg border">
                    <table className="w-full text-[13px]">
                      <thead className="bg-muted/40 text-left text-[12px] text-muted-foreground">
                        <tr>
                          <th className="px-3 py-2 font-medium">Müşteri</th>
                          <th className="hidden px-3 py-2 font-medium sm:table-cell">Eğilim</th>
                          <th className="px-3 py-2 text-right font-medium">Olay</th>
                          <th className="px-3 py-2 text-right font-medium">Açık</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y">
                        {d.customers.map((c) => (
                          <tr key={c.customer?.id} className="hover:bg-accent/40">
                            <td className="max-w-0 px-3 py-2">
                              <Link
                                to="/o/$org/customers/$key"
                                params={{ org: org.slug, key: c.customer?.key ?? "" }}
                                className="flex items-center gap-2 hover:underline"
                              >
                                <span
                                  className="size-2 shrink-0 rounded-full"
                                  style={{ background: healthLabels[c.health].color }}
                                  title={healthLabels[c.health].label}
                                />
                                <span className="truncate font-medium">
                                  {c.customer?.name || c.customer?.key}
                                </span>
                              </Link>
                            </td>
                            <td className="hidden px-3 py-1.5 sm:table-cell">
                              <Sparkline series={c.series} className="h-5 w-24" />
                            </td>
                            <td className="px-3 py-2 text-right tabular">{formatCompact(c.periodEvents)}</td>
                            <td className="px-3 py-2 text-right tabular">
                              {formatNumber(c.unresolvedIssues)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </Section>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function IssueList({ issues, empty }: { issues: IssueSummary[]; empty: string }) {
  const org = useOrg();
  if (issues.length === 0)
    return (
      <p className="rounded-lg border px-4 py-8 text-center text-[13px] text-muted-foreground">{empty}</p>
    );
  return (
    <div className="overflow-hidden rounded-lg border [&>div:last-child]:border-b-0">
      {issues.map((i) => (
        <IssueRow key={i.id} issue={i} org={org.slug} compact showUsers={false} />
      ))}
    </div>
  );
}

function OverviewSkeleton() {
  return (
    <div className="space-y-8">
      <Skeleton className="h-24 w-full" />
      <Skeleton className="h-56 w-full" />
      <div className="grid gap-6 xl:grid-cols-2">
        <Skeleton className="h-64" />
        <Skeleton className="h-64" />
      </div>
    </div>
  );
}
