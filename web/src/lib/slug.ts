const turkish: Record<string, string> = {
  ç: "c",
  ğ: "g",
  ı: "i",
  i̇: "i",
  ö: "o",
  ş: "s",
  ü: "u",
  â: "a",
  î: "i",
  û: "u",
};

/**
 * Mirrors the server's slug rules (internal/slug): lowercase ASCII letters,
 * digits and single hyphens, starting with a letter, at most maxLen long.
 */
export function slugify(name: string, maxLen = 50): string {
  let s = name.toLocaleLowerCase("tr-TR").replace(/[çğıöşüâîû]|i̇/g, (c) => turkish[c] ?? c);
  s = s.replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  if (/^[0-9]/.test(s)) s = `p-${s}`;
  if (s.length > maxLen) s = s.slice(0, maxLen).replace(/-+$/, "");
  return s.length < 2 ? "" : s;
}

export const slugPattern = /^[a-z][a-z0-9-]{0,48}[a-z0-9]$/;
