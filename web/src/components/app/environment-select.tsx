import { useQuery } from "@connectrpc/connect-query";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@kentos/components/ui/select";
import { LayersIcon } from "lucide-react";
import { ProjectService } from "@/gen/izci/api/v1/project_pb";

/** Environment picker; "" means all environments. */
export function EnvironmentSelect({ value, onChange }: { value: string; onChange: (env: string) => void }) {
  const envs = useQuery(ProjectService.method.listEnvironments, {}, { staleTime: 5 * 60_000 });
  return (
    <Select value={value || "*"} onValueChange={(v) => onChange(v === "*" ? "" : v)}>
      <SelectTrigger size="sm" className="gap-1.5 text-sm" aria-label="Ortam">
        <LayersIcon className="size-3.5 text-muted-foreground" />
        <SelectValue />
      </SelectTrigger>
      <SelectContent align="end">
        <SelectItem value="*">Tüm ortamlar</SelectItem>
        {envs.data?.environments.map((e) => (
          <SelectItem key={e.name} value={e.name}>
            {e.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
