import { useInfiniteQuery, useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { StatsPeriod } from "@/gen/izci/api/v1/common_pb";
import { type Project, ProjectService } from "@/gen/izci/api/v1/project_pb";
import { formatNumber } from "@/lib/format";

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
    <div className="grid max-w-4xl grid-cols-1 gap-8 py-6">
      <section>
        <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="font-semibold text-[15px]">Sürümler</h2>
            <p className="mt-0.5 text-[13px] text-muted-foreground">
              Oturum bilgisi Sentry SDK'larının sürüm sağlığı verisinden gelir: bir oturum, mobil ya da
              masaüstü uygulamanın bir çalışması, tarayıcıda bir sayfa ziyareti ya da sunucuda bir istektir.
            </p>
          </div>
          <Select value={String(period)} onValueChange={(v) => setPeriod(Number(v) as StatsPeriod)}>
            <SelectTrigger className="h-8 w-36 text-[13px]" aria-label="Sağlık dönemi">
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
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-[860px] text-[13px]">
              <thead className="bg-muted/40 text-left text-[12px] text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 font-medium">Sürüm</th>
                  <th className="px-3 py-2 text-right font-medium">Benimsenme</th>
                  <th className="px-3 py-2 text-right font-medium">Oturum</th>
                  <th className="px-3 py-2 font-medium">Çökmesiz oturum</th>
                  <th className="px-3 py-2 font-medium">Çökmesiz kullanıcı</th>
                  <th className="px-3 py-2 text-right font-medium">Yeni hata</th>
                  <th className="px-3 py-2 font-medium">İlk görülme</th>
                  <th className="px-3 py-2 font-medium">Dağıtımlar</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((r) => (
                  <tr key={r.id}>
                    <td className="px-3 py-2">
                      <span className="block font-medium font-mono text-[12.5px]">{r.version}</span>
                      {r.commitSha ? (
                        <span className="font-mono text-[11.5px] text-muted-foreground">
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
                    <td className="px-3 py-2 text-right tabular text-muted-foreground">
                      {r.health ? `%${Math.round(r.health.adoption).toLocaleString("tr-TR")}` : "—"}
                    </td>
                    <td className="px-3 py-2 text-right tabular">
                      {r.health ? (
                        formatNumber(r.health.sessions)
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-3 py-2">
                      {r.health ? (
                        <span title={`${formatNumber(r.health.crashedSessions)} çökme`}>
                          <Rate value={r.health.crashFreeSessions} />
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-3 py-2">
                      {r.health ? (
                        <span title={`${formatNumber(r.health.users)} kullanıcı`}>
                          <Rate value={r.health.crashFreeUsers} empty={r.health.users === 0n} />
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right tabular">{formatNumber(r.newIssues)}</td>
                    <td className="px-3 py-2 text-muted-foreground">
                      <RelativeTime value={r.firstSeen} />
                    </td>
                    <td className="px-3 py-2 text-[12px] text-muted-foreground">
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
        )}
        {releases.hasNextPage ? (
          <Button variant="ghost" size="sm" className="mt-2" onClick={() => void releases.fetchNextPage()}>
            Daha eski sürümler
          </Button>
        ) : null}
      </section>
      <section>
        <h2 className="mb-3 font-semibold text-[15px]">Ortamlar</h2>
        {envs.data?.environments.length ? (
          <ul className="divide-y rounded-lg border">
            {envs.data.environments.map((e) => (
              <li key={e.name} className="flex items-center justify-between gap-3 px-3 py-2 text-[13px]">
                <span className="font-mono text-[12.5px]">{e.name}</span>
                <span className="text-[12px] text-muted-foreground">
                  Son olay <RelativeTime value={e.lastSeen} />
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-[13px] text-muted-foreground">Henüz ortam görülmedi.</p>
        )}
      </section>
    </div>
  );
}
