import type { LucideIcon } from "lucide-react";
import type * as React from "react";
import { Button } from "@/components/ui/button";
import { errorMessage } from "@/lib/errors";
import { cn } from "@/lib/utils";

/** Shown when a list has nothing to show; says what to do next. */
export function EmptyState({
  icon: Icon,
  title,
  children,
  action,
  className,
}: {
  icon?: LucideIcon;
  title: string;
  children?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center gap-3 px-6 py-14 text-center", className)}>
      {Icon ? <Icon className="size-6 text-muted-foreground/70" strokeWidth={1.5} /> : null}
      <div className="max-w-sm space-y-1">
        <p className="font-medium text-md">{title}</p>
        {children ? <div className="text-sm text-muted-foreground leading-relaxed">{children}</div> : null}
      </div>
      {action}
    </div>
  );
}

/** Shown when loading failed. */
export function ErrorState({
  error,
  onRetry,
  className,
}: {
  error: unknown;
  onRetry?: () => void;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center gap-3 px-6 py-12 text-center", className)}>
      <p className="max-w-sm text-sm text-muted-foreground">{errorMessage(error)}</p>
      {onRetry ? (
        <Button variant="outline" size="sm" onClick={onRetry}>
          Yeniden dene
        </Button>
      ) : null}
    </div>
  );
}
