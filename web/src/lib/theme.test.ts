import { describe, expect, it } from "vitest";
import { classesOf, defaultAppearance } from "./theme";

describe("classesOf", () => {
  it("needs no scope classes for the defaults", () => {
    expect(classesOf(defaultAppearance)).toEqual([]);
  });

  it("adds one class per non-default choice", () => {
    expect(
      classesOf({
        mode: "dark",
        font: "inter",
        density: "compact",
        radius: "soft",
        accent: "teal",
        elevation: "raised",
      }),
    ).toEqual(["font-inter", "density-compact", "radius-soft", "accent-teal", "elevation-raised"]);
  });

  it("leaves the light/dark mode to the dark class", () => {
    expect(classesOf({ ...defaultAppearance, mode: "dark" })).toEqual([]);
  });
});
