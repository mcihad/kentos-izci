import { IssueStatus, IssueSubstatus, Priority, Role, StatsPeriod } from "@/gen/izci/api/v1/common_pb";
import { Level } from "@/gen/izci/ingest/v1/event_pb";

/** Turkish level names and the token each level is drawn with. */
export const levels: Record<Level, { label: string; color: string; search: string }> = {
  [Level.UNSPECIFIED]: { label: "Belirsiz", color: "var(--level-debug)", search: "" },
  [Level.DEBUG]: { label: "Ayıklama", color: "var(--level-debug)", search: "debug" },
  [Level.INFO]: { label: "Bilgi", color: "var(--level-info)", search: "info" },
  [Level.WARNING]: { label: "Uyarı", color: "var(--level-warning)", search: "warning" },
  [Level.ERROR]: { label: "Hata", color: "var(--level-error)", search: "error" },
  [Level.FATAL]: { label: "Ölümcül", color: "var(--level-fatal)", search: "fatal" },
};

/** Levels from most to least severe. */
export const levelOrder = [Level.FATAL, Level.ERROR, Level.WARNING, Level.INFO, Level.DEBUG];

export const statusLabels: Record<IssueStatus, string> = {
  [IssueStatus.UNSPECIFIED]: "",
  [IssueStatus.UNRESOLVED]: "Çözülmedi",
  [IssueStatus.RESOLVED]: "Çözüldü",
  [IssueStatus.IGNORED]: "Yoksayıldı",
};

export const substatusLabels: Record<IssueSubstatus, string> = {
  [IssueSubstatus.UNSPECIFIED]: "",
  [IssueSubstatus.NEW]: "Yeni",
  [IssueSubstatus.ONGOING]: "Süren",
  [IssueSubstatus.REGRESSED]: "Geri döndü",
  [IssueSubstatus.ESCALATING]: "Artışta",
};

export const priorityLabels: Record<Priority, string> = {
  [Priority.UNSPECIFIED]: "",
  [Priority.LOW]: "Düşük",
  [Priority.MEDIUM]: "Orta",
  [Priority.HIGH]: "Yüksek",
  [Priority.CRITICAL]: "Kritik",
};

export const roleLabels: Record<Role, string> = {
  [Role.UNSPECIFIED]: "",
  [Role.VIEWER]: "İzleyici",
  [Role.MEMBER]: "Üye",
  [Role.ADMIN]: "Yönetici",
  [Role.OWNER]: "Sahip",
};

/** Periods offered by pickers, shortest first. */
export const periods: { value: StatsPeriod; label: string; short: string }[] = [
  { value: StatsPeriod.LAST_HOUR, label: "Son 1 saat", short: "1 sa" },
  { value: StatsPeriod.LAST_24_HOURS, label: "Son 24 saat", short: "24 sa" },
  { value: StatsPeriod.LAST_7_DAYS, label: "Son 7 gün", short: "7 gün" },
  { value: StatsPeriod.LAST_14_DAYS, label: "Son 14 gün", short: "14 gün" },
  { value: StatsPeriod.LAST_30_DAYS, label: "Son 30 gün", short: "30 gün" },
  { value: StatsPeriod.LAST_90_DAYS, label: "Son 90 gün", short: "90 gün" },
];

/** URL-friendly period keys used in search params. */
export const periodKeys: Record<string, StatsPeriod> = {
  "1h": StatsPeriod.LAST_HOUR,
  "24h": StatsPeriod.LAST_24_HOURS,
  "7d": StatsPeriod.LAST_7_DAYS,
  "14d": StatsPeriod.LAST_14_DAYS,
  "30d": StatsPeriod.LAST_30_DAYS,
  "90d": StatsPeriod.LAST_90_DAYS,
};

export function periodKey(p: StatsPeriod): string {
  return Object.entries(periodKeys).find(([, v]) => v === p)?.[0] ?? "24h";
}

export const platformLabels: Record<string, string> = {
  dotnet: ".NET",
  csharp: "C#",
  go: "Go",
  node: "Node.js",
  javascript: "JavaScript",
  typescript: "TypeScript",
  python: "Python",
  dart: "Dart",
  flutter: "Flutter",
  rust: "Rust",
  java: "Java",
  php: "PHP",
  ruby: "Ruby",
  other: "Diğer",
};

export function platformLabel(p: string): string {
  return platformLabels[p] ?? (p ? p : "Diğer");
}
