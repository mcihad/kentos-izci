import * as React from "react"
import { cn } from "@/lib/utils"

function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "flex field-sizing-content min-h-20 w-full px-2.5 py-2 text-base md:text-sm rounded-md border border-(--input-border) bg-(--input-bg) text-foreground transition-[color,box-shadow,border-color] duration-(--duration-fast) outline-none placeholder:text-subtle hover:border-(--input-border-hover) focus-visible:border-(--input-border-focus) focus-visible:shadow-(--focus-shadow) focus-visible:outline-none disabled:cursor-not-allowed disabled:bg-(--surface-muted) disabled:opacity-70 aria-invalid:border-(--red-600)",
        className
      )}
      {...props}
    />
  )
}

export { Textarea }
