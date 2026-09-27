import { useQuery } from "@tanstack/react-query";

/** A guide of the embedded documentation (served by the izci server). */
export type Guide = { slug: string; title: string; description: string; section: string };

export function useGuides() {
  return useQuery({
    queryKey: ["docs", "index"],
    queryFn: async (): Promise<Guide[]> => {
      const res = await fetch("/docs/index.json");
      if (!res.ok) throw new Error(`docs index: ${res.status}`);
      return res.json();
    },
    staleTime: Number.POSITIVE_INFINITY,
  });
}

export function useGuide(slug: string) {
  return useQuery({
    queryKey: ["docs", "guide", slug],
    queryFn: async (): Promise<string> => {
      const res = await fetch(`/docs/raw/${slug}`);
      if (!res.ok) throw new Error(`guide ${slug}: ${res.status}`);
      return res.text();
    },
    staleTime: Number.POSITIVE_INFINITY,
    enabled: slug !== "",
  });
}

/** Groups guides by section, keeping their order. */
export function sections(guides: Guide[]): { name: string; guides: Guide[] }[] {
  const out: { name: string; guides: Guide[] }[] = [];
  for (const g of guides) {
    const last = out.at(-1);
    if (last?.name === g.section) last.guides.push(g);
    else out.push({ name: g.section, guides: [g] });
  }
  return out;
}

/**
 * Resolves a link inside guide `from` to an in-app path: other guides open
 * here, repository files (examples) are shown as text, the rest opens outside.
 */
export function resolveGuideLink(
  org: string,
  from: string,
  href: string,
  known: Set<string>,
): string | null | undefined {
  if (/^[a-z]+:/i.test(href) || href.startsWith("#")) return undefined;
  const [file = "", hash = ""] = href.split("#");
  const base = from.includes("/") ? from.slice(0, from.lastIndexOf("/") + 1) : "";
  const parts: string[] = [];
  for (const p of `${base}${file}`.split("/")) {
    if (p === "..") parts.pop();
    else if (p && p !== ".") parts.push(p);
  }
  const slug = file === "" ? from : parts.join("/").replace(/\.md$/, "");
  if (!known.has(slug)) return null;
  return `/o/${org}/docs/${slug}${hash ? `#${hash}` : ""}`;
}
