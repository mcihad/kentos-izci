import { Skeleton } from "@kentos/components/ui/skeleton";
import { createFileRoute } from "@tanstack/react-router";
import { BookOpenIcon } from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo } from "react";
import { useDocumentTitle } from "@/components/app/page";
import { EmptyState, ErrorState } from "@/components/common/states";
import { resolveGuideLink, useGuide, useGuides } from "@/components/docs/docs";
import { useOrg } from "@/lib/org";

const Markdown = lazy(() => import("@/components/common/markdown"));

export const Route = createFileRoute("/o/$org/docs/$")({
  component: GuidePage,
});

function GuidePage() {
  const org = useOrg();
  const slug = Route.useParams()._splat ?? "";
  const guides = useGuides();
  const guide = useGuide(slug);
  const known = useMemo(() => new Set((guides.data ?? []).map((g) => g.slug)), [guides.data]);
  const meta = guides.data?.find((g) => g.slug === slug);
  useDocumentTitle(meta?.title);
  const resolve = useCallback(
    (href: string) => resolveGuideLink(org.slug, slug, href, known),
    [org.slug, slug, known],
  );

  // Jump to the heading named in the address once the guide is shown.
  useEffect(() => {
    if (!guide.data) return;
    const id = decodeURIComponent(window.location.hash.slice(1));
    if (id) requestAnimationFrame(() => document.getElementById(id)?.scrollIntoView());
    else window.scrollTo({ top: 0 });
  }, [guide.data]);

  if (guides.data && !meta) {
    return (
      <EmptyState icon={BookOpenIcon} title="Rehber bulunamadı">
        Soldaki listeden bir rehber seçin.
      </EmptyState>
    );
  }
  if (guide.error) return <ErrorState error={guide.error} onRetry={() => void guide.refetch()} />;
  const loading = (
    <div className="grid max-w-[72ch] gap-3">
      <Skeleton className="h-8 w-2/3" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-5/6" />
      <Skeleton className="h-48 w-full" />
    </div>
  );
  if (!guide.data) return loading;
  return (
    <article className="max-w-[72ch]">
      <Suspense fallback={loading}>
        <Markdown text={guide.data} variant="doc" resolveLink={resolve} />
      </Suspense>
    </article>
  );
}
