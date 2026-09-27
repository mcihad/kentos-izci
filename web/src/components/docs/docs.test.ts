import { describe, expect, it } from "vitest";
import { resolveGuideLink, sections } from "./docs";

const known = new Set(["kurulum", "yapilandirma", "api/izci.ingest.v1", "api/izci.api.v1"]);

describe("resolveGuideLink", () => {
  it("opens guides inside the app", () => {
    expect(resolveGuideLink("kentos", "kurulum", "yapilandirma.md#github", known)).toBe(
      "/o/kentos/docs/yapilandirma#github",
    );
    expect(resolveGuideLink("kentos", "kurulum", "api/izci.ingest.v1.md", known)).toBe(
      "/o/kentos/docs/api/izci.ingest.v1",
    );
    expect(resolveGuideLink("kentos", "api/izci.ingest.v1", "izci.api.v1.md", known)).toBe(
      "/o/kentos/docs/api/izci.api.v1",
    );
  });
  it("leaves external and in-page links alone and hides repository files", () => {
    expect(resolveGuideLink("kentos", "kurulum", "https://opencode.ai", known)).toBeUndefined();
    expect(resolveGuideLink("kentos", "kurulum", "#gereksinimler", known)).toBeUndefined();
    expect(resolveGuideLink("kentos", "kurulum", "../examples/go-basic/main.go", known)).toBeNull();
  });
});

describe("sections", () => {
  it("groups in order", () => {
    const g = (slug: string, section: string) => ({ slug, section, title: slug, description: "" });
    expect(sections([g("a", "X"), g("b", "X"), g("c", "Y")]).map((s) => [s.name, s.guides.length])).toEqual([
      ["X", 2],
      ["Y", 1],
    ]);
  });
});
