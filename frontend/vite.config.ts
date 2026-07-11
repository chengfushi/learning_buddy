import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // 开发时代理后端，避免 CORS；生产由网关统一
      "/api": { target: process.env.VITE_API_BASE || "http://localhost:8080", changeOrigin: true },
    },
  },
});
