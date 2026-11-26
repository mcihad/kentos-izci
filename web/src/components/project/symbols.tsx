import { createConnectQueryKey, useInfiniteQuery, useMutation, useQuery } from "@connectrpc/connect-query";
import { thClass, theadClass, trClass } from "@kentos/components/common/table";
import { cn } from "@kentos/components/lib/utils";
import { Button } from "@kentos/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@kentos/components/ui/dialog";
import { Skeleton } from "@kentos/components/ui/skeleton";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Trash2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { CodeBlock } from "@/components/common/code-block";
import { RelativeTime } from "@/components/common/relative-time";
import { EmptyState } from "@/components/common/states";
import { pill, tones } from "@/components/issue/badges";
import { type Project, ProjectService } from "@/gen/izci/api/v1/project_pb";
import {
  type ArtifactBundle,
  ArtifactFileKind,
  type DebugFile,
  DebugFileKind,
  SymbolService,
} from "@/gen/izci/api/v1/symbol_pb";
import { errorMessage } from "@/lib/errors";
import { formatBytes } from "@/lib/format";
import { useOrg } from "@/lib/org";
import { canAdmin } from "@/lib/session";

type Guide = { id: string; label: string; lang: string; intro: string; code: string };

function guides(base: string, org: string, project: string): Guide[] {
  return [
    {
      id: "vite",
      label: "Vite",
      lang: "typescript",
      intro:
        "Derleme sırasında kaynak eşlemelerini yükler ve betiklere debug kimliği ekler; sürüm adı vermek zorunlu değildir.",
      code: `// vite.config.ts (npm i -D @sentry/vite-plugin)
import { sentryVitePlugin } from "@sentry/vite-plugin";

export default defineConfig({
  build: { sourcemap: "hidden" },
  plugins: [
    sentryVitePlugin({
      url: "${base}",
      org: "${org}",
      project: "${project}",
      authToken: process.env.SENTRY_AUTH_TOKEN,
      sourcemaps: { filesToDeleteAfterUpload: ["dist/**/*.map"] },
    }),
  ],
});`,
    },
    {
      id: "webpack",
      label: "webpack",
      lang: "javascript",
      intro: "Next.js, Create React App ve diğer webpack projelerinde aynı ayarlar geçerlidir.",
      code: `// webpack.config.js (npm i -D @sentry/webpack-plugin)
const { sentryWebpackPlugin } = require("@sentry/webpack-plugin");

module.exports = {
  devtool: "hidden-source-map",
  plugins: [
    sentryWebpackPlugin({
      url: "${base}",
      org: "${org}",
      project: "${project}",
      authToken: process.env.SENTRY_AUTH_TOKEN,
    }),
  ],
};`,
    },
    {
      id: "cli",
      label: "sentry-cli",
      lang: "shellscript",
      intro: "Derlemeden sonra, yayından önce çalıştırın. inject komutu betiklere debug kimliği ekler.",
      code: `export SENTRY_URL=${base}
export SENTRY_AUTH_TOKEN=izk_…
sentry-cli sourcemaps inject ./dist
sentry-cli sourcemaps upload --org ${org} --project ${project} ./dist`,
    },
    {
      id: "flutter",
      label: "Flutter",
      lang: "yaml",
      intro:
        "--obfuscate ve --split-debug-info ile derlenen uygulamaların yığın izleri ve hata türleri, sembol dosyaları ve karıştırma haritasıyla çözülür.",
      code: `# pubspec.yaml (dev_dependencies: sentry_dart_plugin)
sentry:
  url: ${base}
  org: ${org}
  project: ${project}
  auth_token: izk_…   # ya da SENTRY_AUTH_TOKEN ortam değişkeni
  upload_debug_symbols: true
  symbols_path: build/debug-info
  dart_symbol_map_path: build/app/obfuscation.map.json

# Derleme ve yükleme:
# flutter build apk --obfuscate --split-debug-info=build/debug-info \\
#   --extra-gen-snapshot-options=--save-obfuscation-map=build/app/obfuscation.map.json
# dart run sentry_dart_plugin`,
    },
  ];
}

const fileKindLabels: Record<ArtifactFileKind, string> = {
  [ArtifactFileKind.UNSPECIFIED]: "Dosya",
  [ArtifactFileKind.MINIFIED_SOURCE]: "Betik",
  [ArtifactFileKind.SOURCE_MAP]: "Kaynak eşlemesi",
  [ArtifactFileKind.SOURCE]: "Kaynak",
};

const debugKindLabels: Record<DebugFileKind, string> = {
  [DebugFileKind.UNSPECIFIED]: "Sembol dosyası",
  [DebugFileKind.ELF]: "ELF sembolleri",
  [DebugFileKind.MACHO]: "Mach-O dSYM",
  [DebugFileKind.DART_SYMBOL_MAP]: "Dart karıştırma haritası",
};

type Deleting = { kind: "bundle"; bundle: ArtifactBundle } | { kind: "file"; file: DebugFile };

/** Source maps and debug files: how to upload them, and what is uploaded. */
export function ProjectSymbols({ project }: { project: Project }) {
  const org = useOrg();
  const admin = canAdmin(org.role);
  const qc = useQueryClient();
  const ep = useQuery(ProjectService.method.getIngestEndpoints, { project: project.slug });
  const [active, setActive] = useState(() => (project.platform === "dart" ? "flutter" : "vite"));
  const [deleting, setDeleting] = useState<Deleting>();
  const bundles = useInfiniteQuery(
    SymbolService.method.listArtifactBundles,
    { project: project.slug, pageSize: 20, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const files = useInfiniteQuery(
    SymbolService.method.listDebugFiles,
    { project: project.slug, pageSize: 20, pageToken: "" },
    { pageParamKey: "pageToken", getNextPageParam: (last) => last.nextPageToken || undefined },
  );
  const refresh = () => {
    void qc.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: SymbolService,
        transport: org.transport,
        cardinality: undefined,
      }),
    });
  };
  const onDone = (msg: string) => () => {
    toast.success(msg);
    setDeleting(undefined);
    refresh();
  };
  const onError = (err: Error) => toast.error(errorMessage(err));
  const deleteBundle = useMutation(SymbolService.method.deleteArtifactBundle, {
    onSuccess: onDone("Paket silindi"),
    onError,
  });
  const deleteFile = useMutation(SymbolService.method.deleteDebugFile, {
    onSuccess: onDone("Sembol dosyası silindi"),
    onError,
  });
  const bundleRows = bundles.data?.pages.flatMap((p) => p.bundles) ?? [];
  const fileRows = files.data?.pages.flatMap((p) => p.files) ?? [];
  const list = ep.data ? guides(ep.data.baseUrl, org.slug, project.slug) : [];
  const current = list.find((g) => g.id === active) ?? list[0];

  return (
    <div className="grid max-w-4xl grid-cols-1 *:min-w-0 gap-8 py-6">
      <section className="grid grid-cols-1 gap-3">
        <div>
          <h2 className="font-semibold text-lg">Kaynak eşlemeleri ve semboller</h2>
          <p className="mt-0.5 text-md text-muted-foreground">
            Küçültülmüş JavaScript ve karıştırılmış Flutter kodunun yığın izleri, yüklediğiniz kaynak
            eşlemeleri ve sembol dosyalarıyla özgün dosya, satır ve fonksiyon adlarına çevrilir. Sentry'nin
            derleme eklentileri ve sentry-cli olduğu gibi çalışır; anahtar olarak{" "}
            <Link
              from="/o/$org/projects/$slug"
              search={{ tab: "keys" }}
              className="text-foreground underline underline-offset-4"
            >
              sürüm kaydı yetkisi olan bir API anahtarı
            </Link>{" "}
            verin.
          </p>
        </div>
        {ep.data && current ? (
          <>
            <div
              className="scrollbar-none flex gap-1 overflow-x-auto border-b max-sm:fade-end max-sm:pr-8"
              role="tablist"
              aria-label="Araç"
            >
              {list.map((g) => (
                <button
                  key={g.id}
                  type="button"
                  role="tab"
                  aria-selected={g.id === current.id}
                  onClick={() => setActive(g.id)}
                  className={cn(
                    "-mb-px min-h-11 whitespace-nowrap border-b-2 px-3 text-md transition-colors sm:min-h-10",
                    g.id === current.id
                      ? "border-primary font-semibold text-foreground"
                      : "border-transparent text-muted-foreground hover:text-foreground",
                  )}
                >
                  {g.label}
                </button>
              ))}
            </div>
            <p className="text-md text-muted-foreground">{current.intro}</p>
            <CodeBlock code={current.code} lang={current.lang} maxHeight="30rem" />
          </>
        ) : (
          <Skeleton className="h-64 w-full" />
        )}
      </section>

      <section>
        <h2 className="mb-3 font-semibold text-lg">Kaynak eşleme paketleri</h2>
        {bundles.isPending ? (
          <Skeleton className="h-32 w-full" />
        ) : bundleRows.length === 0 ? (
          <EmptyState title="Henüz kaynak eşlemesi yüklenmedi" className="rounded-lg border">
            Derleme eklentisi ya da sentry-cli ilk yüklemeyi yaptığında paketler burada listelenir.
          </EmptyState>
        ) : (
          <>
            <ul className="divide-y rounded-lg border">
              {bundleRows.map((b) => (
                <BundleRow
                  key={b.id}
                  bundle={b}
                  onDelete={admin ? () => setDeleting({ kind: "bundle", bundle: b }) : undefined}
                />
              ))}
            </ul>
            {bundles.hasNextPage ? (
              <Button
                variant="outline"
                size="sm"
                className="mt-3"
                disabled={bundles.isFetchingNextPage}
                onClick={() => void bundles.fetchNextPage()}
              >
                Daha fazla göster
              </Button>
            ) : null}
          </>
        )}
      </section>

      <section>
        <h2 className="mb-3 font-semibold text-lg">Sembol dosyaları</h2>
        {files.isPending ? (
          <Skeleton className="h-32 w-full" />
        ) : fileRows.length === 0 ? (
          <EmptyState title="Henüz sembol dosyası yüklenmedi" className="rounded-lg border">
            Flutter uygulamasını --split-debug-info ile derleyip sentry_dart_plugin ya da sentry-cli
            debug-files upload ile yüklediğinizde burada listelenir.
          </EmptyState>
        ) : (
          <>
            <ul className="divide-y rounded-lg border md:hidden">
              {fileRows.map((f) => (
                <li key={f.id} className="px-4 py-3.5">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="font-medium font-mono text-sm [overflow-wrap:anywhere]">{f.name}</p>
                      <p className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
                        <span className={cn(pill, tones.neutral)}>{debugKindLabels[f.kind]}</span>
                        {f.arch ? <span>{f.arch}</span> : null}
                        <span className="tabular">{formatBytes(f.sizeBytes)}</span>
                        <RelativeTime value={f.createdAt} />
                      </p>
                      <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{f.debugId}</p>
                    </div>
                    {admin ? (
                      <DeleteButton
                        label={`${f.name} dosyasını sil`}
                        onClick={() => setDeleting({ kind: "file", file: f })}
                      />
                    ) : null}
                  </div>
                </li>
              ))}
            </ul>
            <div className="hidden overflow-x-auto rounded-lg border md:block">
              <table className="w-full min-w-[760px] text-sm">
                <thead className={theadClass}>
                  <tr>
                    <th className={thClass}>Dosya</th>
                    <th className={thClass}>Tür</th>
                    <th className={thClass}>Debug kimliği</th>
                    <th className={cn(thClass, "text-right")}>Boyut</th>
                    <th className={thClass}>Yüklendi</th>
                    <th className={thClass}>Son kullanım</th>
                    {admin ? <th className={thClass} aria-label="İşlemler" /> : null}
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {fileRows.map((f) => (
                    <tr key={f.id} className={trClass}>
                      <td className="px-3.5 py-2">
                        <span className="block font-medium font-mono">{f.name}</span>
                        {f.arch ? <span className="text-xs text-muted-foreground">{f.arch}</span> : null}
                      </td>
                      <td className="px-3.5 py-2">
                        <span className={cn(pill, tones.neutral)}>{debugKindLabels[f.kind]}</span>
                      </td>
                      <td className="px-3.5 py-2 font-mono text-xs text-muted-foreground">{f.debugId}</td>
                      <td className="px-3.5 py-2 text-right tabular">{formatBytes(f.sizeBytes)}</td>
                      <td className="px-3.5 py-2 text-muted-foreground">
                        <RelativeTime value={f.createdAt} />
                      </td>
                      <td className="px-3.5 py-2 text-muted-foreground">
                        {f.lastUsedAt ? <RelativeTime value={f.lastUsedAt} /> : "henüz yok"}
                      </td>
                      {admin ? (
                        <td className="px-2 py-1 text-right">
                          <DeleteButton
                            label={`${f.name} dosyasını sil`}
                            onClick={() => setDeleting({ kind: "file", file: f })}
                          />
                        </td>
                      ) : null}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {files.hasNextPage ? (
              <Button
                variant="outline"
                size="sm"
                className="mt-3"
                disabled={files.isFetchingNextPage}
                onClick={() => void files.fetchNextPage()}
              >
                Daha fazla göster
              </Button>
            ) : null}
          </>
        )}
      </section>

      <Dialog open={!!deleting} onOpenChange={(v) => !v && setDeleting(undefined)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{deleting?.kind === "file" ? "Sembol dosyasını sil" : "Paketi sil"}</DialogTitle>
            <DialogDescription>
              {deleting?.kind === "file" ? (
                <>
                  <strong className="text-foreground">{deleting.file.name}</strong> silinince bu derlemeden
                  gelen yeni hataların yığın izleri çözülmez. Kayıtlı hatalar değişmez.
                </>
              ) : deleting?.kind === "bundle" ? (
                <>
                  {deleting.bundle.release ? (
                    <strong className="text-foreground">{deleting.bundle.release}</strong>
                  ) : (
                    "Bu"
                  )}{" "}
                  paketi silinince bu dosyalarla gelen yeni hataların yığın izleri çözülmez. Kayıtlı hatalar
                  değişmez.
                </>
              ) : null}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleting(undefined)}>
              Vazgeç
            </Button>
            <Button
              variant="destructive"
              disabled={deleteBundle.isPending || deleteFile.isPending}
              onClick={() => {
                if (deleting?.kind === "bundle") {
                  deleteBundle.mutate({ project: project.slug, id: deleting.bundle.id });
                } else if (deleting?.kind === "file") {
                  deleteFile.mutate({ project: project.slug, id: deleting.file.id });
                }
              }}
            >
              Sil
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function DeleteButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <Button
      variant="ghost"
      size="icon"
      className="size-9 shrink-0 text-muted-foreground"
      aria-label={label}
      onClick={onClick}
    >
      <Trash2Icon className="size-4" />
    </Button>
  );
}

function BundleRow({ bundle: b, onDelete }: { bundle: ArtifactBundle; onDelete?: () => void }) {
  const shown = b.files.slice(0, 4);
  return (
    <li className="px-4 py-3.5 sm:px-5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
            {b.release ? (
              <span className="font-mono font-semibold text-md [overflow-wrap:anywhere]">{b.release}</span>
            ) : (
              <span className="font-semibold text-md">Sürümsüz paket</span>
            )}
            {b.dist ? <span className={cn(pill, tones.neutral)}>{b.dist}</span> : null}
            {b.files.some((f) => f.debugId) ? (
              <span className={cn(pill, tones.info)}>debug kimlikli</span>
            ) : null}
          </p>
          <p className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-sm text-muted-foreground">
            <span className="tabular">{b.fileCount} dosya</span>
            <span className="tabular">{formatBytes(b.sizeBytes)}</span>
            <span>
              yüklendi <RelativeTime value={b.createdAt} />
            </span>
            <span>
              {b.lastUsedAt ? (
                <>
                  son kullanım <RelativeTime value={b.lastUsedAt} />
                </>
              ) : (
                "henüz kullanılmadı"
              )}
            </span>
          </p>
          <ul className="mt-2 grid gap-0.5">
            {shown.map((f) => (
              <li key={f.url} className="flex min-w-0 items-baseline gap-2 text-xs">
                <span className="w-24 shrink-0 text-muted-foreground">{fileKindLabels[f.kind]}</span>
                <span className="min-w-0 truncate font-mono">{f.url}</span>
              </li>
            ))}
            {b.fileCount > shown.length ? (
              <li className="text-xs text-muted-foreground">ve {b.fileCount - shown.length} dosya daha</li>
            ) : null}
          </ul>
        </div>
        {onDelete ? <DeleteButton label="Paketi sil" onClick={onDelete} /> : null}
      </div>
    </li>
  );
}
