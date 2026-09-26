import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { CodeLines } from "@/components/common/code-block";

const components: Components = {
  h1: ({ children }) => <h3 className="mt-6 mb-2 font-semibold text-[15px] first:mt-0">{children}</h3>,
  h2: ({ children }) => <h3 className="mt-6 mb-2 font-semibold text-[15px] first:mt-0">{children}</h3>,
  h3: ({ children }) => <h4 className="mt-5 mb-1.5 font-semibold text-[14px] first:mt-0">{children}</h4>,
  h4: ({ children }) => <h4 className="mt-4 mb-1 font-medium text-[13.5px] first:mt-0">{children}</h4>,
  p: ({ children }) => <p className="my-2.5 leading-relaxed">{children}</p>,
  ul: ({ children }) => (
    <ul className="my-2.5 list-disc space-y-1 pl-5 marker:text-muted-foreground">{children}</ul>
  ),
  ol: ({ children }) => (
    <ol className="my-2.5 list-decimal space-y-1 pl-5 marker:text-muted-foreground">{children}</ol>
  ),
  li: ({ children }) => <li className="pl-0.5 leading-relaxed">{children}</li>,
  a: ({ href, children }) => (
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      className="text-primary underline-offset-2 hover:underline"
    >
      {children}
    </a>
  ),
  blockquote: ({ children }) => (
    <blockquote className="my-3 border-l-2 pl-3 text-muted-foreground">{children}</blockquote>
  ),
  table: ({ children }) => (
    <div className="my-3 overflow-x-auto rounded-md border">
      <table className="w-full border-collapse text-[12.5px]">{children}</table>
    </div>
  ),
  th: ({ children }) => (
    <th className="border-b bg-muted/50 px-3 py-1.5 text-left font-medium">{children}</th>
  ),
  td: ({ children }) => <td className="border-b px-3 py-1.5 align-top last:border-b-0">{children}</td>,
  hr: () => <hr className="my-5" />,
  // Fenced blocks are rendered by `pre`; `code` only sees inline code.
  code: ({ children }) => (
    <code className="rounded bg-muted px-1 py-px font-mono text-[0.9em] [overflow-wrap:anywhere]">
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
        className="my-3"
        lines={text.replace(/\n$/, "").split("\n")}
        lang={languageAlias[lang] ?? lang}
      />
    );
  },
};

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
};

/**
 * Markdown written by the AI agent. Raw HTML is not rendered (react-markdown
 * escapes it), so an analysis cannot inject markup into the page.
 */
export default function Markdown({ text }: { text: string }) {
  return (
    <div className="text-[13.5px] [overflow-wrap:anywhere]">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {text}
      </ReactMarkdown>
    </div>
  );
}
