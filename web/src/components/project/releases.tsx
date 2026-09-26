import { useInfiniteQuery, useQuery } from "@connectrpc/connect-query";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { type Project, ProjectService } from "@/gen/izci/api/v1/project_pb";
import { formatNumber } from "@/lib/format";

export function ProjectReleases({ project }: { project: Project }) {
  const releases = useInfiniteQuery(
    ProjectService.method.listReleases,
    { project: project.slug, pageSize: 30, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const envs = useQuery(ProjectService.method.listEnvironments, { project: project.slug });
  const rows = releases.data?.pages.flatMap((p) => p.releases) ?? [];
  return (
    <div className="grid max-w-4xl grid-cols-1 gap-8 py-6">
      <section>
        <h2 className="mb-3 font-semibold text-[15px]">Sürümler</h2>
        {releases.isPending ? (
          <Skeleton className="h-40 w-full" />
        ) : rows.length === 0 ? (
          <EmptyState title="Henüz sürüm görülmedi" className="rounded-lg border">
            Olaylarda <code className="font-mono">release</code> alanını doldurduğunuzda ya da CI'dan sürüm
            kaydettiğinizde burada listelenir.
          </EmptyState>
        ) : (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-[640px] text-[13px]">
              <thead className="bg-muted/40 text-left text-[12px] text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 font-medium">Sürüm</th>
                  <th className="px-3 py-2 font-medium">Commit</th>
                  <th className="px-3 py-2 font-medium">İlk olay</th>
                  <th className="px-3 py-2 font-medium">Son olay</th>
                  <th className="px-3 py-2 text-right font-medium">Yeni hata</th>
                  <th className="px-3 py-2 font-medium">Dağıtımlar</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((r) => (
                  <tr key={r.id}>
                    <td className="px-3 py-2 font-medium font-mono text-[12.5px]">{r.version}</td>
                    <td className="px-3 py-2 font-mono text-[12px] text-muted-foreground">
                      {r.commitSha ? (
                        r.url ? (
                          <a href={r.url} target="_blank" rel="noreferrer" className="hover:underline">
                            {r.commitSha.slice(0, 10)}
                          </a>
                        ) : (
                          r.commitSha.slice(0, 10)
                        )
                      ) : (
                        "—"
                      )}
                    </td>
                    <td className="px-3 py-2 text-muted-foreground">
                      <RelativeTime value={r.firstSeen} />
                    </td>
                    <td className="px-3 py-2 text-muted-foreground">
                      <RelativeTime value={r.lastSeen} />
                    </td>
                    <td className="px-3 py-2 text-right tabular">{formatNumber(r.newIssues)}</td>
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
