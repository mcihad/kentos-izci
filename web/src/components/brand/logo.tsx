import { cn } from "@/lib/utils";

/**
 * The İzci mark: a compass needle that leaves a fading trail behind it, the
 * way an error leaves a trace through the code.
 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={cn("size-6 shrink-0", className)}>
      <path d="M20 4 16.45 10.95 13.05 7.55Z" className="fill-primary" />
      <path d="M9.5 14.5 13.05 7.55 16.45 10.95Z" className="fill-foreground/35" />
      <circle cx="7.1" cy="16.9" r="1.35" className="fill-primary" opacity="0.8" />
      <circle cx="4.75" cy="19.05" r="1.08" className="fill-primary" opacity="0.55" />
      <circle cx="2.85" cy="21.2" r="0.82" className="fill-primary" opacity="0.32" />
    </svg>
  );
}

/** Mark and name, as shown at the top of the sidebar and on sign-in pages. */
export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2", className)}>
      <Logo />
      <span className="font-semibold text-[17px] tracking-[-0.02em]">İzci</span>
    </span>
  );
}
