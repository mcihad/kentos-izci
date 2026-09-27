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
  className,
}: {
  label: string;
  active: string;
  children: ReactNode;
  /** Replaces the page gutter, e.g. inside a card. */
  className?: string;
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
        "scrollbar-none flex gap-1 overflow-x-auto border-b max-sm:fade-end max-sm:pr-8",
        className ?? gutter,
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

/** Classes of one tab: the design system's underline tab, tall enough to tap. */
export function tabClass(active: boolean) {
  return cn(
    "relative inline-flex min-h-11 shrink-0 items-center whitespace-nowrap rounded-t-md px-3.5 text-sm transition-colors duration-(--duration-fast) after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:rounded-t-[1px]",
    active
      ? "font-semibold text-(--tabs-fg-active) after:bg-(--tabs-indicator)"
      : "font-medium text-muted-foreground hover:bg-(--surface-hover) hover:text-foreground",
  );
}

/** Underlined section tabs bound to the `tab` search parameter. */
export function TabNav<T extends string>({
  tabs,
  active,
  from,
  label,
  className,
}: {
  tabs: readonly { id: T; label: ReactNode }[];
  active: T;
  from: string;
  label: string;
  className?: string;
}) {
  return (
    <TabStrip label={label} active={active} className={className}>
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

/**
 * The card a tabbed settings page lives in: section tabs on top, the chosen
 * section below. Runs edge to edge on phones.
 */
export function TabbedCard({ tabs, children }: { tabs: ReactNode; children: ReactNode }) {
  return (
    <div className="card max-w-5xl max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0">
      {tabs}
      <div className="px-4 sm:px-(--card-p)">{children}</div>
    </div>
  );
}
