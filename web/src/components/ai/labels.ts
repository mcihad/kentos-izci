import { type AiRun, AiRunStatus, AiRunTrigger, Risk } from "@/gen/izci/api/v1/ai_pb";
import { toDate } from "@/lib/format";

export const runStatus: Record<AiRunStatus, { label: string; tone: string; active?: boolean }> = {
  [AiRunStatus.UNSPECIFIED]: { label: "Bilinmiyor", tone: "text-muted-foreground border-border" },
  [AiRunStatus.QUEUED]: { label: "Sırada", tone: "text-muted-foreground border-border", active: true },
  [AiRunStatus.PREPARING]: { label: "Hazırlanıyor", tone: "text-primary border-primary/40", active: true },
  [AiRunStatus.RUNNING]: { label: "Çalışıyor", tone: "text-primary border-primary/40", active: true },
  [AiRunStatus.SUCCEEDED]: { label: "Tamamlandı", tone: "text-health-good-ink border-health-good/40" },
  [AiRunStatus.FAILED]: { label: "Başarısız", tone: "text-level-error-ink border-level-error/40" },
  [AiRunStatus.CANCELLED]: { label: "İptal edildi", tone: "text-muted-foreground border-border" },
};

export const runTrigger: Record<AiRunTrigger, string> = {
  [AiRunTrigger.UNSPECIFIED]: "",
  [AiRunTrigger.MANUAL]: "Arayüzden",
  [AiRunTrigger.AUTOMATIC]: "Otomatik",
  [AiRunTrigger.CLI]: "Komut satırından",
  [AiRunTrigger.MCP]: "MCP ile",
  [AiRunTrigger.TELEGRAM]: "Telegram'dan",
};

export const riskLabels: Record<Risk, { label: string; tone: string } | undefined> = {
  [Risk.UNSPECIFIED]: undefined,
  [Risk.LOW]: { label: "Düşük risk", tone: "text-health-good-ink" },
  [Risk.MEDIUM]: { label: "Orta risk", tone: "text-level-warning-ink" },
  [Risk.HIGH]: { label: "Yüksek risk", tone: "text-level-error-ink" },
};

/** True while a run is queued or working. */
export function isActive(run: AiRun | undefined): boolean {
  return !!run && !!runStatus[run.status]?.active;
}

/** Milliseconds a run took (or has taken so far). */
export function runDuration(run: AiRun, now = Date.now()): number | undefined {
  const start = toDate(run.startedAt);
  if (!start) return undefined;
  const end = toDate(run.finishedAt)?.getTime() ?? now;
  return Math.max(0, end - start.getTime());
}

/** Added and removed lines of a run's change. */
export function changeStats(run: AiRun): { files: number; additions: number; deletions: number } {
  let additions = 0;
  let deletions = 0;
  for (const f of run.files) {
    additions += f.additions;
    deletions += f.deletions;
  }
  return { files: run.files.length, additions, deletions };
}

/** "$0,12" for model costs. */
export function formatCost(usd: number): string {
  return new Intl.NumberFormat("tr-TR", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: usd < 1 ? 3 : 2,
  }).format(usd);
}
