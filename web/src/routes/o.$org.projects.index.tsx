import { useQuery } from "@connectrpc/connect-query";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { createFileRoute, Link } from "@tanstack/react-router";
import { BoxesIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { gutter, PageHeader } from "@/components/app/page";
import { Sparkline } from "@/components/charts/sparkline";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState, ErrorState } from "@/components/common/states";
import { NewProjectDialog } from "@/components/project/new-project";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { formatCompact, formatNumber } from "@/lib/format";
import { platformLabel } from "@/lib/labels";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";

export const Route = createFileRoute("/o/$org/projects/")({
  component: ProjectsPage,
});

function ProjectsPage() {
  const org = useOrg();
  const projects = useQuery(ProjectService.method.listProjects, {});
  const [creating, setCreating] = useState(false);
  const list = projects.data?.projects ?? [];

  return (
    <div className="pb-12">
      <PageHeader
        title="Projeler"
        description="Hata gönderen uygulamalar ve servisler"
        actions={
          canAdmin(org.role) ? (
            <Button size="sm" onClick={() => setCreating(true)}>
              <PlusIcon className="size-4" />
              Yeni proje
            </Button>
          ) : null
        }
      />
      {projects.isPending ? (
        <div className={cn("space-y-2", gutter)}>
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
        </div>
      ) : projects.error ? (
        <ErrorState error={projects.error} onRetry={() => void projects.refetch()} />
      ) : list.length === 0 ? (
        <EmptyState
          icon={BoxesIcon}
          className={cn("card mx-4 sm:mx-(--gutter)")}
          title="Henüz proje yok"
          action={
            canAdmin(org.role) ? (
              <Button size="sm" onClick={() => setCreating(true)}>
                İlk projeyi oluştur
              </Button>
            ) : null
          }
        >
          Hata göndermek istediğiniz her uygulama için bir proje oluşturun.
        </EmptyState>
      ) : (
        <div className={gutter}>
          <ul className="card divide-y overflow-hidden max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0">
            {list.map((p) => (
              <li
                key={p.id}
                className="relative transition-colors duration-(--duration-fast) hover:bg-(--table-row-hover)"
              >
                <div className="flex items-center gap-4 px-4 py-3.5 sm:px-(--card-p)">
                  <div className="min-w-0 flex-1">
                    <Link
                      to="/o/$org/projects/$slug"
                      params={{ org: org.slug, slug: p.slug }}
                      className="font-medium text-md after:absolute after:inset-0"
                    >
                      {p.name}
                    </Link>
                    <p className="mt-0.5 flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                      <span className="font-mono">{p.slug}</span>
                      <span>{platformLabel(p.platform)}</span>
                      <span>
                        {p.stats?.lastEventAt ? (
                          <>
                            Son olay <RelativeTime value={p.stats.lastEventAt} />
                          </>
                        ) : (
                          "Henüz olay gelmedi"
                        )}
                      </span>
                      {p.repository ? (
                        <span className="font-mono">
                          {p.repository.owner}/{p.repository.name}
                        </span>
                      ) : null}
                    </p>
                  </div>
                  <Sparkline series={p.stats?.series24h} className="hidden h-6 w-28 sm:block" />
                  <div className="w-16 text-right">
                    <p className="font-medium text-md tabular">{formatCompact(p.stats?.events24h ?? 0n)}</p>
                    <p className="text-xs text-muted-foreground">olay, 24 sa</p>
                  </div>
                  <div className="w-16 text-right">
                    <p className="font-medium text-md tabular">
                      {formatNumber(p.stats?.unresolvedIssues ?? 0)}
                    </p>
                    <p className="text-xs text-muted-foreground">açık hata</p>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
      <NewProjectDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}
