import type * as React from "react";
import { cn } from "@/lib/utils";

/** A keyboard key hint. */
export function Kbd({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex h-5 min-w-5 items-center justify-center rounded border bg-muted px-1 font-medium font-sans text-[11px] text-muted-foreground",
        className,
      )}
    >
      {children}
    </kbd>
  );
}
