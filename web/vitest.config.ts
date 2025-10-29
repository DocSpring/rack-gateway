import path from 'node:path'
import { defineConfig, type UserConfigExport } from 'vitest/config'

const config: UserConfigExport = {
  resolve: {
    alias: {
      '@': path.resolve(process.cwd(), 'src'),
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    exclude: ['node_modules', 'dist', 'e2e'],
    hookTimeout: 10_000,
    testTimeout: 10_000,
    coverage: {
      reporter: ['text', 'json', 'html'],
    },
  },
}

export default defineConfig(config)
