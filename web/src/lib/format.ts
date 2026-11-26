import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";
import {
  formatDate as formatDateOf,
  formatDateTime as formatDateTimeOf,
  formatRelative as formatRelativeOf,
} from "@kentos/components/lib/format";

/*
 * Number and date formatting. The Turkish formats come from the KentOS kit
 * (@kentos/components/lib/format); these wrappers also take the API's
 * protobuf Timestamps.
 */

export {
  formatBytes,
  formatClock,
  formatCompact,
  formatDuration,
  formatNumber,
  formatPercent,
  formatShortDate,
  formatTime,
  initials,
  type Numeric,
} from "@kentos/components/lib/format";

export function toDate(ts: Timestamp | undefined): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

/** 26 Eylül 2026 14:32:05 */
export function formatDateTime(d: Date | Timestamp | undefined): string {
  const date = d instanceof Date ? d : toDate(d);
  return date ? formatDateTimeOf(date) : "";
}

/** 26 Eyl 2026 */
export function formatDate(d: Date | Timestamp | undefined): string {
  const date = d instanceof Date ? d : toDate(d);
  return date ? formatDateOf(date) : "";
}

/**
 * Compact relative time for lists: "az önce", "5 dk önce", "3 sa önce",
 * "2 gün önce", then a date.
 */
export function formatRelative(d: Date | Timestamp | undefined, now = Date.now()): string {
  const date = d instanceof Date ? d : toDate(d);
  return date ? formatRelativeOf(date, now) : "";
}
