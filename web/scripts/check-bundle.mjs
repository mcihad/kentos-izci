// Fails the build when the JavaScript needed for the first page load grows
// past the budget. "Initial" means the entry script and the modules
// index.html preloads; routes and syntax grammars load on demand.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync } from "node:zlib";

const budgetKB = 200;
const dist = join(import.meta.dirname, "..", "dist");
const html = readFileSync(join(dist, "index.html"), "utf8");
const files = [...html.matchAll(/(?:src|href)="\/(assets\/[^"]+\.js)"/g)].map((m) => m[1]);

let total = 0;
for (const f of files) {
  const gz = gzipSync(readFileSync(join(dist, f)), { level: 9 }).length;
  total += gz;
  console.log(`${(gz / 1024).toFixed(1).padStart(7)} KB  ${f}`);
}
console.log(`${(total / 1024).toFixed(1).padStart(7)} KB  initial JavaScript (gzip), budget ${budgetKB} KB`);
if (total > budgetKB * 1024) {
  console.error("Bundle budget exceeded: lazy-load the new dependency or raise the budget deliberately.");
  process.exit(1);
}
