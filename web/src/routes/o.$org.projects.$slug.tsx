import { useQuery } from "@connectrpc/connect-query";
import { PageBreadcrumb } from "@kentos/components/layout/page";
import { cn } from "@kentos/components/lib/utils";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { createFileRoute, Link } from "@tanstack/react-router";
import { BoxesIcon } from "lucide-react";
import { gutter, Page, useDocumentTitle } from "@/components/app/page";
import { EmptyState, ErrorState } from "@/components/common/states";
import { TabbedCard, TabStrip, tabActiveOptions, tabClass } from "@/components/common/tab-nav";
import { ProjectAi } from "@/components/project/ai";
import { ProjectFilters } from "@/components/project/filters";
import { ProjectGeneral } from "@/components/project/general";
import { ProjectGrouping } from "@/components/project/grouping";
import { ProjectKeys } from "@/components/project/keys";
import { ProjectOwnership } from "@/components/project/ownership";
import { ProjectPrivacy } from "@/components/project/privacy";
import { ProjectReleases } from "@/components/project/releases";
import { ProjectRepository } from "@/components/project/repository";
import { ProjectSetup } from "@/components/project/setup";
import { ProjectSymbols } from "@/components/project/symbols";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { isNotFound } from "@/lib/errors";
import { platformLabel } from "@/lib/labels";
import { useOrg } from "@/lib/org";

const tabs = [
  { id: "general", label: "Genel" },
  { id: "setup", label: "Kurulum" },
  { id: "keys", label: "Anahtarlar" },
  { id: "releases", label: "Sürümler" },
  { id: "symbols", label: "Kaynak eşlemeleri" },
  { id: "repository", label: "Depo" },
  { id: "ownership", label: "Sahiplik" },
  { id: "filters", label: "Filtreler" },
  { id: "grouping", label: "Gruplama" },
  { id: "privacy", label: "Gizlilik" },
  { id: "ai", label: "Yapay zekâ" },
] as const;
type Tab = (typeof tabs)[number]["id"];

export const Route = createFileRoute("/o/$org/projects/$slug")({
  validateSearch: (s: Record<string, unknown>): { tab?: Tab } => ({
    tab: typeof s.tab === "string" && tabs.some((t) => t.id === s.tab) ? (s.tab as Tab) : undefined,
  }),
  component: ProjectPage,
});

function ProjectPage() {
  const org = useOrg();
  const { slug } = Route.useParams();
  const search = Route.useSearch();
  const q = useQuery(ProjectService.method.getProject, { slug });
  const project = q.data?.project;
  useDocumentTitle(project?.name);

  if (q.isPending) {
    return (
      <div className={cn("space-y-4 py-6", gutter)}>
        <Skeleton className="h-7 w-56" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  if (!project) {
    return isNotFound(q.error) ? (
      <EmptyState title="Proje bulunamadı" className="py-24">
        <span className="font-mono">{slug}</span> adlı bir proje yok ya da silinmiş.
      </EmptyState>
    ) : (
      <ErrorState error={q.error} onRetry={() => void q.refetch()} className="py-24" />
    );
  }
  const tab: Tab = search.tab ?? (project.stats?.lastEventAt ? "general" : "setup");

  return (
    <Page
      icon={BoxesIcon}
      title={project.name}
      breadcrumb={
        <PageBreadcrumb>
          <Link to="/o/$org/projects" params={{ org: org.slug }}>
            Projeler
          </Link>
          <span className="font-mono">{project.slug}</span>
        </PageBreadcrumb>
      }
      description={
        <>
          {platformLabel(project.platform)} projesi, Sentry numarası{" "}
          <span className="font-mono">{String(project.publicId)}</span>
        </>
      }
    >
      <div className={gutter} key={project.id}>
        <TabbedCard
          tabs={
            <TabStrip label="Ayar bölümleri" active={tab} className="px-2 sm:px-3">
              {tabs.map((t) => (
                <Link
                  key={t.id}
                  from={Route.fullPath}
                  search={{ tab: t.id }}
                  activeOptions={tabActiveOptions}
                  className={tabClass(tab === t.id)}
                  aria-current={tab === t.id ? "page" : undefined}
                >
                  {t.label}
                </Link>
              ))}
            </TabStrip>
          }
        >
          {tab === "general" ? <ProjectGeneral project={project} /> : null}
          {tab === "setup" ? <ProjectSetup project={project} /> : null}
          {tab === "keys" ? <ProjectKeys project={project} /> : null}
          {tab === "releases" ? <ProjectReleases project={project} /> : null}
          {tab === "symbols" ? <ProjectSymbols project={project} /> : null}
          {tab === "repository" ? <ProjectRepository project={project} /> : null}
          {tab === "ownership" ? <ProjectOwnership project={project} /> : null}
          {tab === "filters" ? <ProjectFilters project={project} /> : null}
          {tab === "grouping" ? <ProjectGrouping key={project.id} project={project} /> : null}
          {tab === "privacy" ? <ProjectPrivacy project={project} /> : null}
          {tab === "ai" ? <ProjectAi project={project} /> : null}
        </TabbedCard>
      </div>
    </Page>
  );
}
