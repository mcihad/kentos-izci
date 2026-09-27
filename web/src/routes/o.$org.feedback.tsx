import { useQuery } from "@connectrpc/connect-query";
import { createFileRoute } from "@tanstack/react-router";
import { SearchIcon } from "lucide-react";
import { useState } from "react";
import { gutter, PageHeader } from "@/components/app/page";
import { FeedbackList } from "@/components/feedback/feedback-list";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { cn } from "@/lib/utils";

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
      <div className={cn("max-w-4xl pt-4", gutter)}>
        <form
          className="relative"
          onSubmit={(e) => {
            e.preventDefault();
            void navigate({ search: (p) => ({ ...p, q: draft.trim() || undefined }) });
          }}
        >
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="Mesajda, adda ya da e-postada ara"
            aria-label="Geri bildirimlerde ara"
            className="h-10 w-full rounded-md border bg-card pr-3 pl-8 text-base outline-none md:h-9 md:text-sm placeholder:text-muted-foreground/70 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/30"
          />
        </form>
        <FeedbackList project={search.project} query={search.q} />
      </div>
    </div>
  );
}
