import type { LucideIcon } from "lucide-react";
import { useId } from "react";
import { cn } from "@/lib/utils";

export type SegmentedOption<T extends string> = { value: T; label: string; icon?: LucideIcon };

/**
 * A row of mutually exclusive choices (the design system's pill tabs). Built
 * on native radio buttons, so arrow keys move the choice and Tab leaves it.
 */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
  className,
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly SegmentedOption<T>[];
  label: string;
  className?: string;
}) {
  const name = useId();
  return (
    <fieldset
      className={cn("flex min-w-0 gap-0.5 rounded-md border bg-(--surface-muted) p-[3px]", className)}
    >
      <legend className="sr-only">{label}</legend>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <label
            key={o.value}
            className={cn(
              "inline-flex h-9 min-w-0 flex-1 cursor-pointer items-center justify-center gap-1.5 rounded-[calc(var(--radius-md)-3px)] px-1.5 text-xs transition-colors duration-(--duration-fast) has-[:focus-visible]:shadow-(--focus-shadow) md:h-8",
              on
                ? "bg-primary font-semibold text-primary-foreground"
                : "font-medium text-muted-foreground hover:text-foreground",
            )}
          >
            <input
              type="radio"
              name={name}
              value={o.value}
              checked={on}
              onChange={() => onChange(o.value)}
              className="sr-only"
            />
            {o.icon ? <o.icon className="size-3.5 shrink-0" /> : null}
            <span className="truncate">{o.label}</span>
          </label>
        );
      })}
    </fieldset>
  );
}
