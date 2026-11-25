import { useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { StackedBars } from "@/components/charts/stacked-bars";
import { RelativeTime } from "@/components/common/relative-time";
import { StatsPeriod } from "@/gen/izci/api/v1/common_pb";
import { type Issue, IssueService, type TagFacet } from "@/gen/izci/api/v1/issue_pb";
import { formatCompact, formatDateTime, formatNumber } from "@/lib/format";
import { levels } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { seriesPoints, seriesStep } from "@/lib/series";
import { MergedIssues } from "./merge";
import { IssueOwners } from "./owners";
import { IssueTickets } from "./tickets";

const strategyLabels: Record<string, string> = {
  custom: "uygulamanın gönderdiği parmak izi",
  rule: "projenin parmak izi kuralı",
  stacktrace: "yığın izindeki uygulama çerçeveleri",
  exception: "istisna türü ve normalize edilmiş mesaj",
  message: "mesaj şablonu",
};

function IssueChart({
  issueId,
  period,
  title,
  color,
}: {
  issueId: string;
  period: StatsPeriod;
  title: string;
  color: string;
}) {
  const stats = useQuery(IssueService.method.getIssueStats, { issueId, period }, { staleTime: 60_000 });
  const points = seriesPoints(stats.data?.series);
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between text-sm">
        <span className="text-muted-foreground">{title}</span>
        <span className="font-medium tabular">{stats.data ? formatNumber(stats.data.total) : "…"}</span>
      </div>
      <StackedBars
        layers={[{ key: "events", label: "Olay", color, points }]}
        step={seriesStep(stats.data?.series)}
        height={84}
      />
    </div>
  );
}

export function IssueAside({
  issue,
  facets,
  writable = false,
}: {
  issue: Issue;
  facets: TagFacet[] | undefined;
  writable?: boolean;
}) {
  const org = useOrg();
  const s = issue.summary;
  if (!s) return null;
  const color = levels[s.level].color;
  const customers = facets?.find((f) => f.key === "customer");
  return (
    <div className="grid grid-cols-1 gap-6">
      <IssueChart issueId={s.id} period={StatsPeriod.LAST_24_HOURS} title="Son 24 saat" color={color} />
      <IssueChart issueId={s.id} period={StatsPeriod.LAST_30_DAYS} title="Son 30 gün" color={color} />

      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2.5 text-md">
        <dt className="text-muted-foreground">İlk görülme</dt>
        <dd className="text-right">
          <RelativeTime value={s.firstSeen} />
          {issue.firstRelease ? (
            <span className="block font-mono text-sm text-muted-foreground">{issue.firstRelease}</span>
          ) : null}
        </dd>
        <dt className="text-muted-foreground">Son görülme</dt>
        <dd className="text-right">
          <RelativeTime value={s.lastSeen} />
          {issue.lastRelease ? (
            <span className="block font-mono text-sm text-muted-foreground">{issue.lastRelease}</span>
          ) : null}
        </dd>
        <dt className="text-muted-foreground">Toplam olay</dt>
        <dd className="text-right tabular">{formatNumber(s.timesSeen)}</dd>
        <dt className="text-muted-foreground">Kullanıcı</dt>
        <dd className="text-right tabular">{formatNumber(s.usersCount)}</dd>
        <dt className="text-muted-foreground">Müşteri</dt>
        <dd className="text-right tabular">{formatNumber(s.customersCount)}</dd>
        {issue.resolvedAt ? (
          <>
            <dt className="text-muted-foreground">Çözüldü</dt>
            <dd className="text-right">
              <span title={formatDateTime(issue.resolvedAt)}>
                <RelativeTime value={issue.resolvedAt} />
              </span>
              {issue.resolvedBy ? (
                <span className="block text-sm text-muted-foreground">
                  {issue.resolvedBy.name || issue.resolvedBy.email}
                </span>
              ) : null}
              {issue.resolvedInRelease ? (
                <span className="block font-mono text-sm text-muted-foreground">
                  {issue.resolvedInRelease} sürümünde
                </span>
              ) : null}
            </dd>
          </>
        ) : null}
        {issue.snooze ? (
          <>
            <dt className="text-muted-foreground">Yoksayma</dt>
            <dd className="text-right text-sm">
              {issue.snooze.until ? (
                <span className="block">
                  <RelativeTime value={issue.snooze.until} /> bitiyor
                </span>
              ) : null}
              {issue.snooze.untilCount > 0n ? (
                <span className="block tabular">
                  {formatNumber(issue.snooze.countSince)} / {formatNumber(issue.snooze.untilCount)} olay
                </span>
              ) : null}
              {issue.snooze.untilEscalating ? <span className="block">Artış görülürse biter</span> : null}
              {!issue.snooze.until && issue.snooze.untilCount === 0n && !issue.snooze.untilEscalating
                ? "Süresiz"
                : null}
            </dd>
          </>
        ) : null}
      </dl>

      <IssueOwners issue={issue} writable={writable} />

      <IssueTickets issue={issue} writable={writable} />

      {customers && customers.values.length > 0 ? (
        <div>
          <h3 className="mb-2.5 font-semibold text-md">Etkilenen müşteriler</h3>
          <ul className="grid gap-2">
            {customers.values.slice(0, 6).map((v) => (
              <li key={v.value} className="flex items-center justify-between gap-3 text-md">
                <Link
                  to="/o/$org/customers/$key"
                  params={{ org: org.slug, key: v.value }}
                  className="min-w-0 truncate underline-offset-4 hover:underline"
                >
                  {v.label || v.value}
                </Link>
                <span className="shrink-0 text-muted-foreground tabular">{formatCompact(v.count)}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {issue.grouping ? (
        <div>
          <h3 className="mb-2 font-semibold text-md">Neden bu grupta?</h3>
          <p className="text-sm text-muted-foreground leading-relaxed">
            Bu olaylar {strategyLabels[issue.grouping.strategy] ?? issue.grouping.strategy} aynı olduğu için
            tek bir hatada toplandı.
          </p>
          {issue.grouping.rule ? (
            <p className="mt-1.5 rounded-md bg-(--surface-muted) px-2 py-1 font-mono text-xs [overflow-wrap:anywhere]">
              {issue.grouping.rule}
            </p>
          ) : null}
          {issue.grouping.components.length ? (
            <ul className="mt-2 grid gap-1 font-mono text-sm text-muted-foreground">
              {issue.grouping.components.slice(0, 12).map((c) => (
                <li key={c} className="truncate" title={c}>
                  {c}
                </li>
              ))}
            </ul>
          ) : null}
          <p className="mt-2 font-mono text-xs text-muted-foreground [overflow-wrap:anywhere]">
            {issue.grouping.fingerprint}
          </p>
        </div>
      ) : null}
      {issue.merged.length > 0 ? <MergedIssues issue={issue} writable={writable} /> : null}
    </div>
  );
}
