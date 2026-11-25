import { writeFileSync } from "node:fs";
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
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
// IZCI_BACKEND points a second dev server at another izci instance.
const backend = process.env.IZCI_BACKEND || "http://localhost:5300";
const proxied = [
  "/izci.",
  "/grpc.",
  "/auth/",
  "/api/",
  "/mcp",
  "/healthz",
  "/readyz",
  "/llms.txt",
  "/llms-full.txt",
  "/docs/raw/",
  "/docs/index.json",
  "/integrations/",
  "/attachments/",
];

export default defineConfig({
  plugins: [
    // Must run before the React plugin: it generates src/routeTree.gen.ts and
    // splits every route into its own chunk.
    tanstackRouter({ target: "react", autoCodeSplitting: true, quoteStyle: "double", semicolons: true }),
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
    alias: [
      { find: "@", replacement: path.resolve(import.meta.dirname, "src") },
      // The API descriptors import validate.proto for field options only;
      // see src/lib/proto/validate-stub.ts.
      {
        find: /^(\.\.\/)+buf\/validate\/validate_pb(\.js)?$/,
        replacement: path.resolve(import.meta.dirname, "src/lib/proto/validate-stub.ts"),
      },
    ],
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
