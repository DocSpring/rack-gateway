import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// https://vite.dev/config/
export default defineConfig(() => ({
  // Serve UI consistently under /.gateway/web/ in all envs
  base: "/.gateway/web/",
  plugins: [react(), tailwindcss()],
  build: process.env.VITE_FAST_BUILD === "true" ? {
    minify: false,
    cssMinify: false,
    sourcemap: false,
    target: "esnext",
    modulePreload: false,
  } : undefined,
  resolve: {
    alias: {
      "@": path.resolve(process.cwd(), "src"),
    },
  },
  server: {
    port: parseInt(process.env.WEB_PORT || "5173"),
    hmr: process.env.VITE_DISABLE_HMR === "true" ? false : undefined,
    proxy: {
      "/.gateway/api": {
        target:
          process.env.VITE_API_BASE_URL ||
          `http://127.0.0.1:${process.env.GATEWAY_PORT || "8447"}`,
        changeOrigin: true,
        configure: (proxy, options) => {
          const debug = process.env.VITE_DEBUG_PROXY === "true";
          if (!debug) return;
          proxy.on("proxyReq", (_proxyReq, req) => {
            try {
              // @ts-ignore
              const target = options?.target || "(unknown)";
              console.log(`[vite-proxy] >> ${req.method} ${req.url} -> ${target}`);
            } catch {}
          });
          proxy.on("proxyRes", (proxyRes, req) => {
            try {
              console.log(`[vite-proxy] << ${req.method} ${req.url} ${proxyRes.statusCode}`);
            } catch {}
          });
          proxy.on("error", (err, req) => {
            try {
              console.error(`[vite-proxy] !! ${req.method} ${req.url} error: ${err.message}`);
            } catch {}
          });
        },
      },
    },
  },
}));
