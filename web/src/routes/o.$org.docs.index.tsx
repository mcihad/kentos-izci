import { createFileRoute, Link } from "@tanstack/react-router";
import { ErrorState } from "@/components/common/states";
import { sections, useGuides } from "@/components/docs/docs";
import { Skeleton } from "@/components/ui/skeleton";
import { useOrg } from "@/lib/org";

export const Route = createFileRoute("/o/$org/docs/")({
  component: DocsIndex,
});

function DocsIndex() {
  const org = useOrg();
  const guides = useGuides();
  if (guides.isPending) return <Skeleton className="h-64 w-full max-w-3xl" />;
  if (guides.error) return <ErrorState error={guides.error} onRetry={() => void guides.refetch()} />;
  return (
    <div className="grid max-w-3xl gap-8">
      <p className="text-[14.5px] text-muted-foreground leading-7">
        Bu rehberler sunucunun sürümüyle birlikte gelir. Yapay zekâ araçları aynı içeriği{" "}
        <a href="/llms.txt" className="font-mono text-[13px] text-primary hover:underline">
          /llms.txt
        </a>{" "}
        ve{" "}
        <a href="/llms-full.txt" className="font-mono text-[13px] text-primary hover:underline">
          /llms-full.txt
        </a>{" "}
        adreslerinden okuyabilir.
      </p>
      {sections(guides.data).map((s) => (
        <section key={s.name} aria-label={s.name}>
          <h2 className="mb-3 font-semibold text-[15px]">{s.name}</h2>
          <ul className="grid gap-3 sm:grid-cols-2">
            {s.guides.map((g) => (
              <li key={g.slug}>
                <Link
                  to="/o/$org/docs/$"
                  params={{ org: org.slug, _splat: g.slug }}
                  className="block h-full rounded-lg border px-4 py-3 transition-colors hover:bg-accent/40"
                >
                  <p className="font-medium text-[14px]">{g.title}</p>
                  <p className="mt-1 text-[12.5px] text-muted-foreground leading-relaxed">{g.description}</p>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
