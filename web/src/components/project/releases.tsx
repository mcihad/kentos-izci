import { useInfiniteQuery, useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import { thClass, theadClass, trClass } from "@/components/common/table";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { StatsPeriod } from "@/gen/izci/api/v1/common_pb";
import { type Project, ProjectService } from "@/gen/izci/api/v1/project_pb";
import { formatNumber } from "@/lib/format";
import { cn } from "@/lib/utils";

const periods = [
  { value: StatsPeriod.LAST_24_HOURS, label: "Son 24 saat" },
  { value: StatsPeriod.LAST_7_DAYS, label: "Son 7 gün" },
  { value: StatsPeriod.LAST_14_DAYS, label: "Son 14 gün" },
  { value: StatsPeriod.LAST_30_DAYS, label: "Son 30 gün" },
];

/** A crash-free rate, never rounded up to 100 while crashes exist. */
function rate(v: number): string {
  const floored = Math.floor(v * 100) / 100;
  return `%${floored.toLocaleString("tr-TR", { maximumFractionDigits: 2 })}`;
}

function rateColor(v: number): string {
  return v >= 99.5 ? "var(--health-good)" : v >= 98 ? "var(--level-warning)" : "var(--level-error)";
}

function Rate({ value, empty }: { value: number; empty?: boolean }) {
  if (empty) return <span className="text-muted-foreground">—</span>;
  return (
    <span className="inline-flex items-center gap-1.5 tabular">
      <span aria-hidden className="size-1.5 rounded-full" style={{ background: rateColor(value) }} />
      {rate(value)}
    </span>
  );
}

export function ProjectReleases({ project }: { project: Project }) {
  const [period, setPeriod] = useState(StatsPeriod.LAST_14_DAYS);
  const releases = useInfiniteQuery(
    ProjectService.method.listReleases,
    { project: project.slug, pageSize: 30, pageToken: "", healthPeriod: period },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const envs = useQuery(ProjectService.method.listEnvironments, { project: project.slug });
  const rows = releases.data?.pages.flatMap((p) => p.releases) ?? [];
  return (
    <div className="grid max-w-4xl grid-cols-1 *:min-w-0 gap-8 py-6">
      <section>
        <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="font-semibold text-lg">Sürümler</h2>
            <p className="mt-0.5 text-sm text-muted-foreground">
              Oturum bilgisi Sentry SDK'larının sürüm sağlığı verisinden gelir: bir oturum, mobil ya da
              masaüstü uygulamanın bir çalışması, tarayıcıda bir sayfa ziyareti ya da sunucuda bir istektir.
            </p>
          </div>
          <Select value={String(period)} onValueChange={(v) => setPeriod(Number(v) as StatsPeriod)}>
            <SelectTrigger size="sm" className="w-36 text-sm" aria-label="Sağlık dönemi">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {periods.map((p) => (
                <SelectItem key={p.value} value={String(p.value)}>
                  {p.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {releases.isPending ? (
          <Skeleton className="h-40 w-full" />
        ) : rows.length === 0 ? (
          <EmptyState title="Henüz sürüm görülmedi" className="rounded-lg border">
            Olaylarda <code className="font-mono">release</code> alanını doldurduğunuzda ya da CI'dan sürüm
            kaydettiğinizde burada listelenir.
          </EmptyState>
        ) : (
          <>
            <ul className="divide-y rounded-lg border md:hidden">
              {rows.map((r) => (
                <li key={r.id} className="px-4 py-3.5">
                  <div className="flex items-baseline justify-between gap-3">
                    <span className="min-w-0 font-mono font-semibold text-md [overflow-wrap:anywhere]">
                      {r.version}
                    </span>
                    <span className="shrink-0 text-sm text-muted-foreground">
                      <RelativeTime value={r.firstSeen} />
                    </span>
                  </div>
                  {r.health ? (
                    <dl className="mt-2.5 grid grid-cols-3 gap-2 text-sm">
                      <div className="flex flex-col-reverse">
                        <dt className="text-xs text-muted-foreground">çökmesiz oturum</dt>
                        <dd>
                          <Rate value={r.health.crashFreeSessions} />
                        </dd>
                      </div>
                      <div className="flex flex-col-reverse">
                        <dt className="text-xs text-muted-foreground">çökmesiz kullanıcı</dt>
                        <dd>
                          <Rate value={r.health.crashFreeUsers} empty={r.health.users === 0n} />
                        </dd>
                      </div>
                      <div className="flex flex-col-reverse">
                        <dt className="text-xs text-muted-foreground">oturum, benimsenme</dt>
                        <dd className="tabular">
                          {formatNumber(r.health.sessions)}, %
                          {Math.round(r.health.adoption).toLocaleString("tr-TR")}
                        </dd>
                      </div>
                    </dl>
                  ) : null}
                  <p className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
                    <span className="tabular">
                      <span className="font-semibold text-foreground">{formatNumber(r.newIssues)}</span> yeni
                      hata
                    </span>
                    {r.commitSha ? <span className="font-mono">{r.commitSha.slice(0, 10)}</span> : null}
                    {r.deploys.length ? (
                      <span>
                        {r.deploys
                          .slice(0, 3)
                          .map((d) => d.environment)
                          .join(", ")}
                      </span>
                    ) : null}
                  </p>
                </li>
              ))}
            </ul>
            <div className="hidden overflow-x-auto rounded-lg border md:block">
              <table className="w-full min-w-[860px] text-sm">
                <thead className={theadClass}>
                  <tr>
                    <th className={thClass}>Sürüm</th>
                    <th className={cn(thClass, "text-right")}>Benimsenme</th>
                    <th className={cn(thClass, "text-right")}>Oturum</th>
                    <th className={thClass}>Çökmesiz oturum</th>
                    <th className={thClass}>Çökmesiz kullanıcı</th>
                    <th className={cn(thClass, "text-right")}>Yeni hata</th>
                    <th className={thClass}>İlk görülme</th>
                    <th className={thClass}>Dağıtımlar</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {rows.map((r) => (
                    <tr key={r.id} className={trClass}>
                      <td className="px-3.5 py-2">
                        <span className="block font-medium font-mono text-sm">{r.version}</span>
                        {r.commitSha ? (
                          <span className="font-mono text-xs text-muted-foreground">
                            {r.url ? (
                              <a href={r.url} target="_blank" rel="noreferrer" className="hover:underline">
                                {r.commitSha.slice(0, 10)}
                              </a>
                            ) : (
                              r.commitSha.slice(0, 10)
                            )}
                          </span>
                        ) : null}
                      </td>
                      <td className="px-3.5 py-2 text-right tabular text-muted-foreground">
                        {r.health ? `%${Math.round(r.health.adoption).toLocaleString("tr-TR")}` : "—"}
                      </td>
                      <td className="px-3.5 py-2 text-right tabular">
                        {r.health ? (
                          formatNumber(r.health.sessions)
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-3.5 py-2">
                        {r.health ? (
                          <span title={`${formatNumber(r.health.crashedSessions)} çökme`}>
                            <Rate value={r.health.crashFreeSessions} />
                          </span>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-3.5 py-2">
                        {r.health ? (
                          <span title={`${formatNumber(r.health.users)} kullanıcı`}>
                            <Rate value={r.health.crashFreeUsers} empty={r.health.users === 0n} />
                          </span>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-3.5 py-2 text-right tabular">{formatNumber(r.newIssues)}</td>
                      <td className="px-3.5 py-2 text-muted-foreground">
                        <RelativeTime value={r.firstSeen} />
                      </td>
                      <td className="px-3.5 py-2 text-xs text-muted-foreground">
                        {r.deploys.length
                          ? r.deploys
                              .slice(0, 3)
                              .map((d) => d.environment)
                              .join(", ")
                          : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
        {releases.hasNextPage ? (
          <Button variant="ghost" size="sm" className="mt-2" onClick={() => void releases.fetchNextPage()}>
            Daha eski sürümler
          </Button>
        ) : null}
      </section>
      <section>
        <h2 className="mb-3 font-semibold text-lg">Ortamlar</h2>
        {envs.data?.environments.length ? (
          <ul className="divide-y rounded-lg border">
            {envs.data.environments.map((e) => (
              <li key={e.name} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                <span className="font-mono text-sm">{e.name}</span>
                <span className="text-xs text-muted-foreground">
                  Son olay <RelativeTime value={e.lastSeen} />
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">Henüz ortam görülmedi.</p>
        )}
      </section>
    </div>
  );
}
