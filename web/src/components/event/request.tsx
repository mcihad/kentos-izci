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
    <div className={cn("overflow-hidden rounded-md border", className)}>
      <table className="w-full table-fixed text-[12.5px]">
        <tbody className="divide-y">
          {rows.map(([k, v], i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: repeated header names are allowed
            <tr key={`${k}-${i}`} className="align-top">
              <td
                className="w-[34%] truncate bg-muted/40 px-3 py-1.5 font-mono text-muted-foreground"
                title={k}
              >
                {k}
              </td>
              <td className="break-all px-3 py-1.5 font-mono">
                {v === filtered ? (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-muted-foreground text-xs">
                    gizlendi
                  </span>
                ) : (
                  v
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function RequestView({ request: r }: { request: Request }) {
  const query = r.queryString ? [...new URLSearchParams(r.queryString).entries()] : [];
  const body = r.body ? pretty(r.body) : undefined;
  return (
    <div className="grid grid-cols-1 gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="min-w-0 break-all font-mono text-[13px]">
          {r.method ? <span className="mr-2 font-semibold">{r.method}</span> : null}
          {r.url}
        </p>
        <Button
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          onClick={() => void copyText(toCurl(r), "cURL komutu kopyalandı")}
        >
          <TerminalIcon className="size-3.5" />
          cURL olarak kopyala
        </Button>
      </div>
      {r.route ? (
        <p className="-mt-2 text-[12.5px] text-muted-foreground">
          Rota <span className="font-mono text-foreground">{r.route}</span>
        </p>
      ) : null}
      {query.length > 0 ? (
        <div>
          <h4 className="mb-1.5 text-[12.5px] text-muted-foreground">Sorgu parametreleri</h4>
          <Table rows={query} />
        </div>
      ) : null}
      {r.headers.length > 0 ? (
        <div>
          <h4 className="mb-1.5 text-[12.5px] text-muted-foreground">Başlıklar</h4>
          <Table rows={r.headers.map((h) => [h.name, h.value])} />
        </div>
      ) : null}
      {Object.keys(r.cookies).length > 0 ? (
        <div>
          <h4 className="mb-1.5 text-[12.5px] text-muted-foreground">Çerezler</h4>
          <Table rows={Object.entries(r.cookies)} />
        </div>
      ) : null}
      {body ? (
        <div>
          <h4 className="mb-1.5 text-[12.5px] text-muted-foreground">
            Gövde {r.bodySize > 0n ? <span className="tabular">({formatBytes(r.bodySize)})</span> : null}
          </h4>
          <CodeBlock code={body.code} lang={body.lang} maxHeight="20rem" />
        </div>
      ) : null}
      {Object.keys(r.env).length > 0 ? (
        <div>
          <h4 className="mb-1.5 text-[12.5px] text-muted-foreground">Sunucu ortamı</h4>
          <Table rows={Object.entries(r.env)} />
        </div>
      ) : null}
    </div>
  );
}
