import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Build lands in internal/web/dist for go:embed. Dev proxies /api to a
// running `orc --serve`.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "../internal/web/dist", emptyOutDir: true },
  server: { proxy: { "/api": "http://127.0.0.1:7777" } },
});
