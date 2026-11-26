import { PageWrapper } from "@kentos/components/layout/page";
import { cn } from "@kentos/components/lib/utils";
import type * as React from "react";
import type { ComponentProps } from "react";
import { useEffect } from "react";

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

/**
 * Horizontal page gutter for content inside a Page (which is unpadded so
 * lists can bleed to the edges on phones); matches the kit's page header.
 */
export const gutter = "px-4 md:px-(--gutter)";

/**
 * An izci page: the kit's PageWrapper (icon, title, tagline, breadcrumb,
 * filters, actions that move into a floating button on phones) plus the
 * browser tab title. The body is unpadded; sections add `gutter` themselves.
 */
export function Page({
  title,
  documentTitle,
  padded = false,
  bodyClassName = "pb-12",
  ...props
}: ComponentProps<typeof PageWrapper> & {
  title: string;
  /** Tab title when it should differ from the heading. */
  documentTitle?: string;
}) {
  useDocumentTitle(documentTitle ?? title);
  return <PageWrapper title={title} padded={padded} bodyClassName={bodyClassName} {...props} />;
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
