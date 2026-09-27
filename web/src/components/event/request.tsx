import { TerminalIcon } from "lucide-react";
import { CodeBlock } from "@/components/common/code-block";
import { copyText } from "@/components/common/copy-button";
import { Button } from "@/components/ui/button";
import type { Request } from "@/gen/izci/ingest/v1/event_pb";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";

const filtered = "[Filtered]";

function quote(s: string): string {
  return `'${s.replaceAll("'", "'\\''")}'`;
}

/** Builds a cURL command that repeats the request (without scrubbed values). */
export function toCurl(r: Request): string {
  const url = quote(r.url + (r.queryString && !r.url.includes("?") ? `?${r.queryString}` : ""));
  const lines = [r.method && r.method !== "GET" ? `curl -X ${r.method} ${url}` : `curl ${url}`];
  for (const h of r.headers) {
    if (h.value === filtered || h.name.toLowerCase() === "content-length") continue;
    lines.push(`  -H ${quote(`${h.name}: ${h.value}`)}`);
  }
  if (r.body) lines.push(`  --data-raw ${quote(r.body)}`);
  return lines.join(" \\\n");
}

function pretty(body: string): { code: string; lang?: string } {
  try {
    return { code: JSON.stringify(JSON.parse(body), null, 2), lang: "json" };
  } catch {
    return { code: body };
  }
}

function Table({ rows, className }: { rows: [string, string][]; className?: string }) {
  return (
    <dl className={cn("divide-y overflow-hidden rounded-md border text-sm", className)}>
      {rows.map(([k, v], i) => (
        <div
          // biome-ignore lint/suspicious/noArrayIndexKey: repeated header names are allowed
          key={`${k}-${i}`}
          className="grid sm:grid-cols-[minmax(10rem,32%)_1fr]"
        >
          <dt
            className="px-3 pt-2 font-mono text-muted-foreground [overflow-wrap:anywhere] sm:bg-muted/40 sm:py-2"
            title={k}
          >
            {k}
          </dt>
          <dd className="px-3 pt-0.5 pb-2 font-mono [overflow-wrap:anywhere] sm:py-2">
            {v === filtered ? (
              <span className="rounded bg-muted px-1.5 py-0.5 font-sans text-muted-foreground text-xs">
                gizlendi
              </span>
            ) : (
              v
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}

export function RequestView({ request: r }: { request: Request }) {
  const query = r.queryString ? [...new URLSearchParams(r.queryString).entries()] : [];
  const body = r.body ? pretty(r.body) : undefined;
  return (
    <div className="grid grid-cols-1 gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="min-w-0 font-mono text-sm [overflow-wrap:anywhere]">
          {r.method ? <span className="mr-2 font-semibold">{r.method}</span> : null}
          {r.url}
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void copyText(toCurl(r), "cURL komutu kopyalandı")}
        >
          <TerminalIcon className="size-4" />
          cURL olarak kopyala
        </Button>
      </div>
      {r.route ? (
        <p className="-mt-2 text-sm text-muted-foreground">
          Rota <span className="font-mono text-foreground">{r.route}</span>
        </p>
      ) : null}
      {query.length > 0 ? (
        <div>
          <h4 className="mb-1.5 text-sm text-muted-foreground">Sorgu parametreleri</h4>
          <Table rows={query} />
        </div>
      ) : null}
      {r.headers.length > 0 ? (
        <div>
          <h4 className="mb-1.5 text-sm text-muted-foreground">Başlıklar</h4>
          <Table rows={r.headers.map((h) => [h.name, h.value])} />
        </div>
      ) : null}
      {Object.keys(r.cookies).length > 0 ? (
        <div>
          <h4 className="mb-1.5 text-sm text-muted-foreground">Çerezler</h4>
          <Table rows={Object.entries(r.cookies)} />
        </div>
      ) : null}
      {body ? (
        <div>
          <h4 className="mb-1.5 text-sm text-muted-foreground">
            Gövde {r.bodySize > 0n ? <span className="tabular">({formatBytes(r.bodySize)})</span> : null}
          </h4>
          <CodeBlock code={body.code} lang={body.lang} maxHeight="20rem" />
        </div>
      ) : null}
      {Object.keys(r.env).length > 0 ? (
        <div>
          <h4 className="mb-1.5 text-sm text-muted-foreground">Sunucu ortamı</h4>
          <Table rows={Object.entries(r.env)} />
        </div>
      ) : null}
    </div>
  );
}
