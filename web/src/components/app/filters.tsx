import { useQuery } from "@connectrpc/connect-query";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@kentos/components/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { CalendarClockIcon, CheckIcon, ChevronDownIcon, FolderIcon } from "lucide-react";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";
import { periodKey, periodKeys, periods } from "@/lib/labels";

/** Period picker bound to a URL key such as "24h" or "14d". */
export function PeriodSelect({
  value,
  onChange,
  exclude = [],
  className,
}: {
  value: string;
  onChange: (key: string) => void;
  exclude?: string[];
  className?: string;
}) {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger size="sm" className={cn("gap-1.5 text-sm", className)} aria-label="Dönem">
        <CalendarClockIcon className="size-3.5 text-muted-foreground" />
        <SelectValue />
      </SelectTrigger>
      <SelectContent align="end">
        {periods
          .filter((p) => !exclude.includes(periodKey(p.value)))
          .map((p) => (
            <SelectItem key={p.value} value={periodKey(p.value)}>
              {p.label}
            </SelectItem>
          ))}
      </SelectContent>
    </Select>
  );
}

export function parsePeriod(key: string | undefined, fallback: string) {
  const k = key && key in periodKeys ? key : fallback;
  return { key: k, value: periodKeys[k] ?? periodKeys[fallback] };
}

/** Multi-select of projects; an empty selection means all projects. */
export function ProjectFilter({ value, onChange }: { value: string[]; onChange: (slugs: string[]) => void }) {
  const projects = useQuery(ProjectService.method.listProjects, {}, { staleTime: 60_000 });
  const list = projects.data?.projects ?? [];
  const label =
    value.length === 0
      ? "Tüm projeler"
      : value.length === 1
        ? (list.find((p) => p.slug === value[0])?.name ?? value[0])
        : `${value.length} proje`;
  const toggle = (slug: string) =>
    onChange(value.includes(slug) ? value.filter((s) => s !== slug) : [...value, slug]);
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className="gap-1.5 font-normal text-sm">
          <FolderIcon className="size-3.5 text-muted-foreground" />
          <span className="max-w-36 truncate">{label}</span>
          <ChevronDownIcon className="size-3.5 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-64 p-1">
        <button
          type="button"
          onClick={() => onChange([])}
          className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-accent"
        >
          <span className="flex-1">Tüm projeler</span>
          {value.length === 0 ? <CheckIcon className="size-4 text-brand-ink" /> : null}
        </button>
        <div className="my-1 h-px bg-border" />
        {list.map((p) => (
          <button
            key={p.id}
            type="button"
            onClick={() => toggle(p.slug)}
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-accent"
          >
            <span className="min-w-0 flex-1 truncate">{p.name}</span>
            <span className="font-mono text-xs text-muted-foreground">{p.slug}</span>
            <CheckIcon className={cn("size-4 text-brand-ink", !value.includes(p.slug) && "invisible")} />
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}
