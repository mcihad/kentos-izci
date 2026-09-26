import type * as React from "react";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/** A labelled form control with an optional hint or error below it. */
export function Field({
  label,
  htmlFor,
  error,
  hint,
  children,
  className,
}: {
  label: React.ReactNode;
  htmlFor?: string;
  error?: string;
  hint?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("grid gap-1.5", className)}>
      <Label htmlFor={htmlFor} className="font-medium text-[13px]">
        {label}
      </Label>
      {children}
      {error ? (
        <p role="alert" className="text-destructive text-xs">
          {error}
        </p>
      ) : hint ? (
        <p className="text-muted-foreground text-xs leading-relaxed">{hint}</p>
      ) : null}
    </div>
  );
}

/** A form-level error block. */
export function FormError({ message }: { message?: string }) {
  if (!message) return null;
  return (
    <p
      role="alert"
      className="rounded-md border border-destructive/30 bg-destructive/8 px-3 py-2 text-[13px] text-destructive"
    >
      {message}
    </p>
  );
}
