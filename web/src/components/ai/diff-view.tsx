import { ChevronRightIcon, FileMinusIcon, FilePenLineIcon, FilePlusIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { TokenLine } from "@/components/common/code-block";
import { CopyButton } from "@/components/common/copy-button";
import { type DiffFile, parseDiff } from "@/lib/diff";
import { languageOf, useTokens } from "@/lib/highlight";
import { cn } from "@/lib/utils";

const statusIcon = { added: FilePlusIcon, deleted: FileMinusIcon, modified: FilePenLineIcon };
const statusLabel = { added: "yeni dosya", deleted: "silindi", modified: "değişti" };

/** The proposed change of a run, file by file. */
export function DiffView({ diff }: { diff: string }) {
  const files = useMemo(() => parseDiff(diff), [diff]);
  return (
    <div className="grid grid-cols-1 gap-3">
      {files.map((f) => (
        <DiffFileView key={f.path} file={f} />
      ))}
    </div>
  );
}

function DiffFileView({ file }: { file: DiffFile }) {
  const [open, setOpen] = useState(file.additions + file.deletions <= 400);
  const Icon = statusIcon[file.status];
  const lang = languageOf(file.path);
  // Each side is highlighted as one text so that multi-line constructs
  // (comments, strings) keep their colors.
  const { newText, oldText } = useMemo(() => {
    const n: string[] = [];
    const o: string[] = [];
    for (const h of file.hunks) {
      for (const l of h.lines) {
        if (l.kind !== "del") n.push(l.text);
        if (l.kind !== "add") o.push(l.text);
      }
    }
    return { newText: n.join("\n"), oldText: o.join("\n") };
  }, [file]);
  const newTokens = useTokens(open ? newText : "", lang);
  const oldTokens = useTokens(open && file.deletions > 0 ? oldText : "", lang);

  let ni = 0;
  let oi = 0;
  return (
    <section className="min-w-0 overflow-hidden card" aria-label={file.path}>
      <header className="flex items-center gap-2 bg-muted/40 px-3 py-2">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
        >
          <ChevronRightIcon
            className={cn(
              "size-3.5 shrink-0 text-muted-foreground transition-transform",
              open && "rotate-90",
            )}
          />
          <Icon className="size-4 shrink-0 text-muted-foreground" aria-label={statusLabel[file.status]} />
          <span className="truncate font-mono text-sm">{file.path}</span>
        </button>
        <span className="tabular shrink-0 font-mono text-xs">
          <span className="text-health-good-ink">+{file.additions}</span>{" "}
          <span className="text-level-error-ink">−{file.deletions}</span>
        </span>
        <CopyButton value={file.path} label="Yolu kopyala" />
      </header>
      {open ? (
        file.binary ? (
          <p className="border-t px-4 py-3 text-sm text-muted-foreground">
            İkili dosya; içerik gösterilmiyor.
          </p>
        ) : (
          <div className="overflow-x-auto border-t bg-code font-mono text-sm leading-[1.6]">
            <table className="w-full border-collapse">
              <tbody>
                {file.hunks.map((h) => (
                  <HunkRows key={h.id} header={h.header}>
                    {h.lines.map((l, li) => {
                      const tokens = l.kind === "del" ? oldTokens?.[oi] : newTokens?.[ni];
                      if (l.kind !== "del") ni++;
                      if (l.kind !== "add") oi++;
                      return (
                        <tr
                          // biome-ignore lint/suspicious/noArrayIndexKey: diff lines never reorder
                          key={li}
                          className={cn(
                            l.kind === "add" && "bg-[color-mix(in_oklch,var(--health-good)_12%,transparent)]",
                            l.kind === "del" && "bg-[color-mix(in_oklch,var(--level-error)_11%,transparent)]",
                          )}
                        >
                          <td className="tabular w-[1%] select-none whitespace-nowrap px-2 text-right text-muted-foreground/70">
                            {l.oldNo ?? ""}
                          </td>
                          <td className="tabular w-[1%] select-none whitespace-nowrap px-2 text-right text-muted-foreground/70">
                            {l.newNo ?? ""}
                          </td>
                          <td
                            className={cn(
                              "w-[1%] select-none px-1 text-center",
                              l.kind === "add" && "text-health-good-ink",
                              l.kind === "del" && "text-level-error-ink",
                            )}
                            aria-hidden="true"
                          >
                            {l.kind === "add" ? "+" : l.kind === "del" ? "−" : ""}
                          </td>
                          <td className="whitespace-pre pr-4">
                            <span className="sr-only">
                              {l.kind === "add" ? "eklendi: " : l.kind === "del" ? "silindi: " : ""}
                            </span>
                            <TokenLine tokens={tokens} text={l.text} />
                          </td>
                        </tr>
                      );
                    })}
                  </HunkRows>
                ))}
              </tbody>
            </table>
          </div>
        )
      ) : null}
    </section>
  );
}

function HunkRows({ header, children }: { header: string; children: React.ReactNode }) {
  return (
    <>
      <tr className="bg-muted/60 text-muted-foreground">
        <td colSpan={3} className="select-none px-2 text-center">
          ⋯
        </td>
        <td className="truncate px-0 py-0.5 text-xs">{header}</td>
      </tr>
      {children}
    </>
  );
}
