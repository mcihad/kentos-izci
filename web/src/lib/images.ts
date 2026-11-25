/**
 * The address of an image a document may load, or undefined. Only images of
 * izci itself are loaded: reports partly come from event data, and an
 * image from another site would tell that site who opened the report and
 * when.
 */
export function localImage(src: string | undefined, origin: string): string | undefined {
  if (!src) return undefined;
  try {
    const url = new URL(src, origin);
    return url.origin === origin ? url.pathname + url.search + url.hash : undefined;
  } catch {
    return undefined;
  }
}
