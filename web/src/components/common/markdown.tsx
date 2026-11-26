import { cn } from "@kentos/components/lib/utils";
import { useRouter } from "@tanstack/react-router";
import { Children, isValidElement, type ReactNode, useMemo } from "react";
import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { CodeLines } from "@/components/common/code-block";
import { localImage } from "@/lib/images";

const languageAlias: Record<string, string> = {
  cs: "csharp",
  "c#": "csharp",
  js: "javascript",
  ts: "typescript",
  py: "python",
  sh: "shellscript",
  bash: "shellscript",
  shell: "shellscript",
  yml: "yaml",
  golang: "go",
  ini: "shellscript",
  nginx: "shellscript",
};

function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children);
  return "";
}

/** GitHub's heading id: lower case, punctuation removed, spaces → "-". */
export function headingId(children: ReactNode): string {
  return textOf(Children.toArray(children))
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{Mn}\s_-]/gu, "")
    .replace(/\s/g, "-");
}

/**
 * How a link in a document is followed: an in-app path, null for links that
 * cannot be opened here (rendered as text), undefined for external links.
 */
export type LinkResolver = (href: string) => string | null | undefined;

type Variant = "report" | "doc";

const headings: Record<Variant, Record<"h1" | "h2" | "h3" | "h4", string>> = {
  report: {
    h1: "mt-6 mb-2 font-semibold text-lg first:mt-0",
    h2: "mt-6 mb-2 font-semibold text-lg first:mt-0",
    h3: "mt-5 mb-1.5 font-semibold text-md first:mt-0",
    h4: "mt-4 mb-1 font-medium text-md first:mt-0",
  },
  doc: {
    h1: "mb-5 font-semibold text-2xl leading-tight tracking-[-0.02em]",
    h2: "mt-11 mb-3 scroll-mt-20 border-b pb-2 font-semibold text-xl tracking-[-0.01em] first:mt-0",
    h3: "mt-8 mb-2 scroll-mt-20 font-semibold text-lg",
    h4: "mt-6 mb-1.5 scroll-mt-20 font-medium text-md",
  },
};

function components(
  variant: Variant,
  resolve: LinkResolver | undefined,
  go: (path: string) => void,
): Components {
  const doc = variant === "doc";
  const heading =
    (level: "h1" | "h2" | "h3" | "h4", Tag: "h2" | "h3" | "h4" | "h1") =>
    ({ children }: { children?: ReactNode }) => {
      // Reports nest under the page's own headings; documents keep their
      // hierarchy and get ids for links.
      const T = doc ? level : Tag;
      return (
        <T id={doc ? headingId(children) : undefined} className={headings[variant][level]}>
          {children}
        </T>
      );
    };
  return {
    h1: heading("h1", "h3"),
    h2: heading("h2", "h3"),
    h3: heading("h3", "h4"),
    h4: heading("h4", "h4"),
    p: ({ children }) => <p className={cn("my-2.5", doc ? "leading-7" : "leading-relaxed")}>{children}</p>,
    ul: ({ children }) => (
      <ul className="my-2.5 list-disc space-y-1 pl-5 marker:text-muted-foreground">{children}</ul>
    ),
    ol: ({ children }) => (
      <ol className="my-2.5 list-decimal space-y-1 pl-5 marker:text-muted-foreground">{children}</ol>
    ),
    li: ({ children }) => <li className={cn("pl-0.5", doc ? "leading-7" : "leading-relaxed")}>{children}</li>,
    a: ({ href = "", children }) => {
      const target = resolve?.(href);
      if (target === null) return <span className="text-foreground">{children}</span>;
      if (target !== undefined) {
        return (
          <a
            href={target}
            onClick={(e) => {
              if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
              e.preventDefault();
              go(target);
            }}
            className="text-link underline-offset-2 hover:underline"
          >
            {children}
          </a>
        );
      }
      const local = href.startsWith("#");
      return (
        <a
          href={href}
          target={local ? undefined : "_blank"}
          rel={local ? undefined : "noreferrer noopener"}
          className="text-link underline-offset-2 hover:underline"
        >
          {children}
        </a>
      );
    },
    blockquote: ({ children }) => (
      <blockquote className="my-3 border-l-2 pl-3 text-muted-foreground">{children}</blockquote>
    ),
    table: ({ children }) => (
      <div className="my-4 overflow-x-auto rounded-md border">
        <table className="w-full border-collapse text-sm">{children}</table>
      </div>
    ),
    th: ({ children, style }) => (
      <th
        style={style}
        className="border-b bg-muted/50 px-3 py-2 text-left font-medium [overflow-wrap:normal]"
      >
        {children}
      </th>
    ),
    td: ({ children, style }) => (
      <td
        style={style}
        className="border-b px-3 py-2 align-top leading-relaxed [overflow-wrap:normal] last:border-b-0"
      >
        {children}
      </td>
    ),
    hr: () => <hr className="my-6" />,
    // Images from other sites become links: loading one would tell that
    // site who opened the report.
    img: ({ src, alt }) => {
      const local = localImage(typeof src === "string" ? src : undefined, window.location.origin);
      if (local)
        return (
          <img src={local} alt={alt ?? ""} loading="lazy" className="my-3 max-w-full rounded-md border" />
        );
      if (typeof src !== "string" || src === "") return null;
      return (
        <a
          href={src}
          target="_blank"
          rel="noreferrer noopener"
          className="text-link underline-offset-2 hover:underline"
        >
          {alt ? `Görsel: ${alt}` : "Görsel"}
        </a>
      );
    },
    input: ({ checked }) => (
      <input type="checkbox" checked={checked} readOnly className="mr-1.5 translate-y-px accent-primary" />
    ),
    // Fenced blocks are rendered by `pre`; `code` only sees inline code.
    code: ({ children }) => (
      <code className="rounded bg-muted px-1 py-px font-mono text-[0.88em] [overflow-wrap:anywhere]">
        {children}
      </code>
    ),
    pre: ({ node }) => {
      const code = node?.children[0];
      const lang =
        code && "properties" in code
          ? String((code.properties.className as string[] | undefined)?.[0] ?? "").replace("language-", "")
          : "";
      const text =
        code && "children" in code
          ? code.children.map((c) => ("value" in c ? String(c.value) : "")).join("")
          : "";
      return (
        <CodeLines
          className="my-4"
          lines={text.replace(/\n$/, "").split("\n")}
          lang={languageAlias[lang] ?? lang}
        />
      );
    },
  };
}

/**
 * Renders Markdown: AI analysis reports ("report") and the embedded guides
 * ("doc"). Raw HTML is not rendered (react-markdown escapes it), so a report
 * cannot inject markup into the page, and only images of izci itself are
 * loaded.
 */
export default function Markdown({
  text,
  variant = "report",
  resolveLink,
}: {
  text: string;
  variant?: Variant;
  resolveLink?: LinkResolver;
}) {
  const router = useRouter();
  const comps = useMemo(
    () => components(variant, resolveLink, (path) => router.history.push(path)),
    [variant, resolveLink, router],
  );
  return (
    <div className={cn("[overflow-wrap:anywhere]", variant === "doc" ? "text-md" : "text-md")}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={comps}>
        {text}
      </ReactMarkdown>
    </div>
  );
}
