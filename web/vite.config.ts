import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// https://vite.dev/config/
export default defineConfig(() => ({
  // Serve UI consistently under /.gateway/web/ in all envs
  base: "/.gateway/web/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(process.cwd(), "src"),
    },
  },
  server: {
    port: parseInt(process.env.WEB_PORT || "5173"),
    proxy: {
      "/.gateway/api": {
        target:
          process.env.VITE_API_BASE_URL ||
          `http://127.0.0.1:${process.env.GATEWAY_PORT || "8080"}`,
        changeOrigin: true,
      },
    },
  },
}));
