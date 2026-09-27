import { tones } from "@/components/issue/badges";
import { type AiRun, AiRunStatus, AiRunTrigger, Risk } from "@/gen/izci/api/v1/ai_pb";
import { toDate } from "@/lib/format";

export const runStatus: Record<AiRunStatus, { label: string; tone: string; active?: boolean }> = {
  [AiRunStatus.UNSPECIFIED]: { label: "Bilinmiyor", tone: tones.neutral },
  [AiRunStatus.QUEUED]: { label: "Sırada", tone: tones.neutral, active: true },
  [AiRunStatus.PREPARING]: { label: "Hazırlanıyor", tone: tones.info, active: true },
  [AiRunStatus.RUNNING]: { label: "Çalışıyor", tone: tones.info, active: true },
  [AiRunStatus.SUCCEEDED]: { label: "Tamamlandı", tone: tones.success },
  [AiRunStatus.FAILED]: { label: "Başarısız", tone: tones.danger },
  [AiRunStatus.CANCELLED]: { label: "İptal edildi", tone: tones.neutral },
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
