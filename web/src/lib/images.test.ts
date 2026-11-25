import { describe, expect, it } from "vitest";
import { localImage } from "./images";

const origin = "https://izci.example.com";

describe("localImage", () => {
  it("loads images of izci itself", () => {
    expect(localImage("/attachments/a.png", origin)).toBe("/attachments/a.png");
    expect(localImage("https://izci.example.com/x.png?v=1", origin)).toBe("/x.png?v=1");
  });
  it.each([
    "https://evil.example/pixel.png",
    "//evil.example/pixel.png",
    "/\\evil.example/pixel.png",
    "http://izci.example.com/x.png",
    "javascript:alert(1)",
    "",
    undefined,
  ])("does not load %s", (src) => {
    expect(localImage(src, origin)).toBeUndefined();
  });
});
