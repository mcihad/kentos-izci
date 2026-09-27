// Runs synchronously in <head> so the saved appearance is applied before first
// paint. Mirrors lib/theme.ts (classesOf); keep the two in step.
(() => {
  const root = document.documentElement;
  let a = {};
  try {
    const stored = localStorage.getItem("izci-appearance");
    if (stored) a = JSON.parse(stored) || {};
    else {
      const legacy = localStorage.getItem("izci-theme");
      if (legacy === "dark" || legacy === "light") a = { mode: legacy };
    }
  } catch {
    // Storage may be unavailable (private mode); use the defaults.
  }
  const pick = (v, list, fallback) => (list.includes(v) ? v : fallback);
  const mode = pick(a.mode, ["light", "dark", "system"], "system");
  const dark =
    mode === "dark" || (mode === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  const scopes = [
    ["font", pick(a.font, ["jakarta", "inter", "plex"], "jakarta"), "jakarta"],
    ["density", pick(a.density, ["compact", "standard", "roomy"], "standard"), "standard"],
    ["radius", pick(a.radius, ["square", "tight", "moderate", "soft"], "moderate"), "moderate"],
    ["accent", pick(a.accent, ["navy", "teal", "violet", "green", "amber", "neutral"], "navy"), "navy"],
    ["elevation", pick(a.elevation, ["flat", "bordered", "raised"], "bordered"), "bordered"],
  ];
  for (const [name, value, fallback] of scopes)
    if (value !== fallback) root.classList.add(`${name}-${value}`);
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
})();
