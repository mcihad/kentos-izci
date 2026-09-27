import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { MonitorIntervalUnit, MonitorScheduleSchema } from "@/gen/izci/api/v1/monitor_pb";
import { describeCrontab, describeSchedule } from "./cron";

describe("describeCrontab", () => {
  it.each([
    ["0 3 * * *", "Her gün 03:00"],
    ["30 2 * * 1-5", "Hafta içi her gün 02:30"],
    ["0 9 * * 1", "Her Pazartesi 09:00"],
    ["0 0 * * 7", "Her Pazar 00:00"],
    ["15 4 1 * *", "Her ayın 1. günü 04:15"],
    ["*/10 * * * *", "10 dakikada bir"],
    ["* * * * *", "Her dakika"],
    ["0 * * * *", "Her saat başı"],
    ["5 * * * *", "Her saat 5 geçe"],
    ["0 */6 * * *", "6 saatte bir, saat başında"],
    ["30 */2 * * *", "2 saatte bir, 30 geçe"],
    ["@daily", "Her gün 00:00"],
  ])("%s → %s", (expr, want) => {
    expect(describeCrontab(expr)).toBe(want);
  });

  it.each(["0 3 1 1 *", "1,31 * * * *", "0 3 * * MON", "bozuk"])("%s is left alone", (expr) => {
    expect(describeCrontab(expr)).toBeUndefined();
  });
});

describe("describeSchedule", () => {
  const interval = (value: number, unit: MonitorIntervalUnit) =>
    create(MonitorScheduleSchema, { kind: { case: "interval", value: { value, unit } } });
  it("reads intervals", () => {
    expect(describeSchedule(interval(2, MonitorIntervalUnit.HOUR))).toBe("2 saatte bir");
    expect(describeSchedule(interval(1, MonitorIntervalUnit.DAY))).toBe("Her gün");
    expect(describeSchedule(interval(1, MonitorIntervalUnit.MINUTE))).toBe("Her dakika");
  });
  it("falls back to the crontab", () => {
    const s = create(MonitorScheduleSchema, { kind: { case: "crontab", value: "0 3 1 1 *" } });
    expect(describeSchedule(s)).toBe("0 3 1 1 *");
  });
});
