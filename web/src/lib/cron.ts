import { type Monitor, MonitorIntervalUnit, type MonitorSchedule } from "@/gen/izci/api/v1/monitor_pb";

const days = ["Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"];
const pad = (n: number) => String(n).padStart(2, "0");

const descriptors: Record<string, string> = {
  "@yearly": "Her yıl 1 Ocak 00:00",
  "@annually": "Her yıl 1 Ocak 00:00",
  "@monthly": "Her ayın 1'i 00:00",
  "@weekly": "Her Pazar 00:00",
  "@daily": "Her gün 00:00",
  "@midnight": "Her gün 00:00",
  "@hourly": "Her saat başı",
};

const num = (s: string) => (/^\d+$/.test(s) ? Number(s) : undefined);

/**
 * A Turkish reading of common crontab expressions, e.g. "0 3 * * *" →
 * "Her gün 03:00". Returns undefined for expressions it does not recognize;
 * callers then show the expression itself.
 */
export function describeCrontab(expr: string): string | undefined {
  const e = expr.trim().replace(/\s+/g, " ");
  if (descriptors[e]) return descriptors[e];
  const f = e.split(" ");
  if (f.length !== 5) return undefined;
  const [min = "", hour = "", dom = "", month = "", dow = ""] = f;
  const m = num(min);
  const h = num(hour);
  if (month !== "*") return undefined;
  const everyMin = /^\*\/(\d+)$/.exec(min);
  if (everyMin && hour === "*" && dom === "*" && dow === "*") {
    return everyMin[1] === "1" ? "Her dakika" : `${everyMin[1]} dakikada bir`;
  }
  if (min === "*" && hour === "*" && dom === "*" && dow === "*") return "Her dakika";
  const everyHour = /^\*\/(\d+)$/.exec(hour);
  if (m !== undefined && m < 60 && everyHour && dom === "*" && dow === "*") {
    return `${everyHour[1]} saatte bir, ${m === 0 ? "saat başında" : `${m} geçe`}`;
  }
  if (m !== undefined && m < 60 && hour === "*" && dom === "*" && dow === "*") {
    return m === 0 ? "Her saat başı" : `Her saat ${m} geçe`;
  }
  if (m === undefined || h === undefined || h > 23 || m > 59) return undefined;
  const at = `${pad(h)}:${pad(m)}`;
  if (dom === "*" && dow === "*") return `Her gün ${at}`;
  if (dom === "*" && dow === "1-5") return `Hafta içi her gün ${at}`;
  const d = num(dow);
  if (dom === "*" && d !== undefined && d <= 7) return `Her ${days[d % 7]} ${at}`;
  const dm = num(dom);
  if (dow === "*" && dm !== undefined && dm >= 1 && dm <= 31) return `Her ayın ${dm}. günü ${at}`;
  return undefined;
}

// "2 saatte bir" and "Her saat" for each unit.
const every: Record<MonitorIntervalUnit, [string, string]> = {
  [MonitorIntervalUnit.UNSPECIFIED]: ["", ""],
  [MonitorIntervalUnit.MINUTE]: ["dakikada", "dakika"],
  [MonitorIntervalUnit.HOUR]: ["saatte", "saat"],
  [MonitorIntervalUnit.DAY]: ["günde", "gün"],
  [MonitorIntervalUnit.WEEK]: ["haftada", "hafta"],
  [MonitorIntervalUnit.MONTH]: ["ayda", "ay"],
  [MonitorIntervalUnit.YEAR]: ["yılda", "yıl"],
};

/** Unit names for forms. */
export const intervalUnitLabels: [MonitorIntervalUnit, string][] = [
  [MonitorIntervalUnit.MINUTE, "dakika"],
  [MonitorIntervalUnit.HOUR, "saat"],
  [MonitorIntervalUnit.DAY, "gün"],
  [MonitorIntervalUnit.WEEK, "hafta"],
  [MonitorIntervalUnit.MONTH, "ay"],
  [MonitorIntervalUnit.YEAR, "yıl"],
];

/** "Her gün 03:00", "2 saatte bir" or the crontab itself. */
export function describeSchedule(s: MonitorSchedule | undefined): string {
  if (s?.kind.case === "interval") {
    const { value, unit } = s.kind.value;
    const [plural, singular] = every[unit];
    return value === 1 ? `Her ${singular}` : `${value} ${plural} bir`;
  }
  if (s?.kind.case === "crontab") return describeCrontab(s.kind.value) ?? s.kind.value;
  return "";
}

/** The raw schedule for monospace display: the crontab, or "" for intervals. */
export function crontabOf(m: Monitor): string {
  return m.schedule?.kind.case === "crontab" ? m.schedule.kind.value : "";
}
