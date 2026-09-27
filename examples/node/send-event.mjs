// Reports a caught error to izci with the Connect protocol (JSON over HTTP),
// using only Node.js built-ins (Node 18+). The same code works in TypeScript.
//
//   IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… node examples/node/send-event.mjs
//
// For typed clients generate code from proto/izci/ingest/v1 with
// @bufbuild/protoc-gen-es and use @connectrpc/connect-node.

const url = process.env.IZCI_URL ?? "http://localhost:5300";
const key = process.env.IZCI_KEY;
if (!key) throw new Error("Set IZCI_KEY to an ingest key of the project (izk_…).");

// V8 prints frames newest first ("    at fn (file:line:col)"); izci wants the
// oldest call first.
function frames(stack) {
  const out = [];
  for (const line of stack.split("\n").slice(1)) {
    const m = /^\s*at (?:(.+?) \()?(.+?):(\d+):(\d+)\)?$/.exec(line);
    if (!m) continue;
    const [, fn, file, lineno, colno] = m;
    out.unshift({
      function: fn ?? "<anonymous>",
      filename: file.replace(/^file:\/\//, ""),
      lineno: Number(lineno),
      colno: Number(colno),
      inApp: !file.startsWith("node:") && !file.includes("node_modules"),
    });
  }
  return out;
}

async function report(err, extra = {}) {
  const exceptions = [];
  for (let e = err; e; e = e.cause) {
    exceptions.unshift({ type: e.name, value: e.message, stacktrace: { frames: frames(e.stack ?? "") } });
  }
  const res = await fetch(`${url}/izci.ingest.v1.IngestService/SendEvent`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${key}` },
    body: JSON.stringify({
      event: {
        level: "LEVEL_ERROR",
        platform: "node",
        environment: "production",
        release: "rapor@2.1.0",
        contexts: { runtime: { name: "node", version: process.versions.node } },
        exceptions,
        ...extra,
      },
    }),
  });
  const body = await res.json();
  if (!res.ok) throw new Error(`izci answered ${res.status}: ${body.message}`);
  return body.result;
}

function renderReport(definition) {
  return definition.layout.columns.map((c) => c.name);
}

try {
  renderReport({ id: 12 });
} catch (err) {
  const result = await report(new Error("Rapor oluşturulamadı", { cause: err }), {
    customer: { id: "tokat-belediyesi", name: "Tokat Belediyesi" },
    tags: { modul: "rapor" },
  });
  console.log(result.outcome, result.issueShortId, result.issueUrl);
}
