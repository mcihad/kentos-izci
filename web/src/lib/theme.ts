import { useSyncExternalStore } from "react";

/** The theme the user picked; "system" follows the operating system. */
export type ThemeChoice = "dark" | "light" | "system";

const storageKey = "izci-theme";
const media = typeof window !== "undefined" ? window.matchMedia("(prefers-color-scheme: dark)") : null;
const listeners = new Set<() => void>();

function readChoice(): ThemeChoice {
  try {
    const v = localStorage.getItem(storageKey);
    if (v === "dark" || v === "light") return v;
  } catch {
    // Storage can be unavailable (private mode); fall through to system.
  }
  return "system";
}

let choice: ThemeChoice = typeof window !== "undefined" ? readChoice() : "dark";

function resolve(c: ThemeChoice): "dark" | "light" {
  if (c === "system") return media?.matches === false ? "light" : "dark";
  return c;
}

function apply() {
  const dark = resolve(choice) === "dark";
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
  for (const l of listeners) l();
}

media?.addEventListener("change", () => {
  if (choice === "system") apply();
});

/** Changes and persists the theme. */
export function setTheme(next: ThemeChoice) {
  choice = next;
  try {
    if (next === "system") localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, next);
  } catch {
    // Not persisted; still applied for this page.
  }
  apply();
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

/** Current theme choice and the resolved (effective) theme. */
export function useTheme() {
  const current = useSyncExternalStore(subscribe, () => choice);
  const resolved = useSyncExternalStore(subscribe, () => resolve(choice));
  return { choice: current, resolved, setTheme };
}
