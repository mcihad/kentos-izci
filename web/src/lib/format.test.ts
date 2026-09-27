import { describe, expect, it } from "vitest";
import { formatBytes, formatCompact, formatDuration, formatRelative, initials } from "./format";

describe("formatRelative", () => {
  const now = Date.parse("2026-09-26T12:00:00Z");
  it.each([
    [now - 10_000, "az önce"],
    [now - 5 * 60_000, "5 dk önce"],
    [now - 3 * 3_600_000, "3 sa önce"],
    [now - 2 * 86_400_000, "2 gün önce"],
    [now + 10 * 60_000, "10 dk sonra"],
    [now - 3590_000, "1 sa önce"],
    [now - 23.8 * 3_600_000, "1 gün önce"],
  ])("%s → %s", (t, want) => {
    expect(formatRelative(new Date(t), now)).toBe(want);
  });

  it("falls back to a date after a month", () => {
    expect(formatRelative(new Date(now - 40 * 86_400_000), now)).toMatch(/2026/);
  });
});

describe("number formatting", () => {
  it("keeps small numbers exact and compacts large ones in Turkish", () => {
    expect(formatCompact(9_999)).toBe("9.999");
    expect(formatCompact(12_345)).toBe("12,3\u00a0B");
    expect(formatCompact(3_400_000n)).toBe("3,4\u00a0Mn");
  });

  it("formats bytes and durations", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1,5 KB");
    expect(formatBytes(16n << 30n)).toBe("16 GB");
    expect(formatDuration(850)).toBe("850 ms");
    expect(formatDuration(42_000)).toBe("42 sn");
    expect(formatDuration(3 * 3_600_000 + 12 * 60_000)).toBe("3 sa 12 dk");
  });

  it("builds initials with Turkish casing", () => {
    expect(initials("ayşe yılmaz")).toBe("AY");
    expect(initials("irmak")).toBe("İ");
    expect(initials("  ")).toBe("?");
  });
});
