import { describe, expect, it } from "vitest";
import { safeRedirect } from "./redirect";

describe("safeRedirect", () => {
  it("accepts same-origin paths", () => {
    expect(safeRedirect("/o/kentos/issues?query=is:new")).toBe("/o/kentos/issues?query=is:new");
  });
  it.each(["https://evil.example", "//evil.example", "/\\evil.example", "/login", "/setup", "", undefined])(
    "rejects %s",
    (v) => {
      expect(safeRedirect(v)).toBeUndefined();
    },
  );
});
