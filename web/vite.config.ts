import { writeFileSync } from "node:fs";
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";
import { compression } from "vite-plugin-compression2";

// The Go binary embeds web/dist with `//go:embed all:dist`, and a tracked
// dist/.gitkeep keeps that pattern valid on fresh clones. Vite empties dist on
// every build, so recreate the marker afterwards.
function keepGitkeep(): Plugin {
  return {
    name: "izci:keep-gitkeep",
    apply: "build",
    closeBundle() {
      writeFileSync(path.resolve(import.meta.dirname, "dist/.gitkeep"), "");
    },
  };
}

// Paths served by the Go server during development (Vite runs on :5301).
const backend = "http://localhost:5300";
const proxied = ["/izci.", "/auth/", "/api/", "/mcp", "/healthz", "/readyz", "/llms.txt", "/llms-full.txt"];

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    compression({
      algorithms: ["brotliCompress", "gzip"],
      exclude: [/\.(br|gz)$/, /\.gitkeep$/],
      threshold: 1024,
    }),
    keepGitkeep(),
  ],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "src") },
  },
  server: {
    port: 5301,
    strictPort: true,
    proxy: Object.fromEntries(proxied.map((p) => [p, { target: backend, changeOrigin: false }])),
  },
  build: {
    target: "es2022",
    sourcemap: false,
    chunkSizeWarningLimit: 800,
  },
});
