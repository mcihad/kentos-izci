import { DownloadIcon, FileIcon, FileTextIcon } from "lucide-react";
import type { EventAttachment } from "@/gen/izci/api/v1/event_pb";
import { formatBytes } from "@/lib/format";

const images = ["image/png", "image/jpeg", "image/gif", "image/webp"];
const texts = ["text/plain", "application/json"];

/** What an attachment is, in the words of the UI. */
function kind(a: EventAttachment): string {
  switch (a.attachmentType) {
    case "event.view_hierarchy":
      return "Görünüm hiyerarşisi";
    case "event.minidump":
      return "Minidump";
    case "event.applecrashreport":
      return "Apple çökme raporu";
  }
  if (images.includes(a.contentType)) return "Görsel";
  if (texts.includes(a.contentType)) return "Metin";
  return a.contentType;
}

/**
 * Files sent with the event. Images show inline; text opens in a new tab;
 * everything else downloads (the server never renders HTML or scripts).
 */
export function Attachments({ files }: { files: EventAttachment[] }) {
  const shots = files.filter((f) => images.includes(f.contentType));
  const others = files.filter((f) => !images.includes(f.contentType));
  return (
    <div className="grid gap-4">
      {shots.length > 0 ? (
        <div className="flex flex-wrap gap-3">
          {shots.map((f) => (
            <figure key={f.id} className="w-[min(100%,15rem)]">
              <a
                href={f.url}
                target="_blank"
                rel="noreferrer"
                className="block overflow-hidden rounded-md border bg-muted/40"
              >
                <img src={f.url} alt={f.name} loading="lazy" className="max-h-72 w-full object-contain" />
              </a>
              <figcaption className="mt-1.5 flex items-center justify-between gap-2 text-[12px] text-muted-foreground">
                <span className="truncate font-mono" title={f.name}>
                  {f.name}
                </span>
                <a
                  href={`${f.url}?download=1`}
                  className="shrink-0 hover:text-foreground"
                  aria-label={`${f.name} indir`}
                >
                  <DownloadIcon className="size-3.5" />
                </a>
              </figcaption>
            </figure>
          ))}
        </div>
      ) : null}
      {others.length > 0 ? (
        <ul className="divide-y rounded-md border">
          {others.map((f) => {
            const Icon = texts.includes(f.contentType) ? FileTextIcon : FileIcon;
            return (
              <li key={f.id} className="flex items-center gap-3 px-3 py-2 text-[13px]">
                <Icon className="size-4 shrink-0 text-muted-foreground" />
                {texts.includes(f.contentType) ? (
                  <a
                    href={f.url}
                    target="_blank"
                    rel="noreferrer"
                    className="min-w-0 truncate font-mono text-[12.5px] hover:underline"
                  >
                    {f.name}
                  </a>
                ) : (
                  <span className="min-w-0 truncate font-mono text-[12.5px]">{f.name}</span>
                )}
                <span className="ml-auto flex shrink-0 gap-3 text-[12px] text-muted-foreground">
                  <span>{kind(f)}</span>
                  <span className="tabular">{formatBytes(f.sizeBytes)}</span>
                </span>
                <a
                  href={`${f.url}?download=1`}
                  className="shrink-0 text-muted-foreground hover:text-foreground"
                  aria-label={`${f.name} indir`}
                >
                  <DownloadIcon className="size-3.5" />
                </a>
              </li>
            );
          })}
        </ul>
      ) : null}
    </div>
  );
}
