import { execSync } from 'node:child_process'
import type { IncomingMessage, ServerResponse } from 'node:http'
import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import type { PluginOption } from 'vite'
import { defineConfig } from 'vite'
import viteCompression from 'vite-plugin-compression'
import packageJson from './package.json' with { type: 'json' }

function getGitCommitHash(): string {
  // First check for COMMIT_SHA env var (used in Docker builds)
  const envHash = process.env.COMMIT_SHA?.trim()
  if (envHash) {
    return envHash.substring(0, 7)
  }

  // Fall back to git command (local development)
  return execSync('git rev-parse --short HEAD', { encoding: 'utf-8' }).trim()
}

// https://vite.dev/config/
export default defineConfig(() => {
  const appVersion = packageJson.version
  const commitHash = getGitCommitHash()
  const fastBuild = process.env.VITE_FAST_BUILD === 'true'
  const rollupInputs = {
    main: path.resolve(process.cwd(), 'index.html'),
  }

  const reactPlugin = react() as PluginOption
  const tailwindPlugin = tailwindcss() as PluginOption
  const gzipCompression = viteCompression({
    algorithm: 'gzip',
    ext: '.gz',
    filter: (file) => /\.(js|css|html|svg|json)$/i.test(file),
  }) as PluginOption
  const brotliCompression = viteCompression({
    algorithm: 'brotliCompress',
    ext: '.br',
    filter: (file) => /\.(js|css|html|svg|json)$/i.test(file),
  }) as PluginOption

  // Dev-only: redirect root page to gateway for proper token injection
  const gatewayRedirectPlugin: PluginOption = {
    name: 'gateway-redirect',
    configureServer(server) {
      if (process.env.NODE_ENV === 'production') {
        return
      }
      // Use GATEWAY_PORT from env (set by mise) or fall back to default
      // Note: GATEWAY_PORT should match mise.toml configuration
      const gatewayPort = process.env.GATEWAY_PORT || '8447'
      server.middlewares.use((req: IncomingMessage, res: ServerResponse, next: () => void) => {
        // Skip redirect for proxied requests from the gateway (prevents infinite redirect loop)
        // Node.js automatically lowercases header names
        if (req.headers['x-gateway-proxy'] === 'true') {
          next()
          return
        }
        // Redirect any app page to gateway for proper token injection
        // This handles /, /app, /app/, /app/rack, etc.
        if (req.url && (req.url === '/' || req.url.startsWith('/app'))) {
          res.writeHead(302, {
            Location: `http://localhost:${gatewayPort}${req.url}`,
          })
          res.end()
          return
        }
        next()
      })
    },
  }

  return {
    // Serve UI consistently under /app/ in all envs
    base: '/app/',
    define: {
      __APP_VERSION__: JSON.stringify(appVersion),
      __COMMIT_HASH__: JSON.stringify(commitHash),
    },
    plugins: [
      reactPlugin,
      tailwindPlugin,
      gzipCompression,
      brotliCompression,
      gatewayRedirectPlugin,
    ],
    build: {
      manifest: true,
      minify: false,
      sourcemap: true,
      ...(fastBuild
        ? {
            cssMinify: false,
            target: 'esnext',
            modulePreload: false,
          }
        : {}),
      rollupOptions: {
        input: rollupInputs,
      },
    },
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
        '/api': {
          target:
            process.env.VITE_API_BASE_URL ||
            `http://127.0.0.1:${process.env.GATEWAY_PORT || '8447'}`,
          changeOrigin: true,
          configure: (proxy, options) => {
            const debug = process.env.VITE_DEBUG_PROXY === 'true'
            if (!debug) return
            proxy.on('proxyReq', (_proxyReq, req) => {
              try {
                const target = (options as { target?: string } | undefined)?.target ?? '(unknown)'
                // biome-ignore lint/suspicious/noConsole: helpful during local proxy debugging
                console.log(`[vite-proxy] >> ${req.method} ${req.url} -> ${target}`)
              } catch {
                // Ignore logging errors
              }
            })
            proxy.on('proxyRes', (proxyRes, req) => {
              try {
                // biome-ignore lint/suspicious/noConsole: helpful during local proxy debugging
                console.log(`[vite-proxy] << ${req.method} ${req.url} ${proxyRes.statusCode}`)
              } catch {
                // Ignore logging errors
              }
            })
            proxy.on('error', (err, req) => {
              try {
                // biome-ignore lint/suspicious/noConsole: helpful during local proxy debugging
                console.error(`[vite-proxy] !! ${req.method} ${req.url} error: ${err.message}`)
              } catch {
                // Ignore logging errors
              }
            })
          },
        },
        '/apps': {
          target:
            process.env.VITE_API_BASE_URL ||
            `http://127.0.0.1:${process.env.GATEWAY_PORT || '8447'}`,
          changeOrigin: true,
        },
        '/instances': {
          target:
            process.env.VITE_API_BASE_URL ||
            `http://127.0.0.1:${process.env.GATEWAY_PORT || '8447'}`,
          changeOrigin: true,
        },
        '/system': {
          target:
            process.env.VITE_API_BASE_URL ||
            `http://127.0.0.1:${process.env.GATEWAY_PORT || '8447'}`,
          changeOrigin: true,
        },
      },
    },
  }
})
