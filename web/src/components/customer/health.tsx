import { cn } from "@kentos/components/lib/utils";
import { Health } from "@/gen/izci/api/v1/customer_pb";

export const healthLabels: Record<Health, { label: string; color: string }> = {
  [Health.UNSPECIFIED]: { label: "Bilinmiyor", color: "var(--muted-foreground)" },
  [Health.HEALTHY]: { label: "Sağlıklı", color: "var(--health-good)" },
  [Health.DEGRADED]: { label: "Sorunlu", color: "var(--level-warning)" },
  [Health.CRITICAL]: { label: "Kritik", color: "var(--level-error)" },
};

export function HealthLabel({ health, className }: { health: Health; className?: string }) {
  const h = healthLabels[health];
  return (
    <span className={cn("inline-flex items-center gap-1.5 whitespace-nowrap", className)}>
      <span className="size-2 rounded-full" style={{ background: h.color }} />
      {h.label}
    </span>
  );
}
