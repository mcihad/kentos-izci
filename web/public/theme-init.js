// Runs synchronously in <head> so the saved theme is applied before first paint.
(() => {
  const root = document.documentElement;
  let dark = true;
  try {
    const stored = localStorage.getItem("izci-theme");
    dark =
      stored === "dark" || (stored !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  } catch {
    // Storage may be unavailable (private mode); fall back to dark.
  }
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
})();
