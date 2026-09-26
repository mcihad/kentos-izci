import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";

const numberFmt = new Intl.NumberFormat("tr-TR");
const compactFmt = new Intl.NumberFormat("tr-TR", { notation: "compact", maximumFractionDigits: 1 });
const percentFmt = new Intl.NumberFormat("tr-TR", { style: "percent", maximumFractionDigits: 1 });
const dateTimeFmt = new Intl.DateTimeFormat("tr-TR", {
  day: "numeric",
  month: "long",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});
const dateFmt = new Intl.DateTimeFormat("tr-TR", { day: "numeric", month: "short", year: "numeric" });
const shortDateFmt = new Intl.DateTimeFormat("tr-TR", { day: "numeric", month: "short" });
const timeFmt = new Intl.DateTimeFormat("tr-TR", { hour: "2-digit", minute: "2-digit" });

type Numeric = number | bigint;

/** 12.345 */
export function formatNumber(n: Numeric): string {
  return numberFmt.format(n);
}

/** 1,2 B / 3,4 Mn for large numbers, exact below 10.000. */
export function formatCompact(n: Numeric): string {
  const v = typeof n === "bigint" ? Number(n) : n;
  return Math.abs(v) < 10_000 ? numberFmt.format(v) : compactFmt.format(v);
}

/** %12,5 */
export function formatPercent(ratio: number): string {
  return percentFmt.format(ratio);
}

export function toDate(ts: Timestamp | undefined): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

/** 26 Eylül 2026 14:32:05 */
export function formatDateTime(d: Date | Timestamp | undefined): string {
  const date = d instanceof Date ? d : toDate(d);
  return date ? dateTimeFmt.format(date) : "";
}

/** 26 Eyl 2026 */
export function formatDate(d: Date | Timestamp | undefined): string {
  const date = d instanceof Date ? d : toDate(d);
  return date ? dateFmt.format(date) : "";
}

/** 14:32 */
export function formatTime(d: Date): string {
  return timeFmt.format(d);
}

/** 26 Eyl */
export function formatShortDate(d: Date): string {
  return shortDateFmt.format(d);
}

/**
 * Compact relative time for lists: "az önce", "5 dk önce", "3 sa önce",
 * "2 gün önce", then a date.
 */
export function formatRelative(d: Date | Timestamp | undefined, now = Date.now()): string {
  const date = d instanceof Date ? d : toDate(d);
  if (!date) return "";
  const diff = Math.round((now - date.getTime()) / 1000);
  const future = diff < 0;
  const s = Math.abs(diff);
  let text: string;
  if (s < 45) return future ? "birazdan" : "az önce";
  if (s < 3600) text = `${Math.max(1, Math.round(s / 60))} dk`;
  else if (s < 86_400) text = `${Math.round(s / 3600)} sa`;
  else if (s < 30 * 86_400) text = `${Math.round(s / 86_400)} gün`;
  else return formatDate(date);
  return future ? `${text} sonra` : `${text} önce`;
}

/** 1,2 GB */
export function formatBytes(n: Numeric): string {
  let v = typeof n === "bigint" ? Number(n) : n;
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${new Intl.NumberFormat("tr-TR", { maximumFractionDigits: v < 10 && i > 0 ? 1 : 0 }).format(v)} ${units[i]}`;
}

/** 3 sa 12 dk, 42 sn, 850 ms */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s} sn`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} dk ${s % 60} sn`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h} sa ${m % 60} dk`;
  return `${Math.floor(h / 24)} gün ${h % 24} sa`;
}

/** Initials for avatars: "Ayşe Yılmaz" → "AY". */
export function initials(name: string, fallback = "?"): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return fallback;
  const first = parts[0]?.[0] ?? "";
  const last = parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? "") : "";
  return (first + last).toLocaleUpperCase("tr-TR");
}
