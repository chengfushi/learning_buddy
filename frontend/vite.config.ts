import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  test: {
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.d.ts", "src/**/*.test.{ts,tsx}"],
      reporter: ["text", "json-summary"],
      thresholds: {
        statements: 70,
        branches: 70,
        functions: 70,
        lines: 70,
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      // 开发时代理后端，避免 CORS；生产由网关统一
      "/api": { target: process.env.VITE_API_BASE || "http://localhost:8080", changeOrigin: true },
    },
  },
});
