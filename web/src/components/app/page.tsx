import type * as React from "react";
import { useEffect } from "react";
import { cn } from "@/lib/utils";

/** Sets the browser tab title while the page is shown. */
export function useDocumentTitle(title: string | undefined) {
  useEffect(() => {
    if (!title) return;
    document.title = `${title} — İzci`;
    return () => {
      document.title = "İzci";
    };
  }, [title]);
}

/** Horizontal page gutter shared by headers and bodies; follows the density scope. */
export const gutter = "px-4 sm:px-(--gutter)";

export function PageHeader({
  title,
  description,
  actions,
  children,
  className,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
}) {
  useDocumentTitle(typeof title === "string" ? title : undefined);
  return (
    <header className={className}>
      <div className={cn("flex flex-wrap items-end gap-x-6 gap-y-3 pt-5 pb-4 sm:pt-6 sm:pb-5", gutter)}>
        <div className="min-w-0 flex-[1_1_16rem]">
          <h1 className="font-semibold text-xl leading-tight sm:text-2xl">{title}</h1>
          {description ? <div className="mt-1 text-md text-muted-foreground">{description}</div> : null}
        </div>
        {actions ? (
          <div className="scrollbar-none flex items-center gap-2 *:shrink-0 max-sm:-mx-4 max-sm:w-[calc(100%+2rem)] max-sm:overflow-x-auto max-sm:px-4 sm:flex-wrap">
            {actions}
          </div>
        ) : null}
      </div>
      {children}
    </header>
  );
}

/**
 * A titled card: the design system's Card. With `bleed` a list or table runs
 * edge to edge under the header instead of sitting inside the padding.
 */
export function Section({
  title,
  description,
  actions,
  children,
  className,
  bleed = false,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  bleed?: boolean;
}) {
  return (
    <section className={cn("card min-w-0 overflow-hidden", className)}>
      <div
        className={cn(
          "flex flex-wrap items-start justify-between gap-x-4 gap-y-2 px-4 pt-4 pb-3.5 sm:px-(--card-p) sm:pt-(--card-p)",
          bleed && "border-b",
        )}
      >
        <div className="min-w-0">
          <h2 className="font-semibold text-md leading-snug">{title}</h2>
          {description ? <p className="mt-0.5 text-muted-foreground text-sm">{description}</p> : null}
        </div>
        {actions}
      </div>
      <div className={bleed ? undefined : "px-4 pb-4 sm:px-(--card-p) sm:pb-(--card-p)"}>{children}</div>
    </section>
  );
}
