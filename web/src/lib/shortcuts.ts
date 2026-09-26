import { useEffect, useRef, useSyncExternalStore } from "react";

type Handler = (e: KeyboardEvent) => void;

function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  return (
    ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName) || target.getAttribute("role") === "combobox"
  );
}

function comboOf(e: KeyboardEvent): string {
  const key = e.key.length === 1 ? e.key.toLowerCase() : e.key;
  return (e.metaKey || e.ctrlKey ? "mod+" : "") + key;
}

/**
 * Registers keyboard shortcuts ("j", "?", "Enter", "mod+k") while mounted.
 * Plain keys are ignored while the user types into a field or a dialog is
 * open; "mod+" combinations always fire.
 */
export function useHotkeys(bindings: Record<string, Handler>, enabled = true) {
  const ref = useRef(bindings);
  ref.current = bindings;
  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing) return;
      const combo = comboOf(e);
      const handler = ref.current[combo];
      if (!handler) return;
      if (!combo.startsWith("mod+")) {
        if (e.altKey || isTyping(e.target)) return;
        if (document.querySelector("[role=dialog][data-state=open], [role=menu][data-state=open]")) return;
      }
      e.preventDefault();
      handler(e);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enabled]);
}

/** A tiny global boolean store (open state of app-wide dialogs). */
function createFlag() {
  let value = false;
  const listeners = new Set<() => void>();
  return {
    get: () => value,
    set: (v: boolean) => {
      value = v;
      for (const l of listeners) l();
    },
    subscribe: (l: () => void) => {
      listeners.add(l);
      return () => {
        listeners.delete(l);
      };
    },
  };
}

export const paletteFlag = createFlag();
export const shortcutHelpFlag = createFlag();

export function useFlag(flag: ReturnType<typeof createFlag>) {
  return [useSyncExternalStore(flag.subscribe, flag.get), flag.set] as const;
}

export function useShortcutHelp() {
  return () => shortcutHelpFlag.set(true);
}

/** Shortcut reference shown in the help dialog. */
export const shortcutGroups: { title: string; items: [string[], string][] }[] = [
  {
    title: "Her yerde",
    items: [
      [["⌘", "K"], "Arama ve komutlar"],
      [["?"], "Bu listeyi göster"],
    ],
  },
  {
    title: "Hata listesi",
    items: [
      [["/"], "Aramaya odaklan"],
      [["J"], "Sonraki hata"],
      [["K"], "Önceki hata"],
      [["Enter"], "Hatayı aç"],
      [["X"], "Seç ya da seçimi kaldır"],
      [["R"], "Çöz"],
      [["I"], "Yoksay"],
      [["A"], "Bana ata"],
    ],
  },
  {
    title: "Hata ayrıntısı",
    items: [
      [["R"], "Çöz ya da yeniden aç"],
      [["I"], "Yoksay"],
      [["A"], "Bana ata"],
      [["["], "Önceki olay"],
      [["]"], "Sonraki olay"],
    ],
  },
];
