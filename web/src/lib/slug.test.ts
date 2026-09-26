import { describe, expect, it } from "vitest";
import { slugify } from "./slug";

// Same cases as internal/slug on the server, so both sides agree.
describe("slugify", () => {
  it.each([
    ["Sivas Belediyesi Şikâyet", "sivas-belediyesi-sikayet"],
    ["KentOS Kalem", "kentos-kalem"],
    ["İzmir Ağ Çözümleri", "izmir-ag-cozumleri"],
    ["  --Özel__Proje--  ", "ozel-proje"],
    ["2026 Bütçe", "p-2026-butce"],
    ["!", ""],
  ])("%s → %s", (name, want) => {
    expect(slugify(name)).toBe(want);
  });

  it("respects the length limit without a trailing hyphen", () => {
    const s = slugify(`${"a".repeat(30)} ${"b".repeat(30)}`, 31);
    expect(s).toBe("a".repeat(30));
  });
});
