import type { Duration } from "@bufbuild/protobuf/wkt";
import { type AlertRule, type Channel, TriggerKind } from "@/gen/izci/api/v1/alert_pb";
import { Level } from "@/gen/izci/ingest/v1/event_pb";

export function seconds(d: Duration | undefined): number {
  return d ? Number(d.seconds) : 0;
}

/** "30 dakika", "1 saat", "2 gün" */
export function durationText(s: number): string {
  if (s <= 0) return "0 dakika";
  if (s % 86_400 === 0) return `${s / 86_400} gün`;
  if (s % 3600 === 0) return `${s / 3600} saat`;
  return `${Math.round(s / 60)} dakika`;
}

/** "30 dakikada bir", "1 saatte bir", "2 günde bir" (with Turkish suffixes). */
export function everyText(s: number): string {
  if (s % 86_400 === 0) return `${s / 86_400} günde bir`;
  if (s % 3600 === 0) return `${s / 3600} saatte bir`;
  return `${Math.round(s / 60)} dakikada bir`;
}

const levelWords: Partial<Record<Level, string>> = {
  [Level.WARNING]: "uyarı ve üstü",
  [Level.ERROR]: "hata ve üstü",
  [Level.FATAL]: "ölümcül",
};

/** A rule as one Turkish sentence, e.g. for the rule list. */
export function describeRule(r: AlertRule, channels: Channel[]): string {
  const f = r.filters;
  const scope = r.project ? `${r.project.name} projesinde` : "Tüm projelerde";
  const env = f?.environments.length ? ` ${f.environments.join(", ")} ortamında` : "";
  const level = f?.minLevel ? ` ${levelWords[f.minLevel] ?? ""} seviyede` : "";
  let what: string;
  switch (r.trigger?.kind) {
    case TriggerKind.REGRESSION:
      what = "çözülmüş bir hata geri döndüğünde";
      break;
    case TriggerKind.FREQUENCY:
      what = `bir hata ${durationText(seconds(r.trigger.window))} içinde ${r.trigger.threshold} kez görüldüğünde`;
      break;
    default:
      what = "yeni bir hata çıktığında";
  }
  const extra: string[] = [];
  if (f?.customers.length) extra.push(`yalnız ${f.customers.join(", ")} müşterisi için`);
  if (f?.unhandledOnly) extra.push("yalnız işlenmemiş istisnalarda");
  const names = r.channelIds.map((id) => channels.find((c) => c.id === id)?.name).filter(Boolean);
  const target =
    names.length === 1
      ? `${names[0]} kanalına`
      : names.length
        ? `${names.join(", ")} kanallarına`
        : "hiçbir kanala (kanal seçilmemiş)";
  const throttle = seconds(r.throttle);
  return `${scope}${env}${level} ${what}${extra.length ? `, ${extra.join(", ")}` : ""} ${target} bildirir.${
    throttle ? ` Aynı hata için en fazla ${everyText(throttle)}.` : ""
  }`;
}
