import { useQuery } from "@connectrpc/connect-query";
import { SearchIcon, XIcon } from "lucide-react";
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import { IssueService, SuggestionKind } from "@/gen/izci/api/v1/issue_pb";
import { formatCompact } from "@/lib/format";
import { cn } from "@/lib/utils";

export type SearchBarHandle = { focus: () => void };

/**
 * Search input for the issue query language, with completions for keys and
 * values fetched from the server as the user types.
 */
export const SearchBar = forwardRef<
  SearchBarHandle,
  { value: string; onSubmit: (q: string) => void; placeholder?: string; className?: string }
>(function SearchBar({ value, onSubmit, placeholder, className }, ref) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [text, setText] = useState(value);
  const [cursor, setCursor] = useState(value.length);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [debounced, setDebounced] = useState({ text: value, cursor: value.length });

  useImperativeHandle(ref, () => ({ focus: () => inputRef.current?.focus() }));
  useEffect(() => setText(value), [value]);
  useEffect(() => {
    const t = setTimeout(() => setDebounced({ text, cursor }), 120);
    return () => clearTimeout(t);
  }, [text, cursor]);

  const suggest = useQuery(
    IssueService.method.suggestSearch,
    { query: debounced.text, cursor: debounced.cursor },
    { enabled: open, staleTime: 30_000, placeholderData: (prev) => prev },
  );
  const items = open ? (suggest.data?.suggestions ?? []) : [];

  const accept = (i: number) => {
    const s = items[i];
    const res = suggest.data;
    if (!s || !res) return;
    const before = text.slice(0, res.replaceFrom);
    const after = text.slice(res.replaceTo);
    const insert = s.kind === SuggestionKind.KEY ? s.text : `${s.text} `;
    const next = `${before}${insert}${after.trimStart()}`;
    const pos = before.length + insert.length;
    setText(next);
    setCursor(pos);
    setActive(-1);
    requestAnimationFrame(() => inputRef.current?.setSelectionRange(pos, pos));
  };

  return (
    <div className={cn("relative", className)}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setOpen(false);
          onSubmit(text.trim());
        }}
      >
        <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <input
          ref={inputRef}
          value={text}
          spellCheck={false}
          autoComplete="off"
          role="combobox"
          aria-expanded={items.length > 0}
          aria-controls="issue-search-suggestions"
          aria-label="Hatalarda ara"
          placeholder={placeholder ?? 'is:unresolved level:error customer:sivas-belediyesi "timeout"'}
          className="h-9 w-full rounded-md border bg-card pr-8 pl-8 font-mono text-[12.5px] outline-none transition-shadow placeholder:font-sans placeholder:text-muted-foreground/70 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/30"
          onChange={(e) => {
            setText(e.target.value);
            setCursor(e.target.selectionStart ?? e.target.value.length);
            setOpen(true);
            setActive(-1);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 120)}
          onClick={(e) => setCursor(e.currentTarget.selectionStart ?? text.length)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" && items.length) {
              e.preventDefault();
              setActive((a) => (a + 1) % items.length);
            } else if (e.key === "ArrowUp" && items.length) {
              e.preventDefault();
              setActive((a) => (a <= 0 ? items.length - 1 : a - 1));
            } else if ((e.key === "Enter" || e.key === "Tab") && active >= 0) {
              e.preventDefault();
              accept(active);
            } else if (e.key === "Escape") {
              if (open) setOpen(false);
              else inputRef.current?.blur();
            }
          }}
        />
        {text ? (
          <button
            type="button"
            aria-label="Aramayı temizle"
            className="absolute top-1/2 right-2 grid size-5 -translate-y-1/2 place-items-center rounded text-muted-foreground hover:text-foreground"
            onClick={() => {
              setText("");
              onSubmit("");
              inputRef.current?.focus();
            }}
          >
            <XIcon className="size-3.5" />
          </button>
        ) : null}
      </form>
      {items.length > 0 ? (
        <div
          id="issue-search-suggestions"
          role="listbox"
          className="absolute inset-x-0 top-full z-40 mt-1 max-h-80 overflow-y-auto rounded-md border bg-popover p-1 shadow-lg"
        >
          {items.map((s, i) => (
            <div
              key={`${s.kind}:${s.text}`}
              role="option"
              tabIndex={-1}
              aria-selected={i === active}
              onMouseDown={(e) => {
                e.preventDefault();
                accept(i);
              }}
              onMouseEnter={() => setActive(i)}
              className={cn(
                "flex cursor-default items-center gap-3 rounded px-2 py-1.5 text-[13px]",
                i === active && "bg-accent",
              )}
            >
              <span className="font-mono text-[12.5px]">
                {s.kind === SuggestionKind.KEY ? s.text : s.label}
              </span>
              {s.description ? (
                <span className="truncate text-muted-foreground text-xs">{s.description}</span>
              ) : null}
              {s.kind === SuggestionKind.VALUE && s.label !== s.text.split(":").slice(1).join(":") ? (
                <span className="truncate font-mono text-muted-foreground text-xs">{s.text}</span>
              ) : null}
              {s.count > 0n ? (
                <span className="ml-auto text-muted-foreground text-xs tabular">
                  {formatCompact(s.count)}
                </span>
              ) : null}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
});
