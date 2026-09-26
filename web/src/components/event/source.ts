import type { EventDetail } from "@/gen/izci/api/v1/event_pb";
import type { Frame } from "@/gen/izci/ingest/v1/event_pb";

/** Link to the frame's line in the linked repository, when one is known. */
export function sourceUrl(frame: Frame, d: EventDetail | undefined): string | undefined {
  if (!d?.repositoryUrl || !d.sourceRef || frame.inApp === false) return undefined;
  let path = frame.filename || frame.absPath;
  if (!path || /^[a-z][a-z0-9+.-]*:/i.test(path)) return undefined;
  for (const r of d.pathRewrites) {
    if (r.fromPrefix && path.startsWith(r.fromPrefix)) {
      path = r.toPrefix + path.slice(r.fromPrefix.length);
      break;
    }
  }
  path = path.replace(/^\.?\/+/, "");
  const encoded = path.split("/").map(encodeURIComponent).join("/");
  return `${d.repositoryUrl}/blob/${encodeURIComponent(d.sourceRef)}/${encoded}${frame.lineno ? `#L${frame.lineno}` : ""}`;
}

/** The last path segment of a file name or URL. */
export function basename(path: string): string {
  const clean = path.split(/[?#]/)[0] ?? path;
  return clean.slice(Math.max(clean.lastIndexOf("/"), clean.lastIndexOf("\\")) + 1) || clean;
}
