import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Move regex to top level as per ultracite rules
const API_REGEX = /^\/api/

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(process.cwd(), 'src'),
    },
  },
  server: {
    port: parseInt(process.env.WEB_PORT || '5173'),
    proxy: {
      '/api': {
        target: `http://localhost:${process.env.GATEWAY_PORT || '8447'}`,
        changeOrigin: true,
        rewrite: (pathStr) => pathStr.replace(API_REGEX, ''),
      },
      '/.gateway': {
        target: `http://localhost:${process.env.GATEWAY_PORT || '8447'}`,
        changeOrigin: true,
      },
    },
  },
})
