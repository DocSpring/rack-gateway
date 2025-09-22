import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import viteCompression from 'vite-plugin-compression'

// https://vite.dev/config/
export default defineConfig(() => ({
  // Serve UI consistently under /.gateway/web/ in all envs
  base: '/.gateway/web/',
  plugins: [
    react(),
    tailwindcss(),
    viteCompression({
      algorithm: 'gzip',
      ext: '.gz',
      filter: (file) => !(file.endsWith('.gz') || file.endsWith('.br')),
    }),
    viteCompression({
      algorithm: 'brotliCompress',
      ext: '.br',
      filter: (file) => !(file.endsWith('.gz') || file.endsWith('.br')),
    }),
  ],
  build:
    process.env.VITE_FAST_BUILD === 'true'
      ? {
          minify: false,
          cssMinify: false,
          sourcemap: false,
          target: 'esnext',
          modulePreload: false,
        }
      : undefined,
  resolve: {
    alias: {
      '@': path.resolve(process.cwd(), 'src'),
    },
  },
  server: {
    port: Number.parseInt(process.env.WEB_PORT || '5223', 10),
    strictPort: true,
    hmr: process.env.VITE_DISABLE_HMR === 'true' ? false : undefined,
    proxy: {
      '/.gateway/api': {
        target:
          process.env.VITE_API_BASE_URL || `http://127.0.0.1:${process.env.GATEWAY_PORT || '8447'}`,
        changeOrigin: true,
        configure: (proxy, options) => {
          const debug = process.env.VITE_DEBUG_PROXY === 'true'
          if (!debug) return
          proxy.on('proxyReq', (_proxyReq, req) => {
            try {
              const target = (options as { target?: string } | undefined)?.target ?? '(unknown)'
              // biome-ignore lint/suspicious/noConsole: helpful during local proxy debugging
              console.log(`[vite-proxy] >> ${req.method} ${req.url} -> ${target}`)
            } catch {}
          })
          proxy.on('proxyRes', (proxyRes, req) => {
            try {
              // biome-ignore lint/suspicious/noConsole: helpful during local proxy debugging
              console.log(`[vite-proxy] << ${req.method} ${req.url} ${proxyRes.statusCode}`)
            } catch {}
          })
          proxy.on('error', (err, req) => {
            try {
              // biome-ignore lint/suspicious/noConsole: helpful during local proxy debugging
              console.error(`[vite-proxy] !! ${req.method} ${req.url} error: ${err.message}`)
            } catch {}
          })
        },
      },
      '/apps': {
        target:
          process.env.VITE_API_BASE_URL || `http://127.0.0.1:${process.env.GATEWAY_PORT || '8447'}`,
        changeOrigin: true,
      },
      '/instances': {
        target:
          process.env.VITE_API_BASE_URL || `http://127.0.0.1:${process.env.GATEWAY_PORT || '8447'}`,
        changeOrigin: true,
      },
      '/system': {
        target:
          process.env.VITE_API_BASE_URL || `http://127.0.0.1:${process.env.GATEWAY_PORT || '8447'}`,
        changeOrigin: true,
      },
    },
  },
}))
