import type { ThemedToken } from "shiki/core";
import { useTokens } from "@/lib/highlight";
import { cn } from "@/lib/utils";
import { CopyButton } from "./copy-button";

/** One source line, highlighted when tokens are available. */
export function TokenLine({ tokens, text }: { tokens?: ThemedToken[]; text: string }) {
  if (!tokens) return <>{text || " "}</>;
  if (tokens.length === 0) return <> </>;
  return (
    <>
      {tokens.map((t, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: tokens of a line never reorder
        <span key={i} style={t.color ? { color: t.color } : undefined}>
          {t.content}
        </span>
      ))}
    </>
  );
}

/**
 * Source lines with line numbers; one line can be marked (the line where
 * an error happened) in the given color.
 */
export function CodeLines({
  lines,
  start = 1,
  mark,
  markColor = "var(--level-error)",
  lang,
  className,
}: {
  lines: string[];
  start?: number;
  mark?: number;
  markColor?: string;
  lang?: string;
  className?: string;
}) {
  const tokens = useTokens(lines.join("\n"), lang);
  return (
    <div
      className={cn(
        "overflow-x-auto rounded-md border bg-code py-1.5 font-mono text-sm leading-[1.65]",
        className,
      )}
    >
      <table className="w-full border-collapse">
        <tbody>
          {lines.map((text, i) => {
            const marked = i === mark;
            return (
              <tr
                // biome-ignore lint/suspicious/noArrayIndexKey: line positions are stable
                key={i}
                style={
                  marked ? { background: `color-mix(in oklch, ${markColor} 13%, transparent)` } : undefined
                }
              >
                <td
                  className="w-px select-none whitespace-nowrap border-transparent border-l-2 pr-3 pl-3 text-right text-muted-foreground tabular"
                  style={marked ? { borderLeftColor: markColor, color: markColor } : undefined}
                >
                  {start + i}
                </td>
                <td className="whitespace-pre pr-4">
                  <TokenLine tokens={tokens?.[i]} text={text} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/** A highlighted block of code or data with a copy button. */
export function CodeBlock({
  code,
  lang,
  className,
  copy = true,
  maxHeight = "28rem",
}: {
  code: string;
  lang?: string;
  className?: string;
  copy?: boolean;
  maxHeight?: string;
}) {
  const tokens = useTokens(code, lang);
  const lines = code.split("\n");
  return (
    <div className={cn("group/code relative rounded-md border bg-code", className)}>
      {copy ? (
        <CopyButton
          value={code}
          className="absolute top-1.5 right-1.5 z-10 bg-code opacity-0 group-hover/code:opacity-100 focus-visible:opacity-100 [@media(hover:none)]:opacity-100"
        />
      ) : null}
      <pre className="overflow-auto px-3.5 py-2.5 font-mono text-sm leading-[1.65]" style={{ maxHeight }}>
        {lines.map((text, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: line positions are stable
          <div key={i}>
            <TokenLine tokens={tokens?.[i]} text={text} />
          </div>
        ))}
      </pre>
    </div>
  );
}
