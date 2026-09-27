import { Link } from "@tanstack/react-router";
import { type ReactNode, useLayoutEffect, useRef } from "react";
import { gutter } from "@/components/app/page";
import { cn } from "@/lib/utils";

/**
 * An underlined tab strip. On phones it scrolls sideways without a
 * scrollbar and fades at its end to show that more tabs follow; the current
 * tab (the one marked aria-current) is kept scrolled into view.
 */
export function TabStrip({
  label,
  active,
  children,
}: {
  label: string;
  active: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLElement>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-run when the current tab changes
  useLayoutEffect(() => {
    const strip = ref.current;
    const current = strip?.querySelector<HTMLElement>('[aria-current="page"]');
    if (!strip || !current || strip.scrollWidth <= strip.clientWidth) return;
    const s = strip.getBoundingClientRect();
    const r = current.getBoundingClientRect();
    // Clear of the faded end (2rem) as well as the visible edge.
    if (r.right > s.right - 32) strip.scrollLeft += r.right - s.right + 48;
    else if (r.left < s.left) strip.scrollLeft -= s.left - r.left + 16;
  }, [active]);
  return (
    <nav
      ref={ref}
      aria-label={label}
      className={cn(
        "scrollbar-none -mb-px flex gap-6 overflow-x-auto text-md max-sm:fade-end max-sm:pr-8",
        gutter,
      )}
    >
      {children}
    </nav>
  );
}

/**
 * Router matching for tab links: exact, so the first tab (which has no
 * search parameter) is not also marked current when another tab is.
 */
export const tabActiveOptions = { exact: true, includeSearch: true } as const;

/** Classes of one tab; tall enough to tap on phones. */
export function tabClass(active: boolean) {
  return cn(
    "inline-flex min-h-11 items-center whitespace-nowrap border-b-2 transition-colors sm:min-h-10",
    active
      ? "border-primary font-semibold text-foreground"
      : "border-transparent text-muted-foreground hover:text-foreground",
  );
}

/** Underlined section tabs bound to the `tab` search parameter. */
export function TabNav<T extends string>({
  tabs,
  active,
  from,
  label,
}: {
  tabs: readonly { id: T; label: ReactNode }[];
  active: T;
  from: string;
  label: string;
}) {
  return (
    <TabStrip label={label} active={active}>
      {tabs.map((t, i) => (
        <Link
          key={t.id}
          from={from as never}
          search={{ tab: i === 0 ? undefined : t.id } as never}
          activeOptions={tabActiveOptions}
          className={tabClass(active === t.id)}
          aria-current={active === t.id ? "page" : undefined}
        >
          {t.label}
        </Link>
      ))}
    </TabStrip>
  );
}
