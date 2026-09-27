import { Link } from "@tanstack/react-router";
import { TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import { type DebugImage, type ProcessingError, ProcessingErrorKind } from "@/gen/izci/ingest/v1/event_pb";
import { useOrg } from "@/lib/org";

const code = (s: string) => <code className="font-mono [overflow-wrap:anywhere]">{s}</code>;

function describe(e: ProcessingError, release: string, debugId: string | undefined): ReactNode {
  switch (e.kind) {
    case ProcessingErrorKind.SOURCE_MAP_MISSING:
      return (
        <>
          {code(e.target)} betiği için kaynak eşlemesi bulunamadı
          {debugId ? (
            <> ({code(debugId)} debug kimliğiyle yüklenmemiş)</>
          ) : release ? (
            <> ({code(release)} sürümü için yüklenmemiş)</>
          ) : (
            " (olayda sürüm ya da debug kimliği yok)"
          )}
          .
        </>
      );
    case ProcessingErrorKind.SOURCE_MAP_INVALID:
      return <>{code(e.target)} için yüklenen kaynak eşlemesi okunamadı.</>;
    case ProcessingErrorKind.DEBUG_FILE_MISSING:
      return <>{code(e.target)} debug kimlikli sembol dosyası yüklenmemiş.</>;
    case ProcessingErrorKind.DEBUG_FILE_INVALID:
      return <>{code(e.target)} debug kimlikli sembol dosyası okunamadı.</>;
    default:
      return <>{code(e.target)} çözülemedi.</>;
  }
}

/** Why some frames of the event could not be resolved to original code. */
export function ProcessingErrors({
  errors,
  release,
  images,
}: {
  errors: ProcessingError[];
  release: string;
  images: DebugImage[];
}) {
  const org = useOrg();
  if (errors.length === 0) return null;
  return (
    <div
      role="note"
      className="mb-6 rounded-lg border border-status-warning-border bg-status-warning-bg px-4 py-3 text-sm text-status-warning-fg"
    >
      <p className="flex items-center gap-2 font-semibold">
        <TriangleAlertIcon className="size-4 shrink-0" />
        Yığın izinin bir kısmı özgün koda çevrilemedi
      </p>
      <ul className="mt-2 grid list-disc gap-1 pl-6">
        {errors.map((e) => (
          <li key={`${e.kind}-${e.target}`}>
            {describe(
              e,
              release,
              images.find((im) => im.type === "sourcemap" && im.codeFile === e.target)?.debugId,
            )}
          </li>
        ))}
      </ul>
      <p className="mt-2">
        Yükleme adımları:{" "}
        <Link
          to="/o/$org/docs/$"
          params={{ org: org.slug, _splat: "kaynak-eslemeleri" }}
          className="font-medium underline underline-offset-4"
        >
          Kaynak eşlemeleri ve semboller
        </Link>
      </p>
    </div>
  );
}
