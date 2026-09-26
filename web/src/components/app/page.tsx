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

/** Horizontal page gutter shared by headers and bodies. */
export const gutter = "px-4 sm:px-6 lg:px-8";

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
    <header className={cn("border-b", className)}>
      <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-3 pt-5 pb-4", gutter)}>
        <div className="min-w-0 flex-1">
          <h1 className="truncate font-semibold text-[20px] leading-tight tracking-[-0.015em]">{title}</h1>
          {description ? <div className="mt-1 text-[13px] text-muted-foreground">{description}</div> : null}
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
      {children}
    </header>
  );
}

/** A titled region inside a page. */
export function Section({
  title,
  description,
  actions,
  children,
  className,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("min-w-0", className)}>
      <div className="mb-3 flex items-end justify-between gap-3">
        <div className="min-w-0">
          <h2 className="font-semibold text-[14px] tracking-tight">{title}</h2>
          {description ? <p className="mt-0.5 text-[12.5px] text-muted-foreground">{description}</p> : null}
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}
