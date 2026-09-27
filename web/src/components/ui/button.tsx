import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"
import { Slot } from "radix-ui"

// KentOS buttons: semibold, 6px corners, heights from the density scope. Below
// md a control never drops under a comfortable touch target.
const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md border border-transparent font-semibold tracking-[-0.005em] whitespace-nowrap transition-[color,background-color,border-color,box-shadow] duration-(--duration-fast) outline-none focus-visible:shadow-(--focus-shadow) focus-visible:outline-none disabled:pointer-events-none disabled:opacity-55 aria-invalid:border-destructive [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "bg-primary text-primary-foreground hover:bg-(--brand-bg-hover) active:bg-(--brand-bg-active)",
        destructive:
          "bg-(--danger-bg) text-(--danger-fg) hover:bg-(--danger-bg-hover) focus-visible:shadow-(--shadow-focus-danger)",
        outline:
          "border-(--border-default) bg-(--neutral-bg) text-foreground hover:border-(--border-strong) hover:bg-(--surface-hover) active:bg-(--surface-active)",
        secondary:
          "bg-(--surface-muted) text-strong hover:bg-(--surface-active)",
        ghost:
          "text-muted-foreground hover:bg-(--surface-hover) hover:text-foreground active:bg-(--surface-active)",
        link: "h-auto px-0 text-link underline-offset-4 hover:text-link-hover hover:underline",
      },
      size: {
        default:
          "h-[max(var(--control-h-md),2.5rem)] px-(--control-px-md) text-sm md:h-(--control-h-md)",
        xs: "h-6 gap-1 px-2 text-xs [&_svg:not([class*='size-'])]:size-3",
        sm: "h-[max(var(--control-h-sm),2.25rem)] px-(--control-px-sm) text-xs md:h-(--control-h-sm) [&_svg:not([class*='size-'])]:size-3.5",
        lg: "h-[max(var(--control-h-lg),2.75rem)] px-(--control-px-lg) text-md md:h-(--control-h-lg)",
        icon: "size-[max(var(--control-h-md),2.5rem)] md:size-(--control-h-md)",
        "icon-xs": "size-6 [&_svg:not([class*='size-'])]:size-3",
        "icon-sm": "size-[max(var(--control-h-sm),2.25rem)] md:size-(--control-h-sm)",
        "icon-lg": "size-[max(var(--control-h-lg),2.75rem)] md:size-(--control-h-lg)",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot.Root : "button"

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
