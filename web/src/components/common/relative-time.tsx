import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { useEffect, useState } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDateTime, formatRelative, toDate } from "@/lib/format";
import { cn } from "@/lib/utils";

// One shared clock for every relative time on screen.
const listeners = new Set<(now: number) => void>();
let timer: ReturnType<typeof setInterval> | undefined;

function useNow(): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    listeners.add(setNow);
    timer ??= setInterval(() => {
      const t = Date.now();
      for (const l of listeners) l(t);
    }, 30_000);
    return () => {
      listeners.delete(setNow);
      if (listeners.size === 0 && timer) {
        clearInterval(timer);
        timer = undefined;
      }
    };
  }, []);
  return now;
}

/** "5 dk önce", with the exact time in a tooltip. */
export function RelativeTime({
  value,
  className,
}: {
  value: Timestamp | Date | undefined;
  className?: string;
}) {
  const now = useNow();
  const date = value instanceof Date ? value : toDate(value);
  if (!date) return <span className={className}>—</span>;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time dateTime={date.toISOString()} className={cn("tabular whitespace-nowrap", className)}>
          {formatRelative(date, now)}
        </time>
      </TooltipTrigger>
      <TooltipContent>{formatDateTime(date)}</TooltipContent>
    </Tooltip>
  );
}
