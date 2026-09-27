import * as React from "react"
import { cn } from "@/lib/utils"

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "h-[max(var(--control-h-md),2.5rem)] w-full min-w-0 px-2.5 py-1 text-base file:inline-flex file:h-7 file:border-0 file:bg-transparent file:font-semibold file:text-foreground file:text-sm md:h-(--control-h-md) md:text-sm",
        "rounded-md border border-(--input-border) bg-(--input-bg) text-foreground transition-[color,box-shadow,border-color] duration-(--duration-fast) outline-none placeholder:text-subtle hover:border-(--input-border-hover) focus-visible:border-(--input-border-focus) focus-visible:shadow-(--focus-shadow) focus-visible:outline-none disabled:cursor-not-allowed disabled:bg-(--surface-muted) disabled:opacity-70 aria-invalid:border-(--red-600)",
        className
      )}
      {...props}
    />
  )
}

export { Input }
