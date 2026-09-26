import { Link } from "@tanstack/react-router";
import { gutter } from "@/components/app/page";
import { cn } from "@/lib/utils";

/** Underlined section tabs bound to the `tab` search parameter. */
export function TabNav<T extends string>({
  tabs,
  active,
  from,
  label,
}: {
  tabs: readonly { id: T; label: string }[];
  active: T;
  from: string;
  label: string;
}) {
  return (
    <nav className={cn("-mb-px flex gap-5 overflow-x-auto text-[13px]", gutter)} aria-label={label}>
      {tabs.map((t, i) => (
        <Link
          key={t.id}
          from={from as never}
          search={{ tab: i === 0 ? undefined : t.id } as never}
          className={cn(
            "whitespace-nowrap border-b-2 pb-2.5 transition-colors",
            active === t.id
              ? "border-primary font-medium text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground",
          )}
        >
          {t.label}
        </Link>
      ))}
    </nav>
  );
}
