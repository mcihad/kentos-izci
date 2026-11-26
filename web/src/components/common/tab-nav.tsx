import { TabsNav, TabsNavItem } from "@kentos/components/ui/tabs";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { gutter } from "@/components/app/page";

/**
 * Underlined section tabs from the kit (TabsNav): on phones the row scrolls
 * with touch, with the wheel on desktop, fades at its edges and keeps the
 * current tab in view; the indicator slides to the current tab. Children are
 * TabsNavItem elements wrapping router links.
 */
export function TabStrip({
  label,
  children,
  className,
}: {
  label: string;
  children: ReactNode;
  /** Replaces the page gutter, e.g. inside a card. */
  className?: string;
}) {
  return (
    <TabsNav label={label} className={className ?? gutter}>
      {children}
    </TabsNav>
  );
}

/**
 * Router matching for tab links: exact, so the first tab (which has no
 * search parameter) is not also marked current when another tab is.
 */
export const tabActiveOptions = { exact: true, includeSearch: true } as const;

/** Underlined section tabs bound to the `tab` search parameter. */
export function TabNav<T extends string>({
  tabs,
  active,
  from,
  label,
  className,
}: {
  tabs: readonly { id: T; label: ReactNode; badge?: ReactNode }[];
  active: T;
  from: string;
  label: string;
  className?: string;
}) {
  return (
    <TabStrip label={label} className={className}>
      {tabs.map((t, i) => (
        <TabsNavItem key={t.id} asChild active={active === t.id} label={t.label} badge={t.badge}>
          <Link
            from={from as never}
            search={{ tab: i === 0 ? undefined : t.id } as never}
            activeOptions={tabActiveOptions}
          />
        </TabsNavItem>
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
    <div className="card max-sm:-mx-4 max-sm:rounded-none max-sm:border-x-0">
      {tabs}
      <div className="px-4 sm:px-(--card-p)">{children}</div>
    </div>
  );
}
