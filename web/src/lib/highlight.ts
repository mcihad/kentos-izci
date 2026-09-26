import { useQuery } from "@tanstack/react-query";
import type { HighlighterCore, ThemedToken } from "shiki/core";

type Grammar = { default: Parameters<HighlighterCore["loadLanguage"]>[0] };

// Grammars load on demand, one chunk per language. Only languages of the
// platforms izci targets are listed: every grammar ends up in the binary.
const grammars: Record<string, () => Promise<Grammar>> = {
  csharp: () => import("shiki/dist/langs/csharp.mjs"),
  go: () => import("shiki/dist/langs/go.mjs"),
  javascript: () => import("shiki/dist/langs/javascript.mjs"),
  typescript: () => import("shiki/dist/langs/typescript.mjs"),
  tsx: () => import("shiki/dist/langs/tsx.mjs"),
  python: () => import("shiki/dist/langs/python.mjs"),
  dart: () => import("shiki/dist/langs/dart.mjs"),
  rust: () => import("shiki/dist/langs/rust.mjs"),
  java: () => import("shiki/dist/langs/java.mjs"),
  kotlin: () => import("shiki/dist/langs/kotlin.mjs"),
  sql: () => import("shiki/dist/langs/sql.mjs"),
  json: () => import("shiki/dist/langs/json.mjs"),
  shellscript: () => import("shiki/dist/langs/shellscript.mjs"),
  yaml: () => import("shiki/dist/langs/yaml.mjs"),
  xml: () => import("shiki/dist/langs/xml.mjs"),
};

let core: Promise<HighlighterCore> | undefined;

function highlighter(): Promise<HighlighterCore> {
  core ??= (async () => {
    const [{ createHighlighterCore }, { createJavaScriptRegexEngine }, light, dark] = await Promise.all([
      import("shiki/core"),
      import("shiki/engine/javascript"),
      import("shiki/dist/themes/vitesse-light.mjs"),
      import("shiki/dist/themes/vitesse-dark.mjs"),
    ]);
    // The JavaScript regex engine avoids WebAssembly, which the page's CSP
    // does not allow.
    return createHighlighterCore({
      themes: [light.default, dark.default],
      langs: [],
      engine: createJavaScriptRegexEngine(),
    });
  })();
  return core;
}

const extensions: Record<string, string> = {
  cs: "csharp",
  go: "go",
  js: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  jsx: "tsx",
  ts: "typescript",
  mts: "typescript",
  cts: "typescript",
  tsx: "tsx",
  py: "python",
  dart: "dart",
  rs: "rust",
  java: "java",
  kt: "kotlin",
  sql: "sql",
  json: "json",
  sh: "shellscript",
  yml: "yaml",
  yaml: "yaml",
  xml: "xml",
};

const platforms: Record<string, string> = {
  dotnet: "csharp",
  csharp: "csharp",
  go: "go",
  node: "javascript",
  javascript: "javascript",
  python: "python",
  dart: "dart",
  flutter: "dart",
  rust: "rust",
  java: "java",
};

/** Language of a source file, falling back to the event platform. */
export function languageOf(filename: string, platform?: string): string | undefined {
  const clean = filename.split(/[?#]/)[0] ?? "";
  const ext = clean.slice(clean.lastIndexOf(".") + 1).toLowerCase();
  return extensions[ext] ?? (platform ? platforms[platform] : undefined);
}

async function tokenize(code: string, lang: string): Promise<ThemedToken[][]> {
  const load = grammars[lang];
  const hl = await highlighter();
  if (!load) return [];
  if (!hl.getLoadedLanguages().includes(lang)) await hl.loadLanguage((await load()).default);
  return hl.codeToTokens(code, {
    lang,
    themes: { light: "vitesse-light", dark: "vitesse-dark" },
    defaultColor: false,
  }).tokens;
}

/** Highlighted lines of code; undefined while loading or for unknown languages. */
export function useTokens(code: string, lang: string | undefined): ThemedToken[][] | undefined {
  const q = useQuery({
    queryKey: ["highlight", lang, code],
    queryFn: () => tokenize(code, lang ?? ""),
    enabled: !!lang && !!grammars[lang] && code.length < 200_000,
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: 5 * 60_000,
  });
  return q.data && q.data.length > 0 ? q.data : undefined;
}
