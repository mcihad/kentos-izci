import { useSyncExternalStore } from "react";

/*
 * Appearance: the KentOS design system's theme scopes, chosen per browser.
 * Each choice is one class on <html> (see index.css); public/theme-init.js
 * applies the same classes before first paint and must stay in step with
 * `classesOf` below.
 */

export type ThemeMode = "light" | "dark" | "system";
export type FontChoice = "jakarta" | "inter" | "plex";
export type Density = "compact" | "standard" | "roomy";
export type Radius = "square" | "tight" | "moderate" | "soft";
export type Accent = "navy" | "teal" | "violet" | "green" | "amber" | "neutral";
export type Elevation = "flat" | "bordered" | "raised";

export type Appearance = {
  mode: ThemeMode;
  font: FontChoice;
  density: Density;
  radius: Radius;
  accent: Accent;
  elevation: Elevation;
};

export const defaultAppearance: Appearance = {
  mode: "system",
  font: "jakarta",
  density: "standard",
  radius: "moderate",
  accent: "navy",
  elevation: "bordered",
};

const storageKey = "izci-appearance";
const legacyKey = "izci-theme";
const scopePattern = /^(font|density|radius|accent|elevation)-/;
const media = typeof window !== "undefined" ? window.matchMedia("(prefers-color-scheme: dark)") : null;
const listeners = new Set<() => void>();

const allowed: { [K in keyof Appearance]: readonly Appearance[K][] } = {
  mode: ["light", "dark", "system"],
  font: ["jakarta", "inter", "plex"],
  density: ["compact", "standard", "roomy"],
  radius: ["square", "tight", "moderate", "soft"],
  accent: ["navy", "teal", "violet", "green", "amber", "neutral"],
  elevation: ["flat", "bordered", "raised"],
};

function sanitize(raw: unknown): Appearance {
  const out = { ...defaultAppearance };
  if (!raw || typeof raw !== "object") return out;
  for (const key of Object.keys(allowed) as (keyof Appearance)[]) {
    const v = (raw as Record<string, unknown>)[key];
    if ((allowed[key] as readonly unknown[]).includes(v)) (out as Record<string, unknown>)[key] = v;
  }
  return out;
}

function read(): Appearance {
  try {
    const stored = localStorage.getItem(storageKey);
    if (stored) return sanitize(JSON.parse(stored));
    // Before appearance settings there was only the light/dark choice.
    const legacy = localStorage.getItem(legacyKey);
    if (legacy === "dark" || legacy === "light") return { ...defaultAppearance, mode: legacy };
  } catch {
    // Storage can be unavailable (private mode) or hold garbage; use defaults.
  }
  return { ...defaultAppearance };
}

let current: Appearance = typeof window !== "undefined" ? read() : { ...defaultAppearance };

function resolveMode(mode: ThemeMode): "dark" | "light" {
  if (mode === "system") return media?.matches ? "dark" : "light";
  return mode;
}

/** The scope classes an appearance puts on <html>; defaults need none. */
export function classesOf(a: Appearance): string[] {
  const out: string[] = [];
  if (a.font !== "jakarta") out.push(`font-${a.font}`);
  if (a.density !== "standard") out.push(`density-${a.density}`);
  if (a.radius !== "moderate") out.push(`radius-${a.radius}`);
  if (a.accent !== "navy") out.push(`accent-${a.accent}`);
  if (a.elevation !== "bordered") out.push(`elevation-${a.elevation}`);
  return out;
}

function apply() {
  const root = document.documentElement;
  const dark = resolveMode(current.mode) === "dark";
  for (const c of Array.from(root.classList)) if (scopePattern.test(c)) root.classList.remove(c);
  root.classList.add(...classesOf(current));
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
  for (const l of listeners) l();
}

media?.addEventListener("change", () => {
  if (current.mode === "system") apply();
});

/** Changes part of the appearance and persists it for this browser. */
export function setAppearance(patch: Partial<Appearance>) {
  current = { ...current, ...patch };
  try {
    localStorage.setItem(storageKey, JSON.stringify(current));
    localStorage.removeItem(legacyKey);
  } catch {
    // Not persisted; still applied for this page.
  }
  apply();
}

/** Back to the design system's defaults. */
export function resetAppearance() {
  current = { ...defaultAppearance };
  try {
    localStorage.removeItem(storageKey);
    localStorage.removeItem(legacyKey);
  } catch {
    // Nothing stored.
  }
  apply();
}

/** Shorthand for the light/dark choice. */
export function setTheme(mode: ThemeMode) {
  setAppearance({ mode });
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

/** The current appearance and the effective light/dark mode. */
export function useAppearance() {
  const appearance = useSyncExternalStore(subscribe, () => current);
  const resolved = useSyncExternalStore(subscribe, () => resolveMode(current.mode));
  return { appearance, resolved, setAppearance, resetAppearance };
}
