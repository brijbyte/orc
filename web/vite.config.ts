import { writeFileSync } from "node:fs";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// emptyOutDir wipes dist/.gitkeep, but a fresh clone needs it for
// go:embed — put it back after every build.
const keepGitkeep: Plugin = {
  name: "keep-gitkeep",
  closeBundle() {
    writeFileSync("../internal/web/dist/.gitkeep", "");
  },
};

// Build lands in internal/web/dist for go:embed. Dev proxies /api to a
// running `orc --serve`.
export default defineConfig({
  plugins: [react(), keepGitkeep],
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
    sourcemap: true,
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: "react-router",
              test: /node_modules[\\/]react-router/,
              priority: 2,
            },
            {
              name: "react",
              test: /node_modules[\\/](react|react-dom)/,
              priority: 2,
            },
            {
              // Keep CodeMirror language parsers in lazy chunks.
              name: "all-libs",
              test: /node_modules[\\/](?!@codemirror[\\/]|@lezer[\\/])/,
              priority: 1,
            },
          ],
        },
      },
    },
  },
  server: { proxy: { "/api": "http://127.0.0.1:7777" } },
});
