import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { EventDetailSchema, PathRewriteSchema } from "@/gen/izci/api/v1/event_pb";
import { FrameSchema, RequestSchema } from "@/gen/izci/ingest/v1/event_pb";
import { toCurl } from "./request";
import { basename, sourceUrl } from "./source";

describe("sourceUrl", () => {
  const detail = create(EventDetailSchema, {
    repositoryUrl: "https://github.com/kentos/kalem",
    sourceRef: "4f9c2a1",
    pathRewrites: [create(PathRewriteSchema, { fromPrefix: "/app/", toPrefix: "src/" })],
  });

  it("maps frame paths into the repository", () => {
    const f = create(FrameSchema, { filename: "/app/Services/Order Service.cs", lineno: 142, inApp: true });
    expect(sourceUrl(f, detail)).toBe(
      "https://github.com/kentos/kalem/blob/4f9c2a1/src/Services/Order%20Service.cs#L142",
    );
  });

  it("skips library frames and URLs", () => {
    expect(
      sourceUrl(create(FrameSchema, { filename: "net/http/server.go", inApp: false }), detail),
    ).toBeUndefined();
    expect(
      sourceUrl(create(FrameSchema, { filename: "https://cdn.example.com/app.js", inApp: true }), detail),
    ).toBeUndefined();
  });

  it("needs a linked repository", () => {
    const f = create(FrameSchema, { filename: "main.go", inApp: true });
    expect(sourceUrl(f, create(EventDetailSchema, {}))).toBeUndefined();
  });

  it("extracts base names from paths and URLs", () => {
    expect(basename("src/Services/OrderService.cs")).toBe("OrderService.cs");
    expect(basename("C:\\app\\Program.cs")).toBe("Program.cs");
    expect(basename("https://kalem.example.com/assets/form-Dk39aLq1.js?v=2")).toBe("form-Dk39aLq1.js");
  });
});

describe("toCurl", () => {
  it("repeats the request without scrubbed headers", () => {
    const r = create(RequestSchema, {
      method: "POST",
      url: "https://kalem.example.com/api/orders",
      queryString: "page=1",
      headers: [
        { name: "Content-Type", value: "application/json" },
        { name: "Authorization", value: "[Filtered]" },
      ],
      body: `{"note":"it's"}`,
    });
    expect(toCurl(r)).toBe(
      [
        "curl -X POST 'https://kalem.example.com/api/orders?page=1'",
        "  -H 'Content-Type: application/json'",
        `  --data-raw '{"note":"it'\\''s"}'`,
      ].join(" \\\n"),
    );
  });
});
