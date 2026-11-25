import { useQuery } from "@connectrpc/connect-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { ChevronRightIcon, DownloadIcon, PrinterIcon } from "lucide-react";
import { PeriodSelect, parsePeriod } from "@/components/app/filters";
import { gutter, useDocumentTitle } from "@/components/app/page";
import { StackedBars } from "@/components/charts/stacked-bars";
import { ErrorState } from "@/components/common/states";
import { HealthLabel } from "@/components/customer/health";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { IssueStatus } from "@/gen/izci/api/v1/common_pb";
import { CustomerService } from "@/gen/izci/api/v1/customer_pb";
import type { IssueSummary } from "@/gen/izci/api/v1/issue_pb";
import { downloadText, toCsv } from "@/lib/csv";
import { formatDate, formatDateTime, formatNumber } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { seriesPoints, seriesStep } from "@/lib/series";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/o/$org/customer-report/$key")({
  validateSearch: (s: Record<string, unknown>): { period?: string } => ({
    period: typeof s.period === "string" ? s.period : undefined,
  }),
  component: CustomerReport,
});

const periodDays: Record<string, number> = {
  "1h": 1 / 24,
  "24h": 1,
  "7d": 7,
  "14d": 14,
  "30d": 30,
  "90d": 90,
};

const statusText: Record<number, string> = {
  [IssueStatus.UNRESOLVED]: "Açık",
  [IssueStatus.RESOLVED]: "Çözüldü",
  [IssueStatus.IGNORED]: "Yoksayıldı",
};

/** CSV of the report's issues. */
function issuesCsv(issues: IssueSummary[]): string {
  const head = [
    "Kısa kimlik",
    "Başlık",
    "Proje",
    "Durum",
    "Dönemdeki olay",
    "Kullanıcı",
    "İlk görülme",
    "Son görülme",
  ];
  return toCsv(
    head,
    issues.map((i) => [
      i.shortId,
      i.title,
      i.project?.name ?? "",
      statusText[i.status] ?? "",
      i.periodCount,
      i.usersCount,
      formatDateTime(i.firstSeen),
      formatDateTime(i.lastSeen),
    ]),
  );
}

/** A printable health report of one customer over a period. */
function CustomerReport() {
  const org = useOrg();
  const { key } = Route.useParams();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const period = parsePeriod(search.period, "30d");
  const q = useQuery(CustomerService.method.getCustomer, { key, period: period.value });
  const d = q.data;
  const s = d?.summary;
  const c = s?.customer;
  useDocumentTitle(c ? `${c.name || c.key} sağlık raporu` : undefined);

  if (q.isPending) {
    return (
      <div className={cn("space-y-4 py-6", gutter)}>
        <Skeleton className="h-7 w-72" />
        <Skeleton className="h-52 w-full" />
      </div>
    );
  }
  if (q.error || !s || !c)
    return <ErrorState error={q.error} onRetry={() => void q.refetch()} className="py-24" />;

  const end = new Date();
  const start = new Date(end.getTime() - (periodDays[period.key] ?? 30) * 86_400_000);
  const download = () =>
    downloadText(`izci-${c.key}-rapor-${end.toISOString().slice(0, 10)}.csv`, issuesCsv(d.topIssues));

  return (
    <div className={cn("mx-auto max-w-4xl space-y-8 py-6 print:max-w-none print:py-0", gutter)}>
      <div className="flex flex-wrap items-center justify-between gap-2 print:hidden">
        <nav className="flex items-center gap-1.5 text-sm text-muted-foreground" aria-label="Konum">
          <Link to="/o/$org/customers" params={{ org: org.slug }} className="hover:text-foreground">
            Müşteriler
          </Link>
          <ChevronRightIcon className="size-3.5" />
          <Link
            to="/o/$org/customers/$key"
            params={{ org: org.slug, key: c.key }}
            className="font-mono hover:text-foreground"
          >
            {c.key}
          </Link>
          <ChevronRightIcon className="size-3.5" />
          <span className="text-foreground">Sağlık raporu</span>
        </nav>
        <div className="flex gap-2">
          <PeriodSelect
            value={period.key}
            exclude={["1h"]}
            onChange={(k) => void navigate({ search: { period: k }, replace: true })}
          />
          <Button variant="outline" size="sm" onClick={download} disabled={d.topIssues.length === 0}>
            <DownloadIcon className="size-4" />
            CSV
          </Button>
          <Button size="sm" onClick={() => window.print()}>
            <PrinterIcon className="size-4" />
            Yazdır ya da PDF
          </Button>
        </div>
      </div>

      <header className="border-b pb-5">
        <p className="text-muted-foreground text-sm">{org.name} · Hata izleme sağlık raporu</p>
        <h1 className="mt-1 font-semibold text-2xl tracking-[-0.015em]">{c.name || c.key}</h1>
        <p className="mt-1.5 text-sm text-muted-foreground">
          {formatDate(start)} – {formatDate(end)} dönemi · oluşturulma {formatDateTime(end)}
        </p>
      </header>

      <section className="grid grid-cols-2 gap-4 sm:grid-cols-5">
        <div>
          <p className="text-muted-foreground text-sm">Durum</p>
          <HealthLabel health={s.health} className="mt-1 font-semibold text-lg" />
        </div>
        {(
          [
            ["Olay", formatNumber(s.periodEvents)],
            ["Yeni hata", formatNumber(s.newIssues)],
            ["Açık hata", formatNumber(s.unresolvedIssues)],
            ["Etkilenen proje", formatNumber(s.affectedProjects)],
          ] as const
        ).map(([label, value]) => (
          <div key={label}>
            <p className="text-muted-foreground text-sm">{label}</p>
            <p className="mt-1 font-semibold text-2xl tabular">{value}</p>
          </div>
        ))}
      </section>

      <section className="break-inside-avoid">
        <h2 className="mb-3 font-semibold text-lg">Olayların dağılımı</h2>
        <StackedBars
          height={160}
          step={seriesStep(s.series)}
          layers={[{ key: "events", label: "Olay", color: "var(--primary)", points: seriesPoints(s.series) }]}
        />
      </section>

      <section>
        <h2 className="mb-3 font-semibold text-lg">En çok etkileyen açık hatalar</h2>
        {d.topIssues.length === 0 ? (
          <p className="text-sm text-muted-foreground">Bu müşteriyi etkileyen açık hata yok.</p>
        ) : (
          <table className="w-full text-sm">
            <thead className="border-b text-left text-muted-foreground text-xs">
              <tr>
                <th className="py-2 pr-3 font-medium">Hata</th>
                <th className="py-2 pr-3 font-medium">Proje</th>
                <th className="py-2 pr-3 text-right font-medium">Olay</th>
                <th className="py-2 pr-3 text-right font-medium">Kullanıcı</th>
                <th className="py-2 font-medium">Son görülme</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {d.topIssues.map((i) => (
                <tr key={i.id} className="break-inside-avoid align-top">
                  <td className="py-2 pr-3">
                    <span className="font-mono text-muted-foreground text-xs">{i.shortId}</span>
                    <span className="block">{i.title}</span>
                  </td>
                  <td className="py-2 pr-3">{i.project?.name}</td>
                  <td className="py-2 pr-3 text-right tabular">{formatNumber(i.periodCount)}</td>
                  <td className="py-2 pr-3 text-right tabular">{formatNumber(i.usersCount)}</td>
                  <td className="whitespace-nowrap py-2">{formatDate(i.lastSeen)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {d.projects.length > 0 ? (
        <section className="break-inside-avoid">
          <h2 className="mb-3 font-semibold text-lg">Projelere göre</h2>
          <table className="w-full text-sm">
            <thead className="border-b text-left text-muted-foreground text-xs">
              <tr>
                <th className="py-2 pr-3 font-medium">Proje</th>
                <th className="py-2 pr-3 text-right font-medium">Açık hata</th>
                <th className="py-2 text-right font-medium">Olay</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {d.projects.map((p) => (
                <tr key={p.project?.id}>
                  <td className="py-2 pr-3">{p.project?.name}</td>
                  <td className="py-2 pr-3 text-right tabular">{formatNumber(p.unresolvedIssues)}</td>
                  <td className="py-2 text-right tabular">{formatNumber(p.periodEvents)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}
    </div>
  );
}
