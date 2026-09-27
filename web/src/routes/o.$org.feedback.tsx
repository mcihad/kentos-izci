import { useQuery } from "@connectrpc/connect-query";
import { createFileRoute } from "@tanstack/react-router";
import { SearchIcon } from "lucide-react";
import { useState } from "react";
import { gutter, PageHeader } from "@/components/app/page";
import { FeedbackList } from "@/components/feedback/feedback-list";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";

type Search = { project?: string; q?: string };

export const Route = createFileRoute("/o/$org/feedback")({
  validateSearch: (s: Record<string, unknown>): Search => ({
    project: typeof s.project === "string" ? s.project : undefined,
    q: typeof s.q === "string" && s.q ? s.q : undefined,
  }),
  component: FeedbackPage,
});

function FeedbackPage() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const projects = useQuery(ProjectService.method.listProjects, {});
  const [draft, setDraft] = useState(search.q ?? "");
  return (
    <div className="pb-12">
      <PageHeader
        title="Geri bildirim"
        description="Kullanıcıların uygulamalardan yazdıkları"
        actions={
          <Select
            value={search.project ?? "all"}
            onValueChange={(v) =>
              void navigate({ search: (p) => ({ ...p, project: v === "all" ? undefined : v }) })
            }
          >
            <SelectTrigger size="sm" className="w-44 text-sm" aria-label="Proje">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Tüm projeler</SelectItem>
              {projects.data?.projects.map((p) => (
                <SelectItem key={p.slug} value={p.slug}>
                  {p.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
      />
      <div className={gutter}>
        <div className="card max-w-4xl overflow-hidden max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0">
          <form
            className="relative border-b px-4 py-3"
            onSubmit={(e) => {
              e.preventDefault();
              void navigate({ search: (p) => ({ ...p, q: draft.trim() || undefined }) });
            }}
          >
            <SearchIcon className="pointer-events-none absolute top-1/2 left-6.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="Mesajda, adda ya da e-postada ara"
              aria-label="Geri bildirimlerde ara"
              className="h-[max(var(--control-h-md),2.5rem)] w-full rounded-md border border-(--input-border) bg-(--input-bg) text-base outline-none transition-[color,box-shadow,border-color] duration-(--duration-fast) placeholder:text-subtle hover:border-(--input-border-hover) focus-visible:border-(--input-border-focus) focus-visible:shadow-(--focus-shadow) focus-visible:outline-none md:h-(--control-h-md) md:text-sm pr-3 pl-8"
            />
          </form>
          <div className="px-4 sm:px-(--card-p)">
            <FeedbackList project={search.project} query={search.q} />
          </div>
        </div>
      </div>
    </div>
  );
}
