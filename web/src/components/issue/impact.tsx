import { Link } from "@tanstack/react-router";
import { EmptyState } from "@/components/common/states";
import type { TagFacet } from "@/gen/izci/api/v1/issue_pb";
import { formatCompact, formatPercent } from "@/lib/format";
import { useOrg } from "@/lib/org";

export const facetLabels: Record<string, string> = {
  customer: "Müşteri",
  tenant: "Kiracı",
  environment: "Ortam",
  release: "Sürüm",
  browser: "Tarayıcı",
  os: "İşletim sistemi",
  device: "Cihaz",
  server_name: "Sunucu",
  level: "Seviye",
  user: "Kullanıcı",
  handled: "İşlenme",
  runtime: "Çalışma ortamı",
  transaction: "İşlem",
  logger: "Logger",
};

// Facet keys map to search keys where the names differ.
const searchKeys: Record<string, string> = { server_name: "server" };

/** How the issue's events spread over customers, releases, browsers and more. */
export function Impact({ facets }: { facets: TagFacet[] | undefined }) {
  const org = useOrg();
  if (!facets) return null;
  if (facets.length === 0)
    return <EmptyState title="Dağılım bilgisi yok">Bu hatanın olaylarında etiket bulunmuyor.</EmptyState>;
  return (
    <div className="grid gap-x-8 gap-y-7 py-5 md:grid-cols-2">
      {facets.map((f) => (
        <section key={f.key} className="min-w-0">
          <div className="mb-2 flex items-baseline justify-between gap-3">
            <h3 className="font-medium text-[13px]">{facetLabels[f.key] ?? f.key}</h3>
            <span className="text-[12px] text-muted-foreground tabular">
              {f.uniqueValues > f.values.length ? `${f.uniqueValues} farklı değer` : null}
            </span>
          </div>
          <ul className="grid gap-1">
            {f.values.map((v) => {
              const share = f.total > 0n ? Number(v.count) / Number(f.total) : 0;
              const key = searchKeys[f.key] ?? f.key;
              return (
                <li key={v.value}>
                  <Link
                    to="/o/$org/issues"
                    params={{ org: org.slug }}
                    search={{
                      query: `is:unresolved ${key}:${/[\s"]/.test(v.value) ? JSON.stringify(v.value) : v.value}`,
                    }}
                    className="group relative flex h-7 items-center gap-3 overflow-hidden rounded px-2 text-[12.5px]"
                  >
                    <span
                      aria-hidden="true"
                      className="absolute inset-y-0 left-0 rounded bg-primary/12 transition-colors group-hover:bg-primary/20"
                      style={{ width: `${Math.max(share * 100, 1.5)}%` }}
                    />
                    <span className="relative min-w-0 flex-1 truncate">{v.label || v.value}</span>
                    <span className="relative text-muted-foreground tabular">{formatPercent(share)}</span>
                    <span className="relative w-12 text-right tabular">{formatCompact(v.count)}</span>
                  </Link>
                </li>
              );
            })}
          </ul>
        </section>
      ))}
    </div>
  );
}
