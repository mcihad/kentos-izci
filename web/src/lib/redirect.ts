/** Accepts only same-origin absolute paths as post-login destinations. */
export function safeRedirect(target: string | undefined): string | undefined {
  if (!target?.startsWith("/") || target.startsWith("//") || target.startsWith("/\\")) return undefined;
  if (target.startsWith("/login") || target.startsWith("/setup")) return undefined;
  return target;
}
