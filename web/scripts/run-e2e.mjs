#!/usr/bin/env node
import { spawn } from 'node:child_process'

const env = process.env

const toInt = (value, fallback) => {
  if (value === undefined || value === null || value === '') {
    return fallback
  }
  const parsed = Number.parseInt(String(value), 10)
  return Number.isNaN(parsed) ? fallback : parsed
}

const parseList = (value) =>
  value
    ? value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
    : []

const inferShards = () => {
  if (env.WEB_E2E_SHARDS) {
    return toInt(env.WEB_E2E_SHARDS, env.CI ? 1 : 7)
  }
  if (env.CI) {
    return 1
  }
  return 7
}

const shards = Math.max(1, inferShards())
env.WEB_E2E_SHARDS = String(shards)

if (!env.PLAYWRIGHT_WORKERS) {
  env.PLAYWRIGHT_WORKERS = String(shards)
}

const defaultGatewayPort = toInt(
  env.TEST_GATEWAY_PORT || env.E2E_GATEWAY_PORT || env.GATEWAY_PORT || env.WEB_PORT,
  9447
)

let gatewayPorts = parseList(env.E2E_GATEWAY_PORTS)
if (gatewayPorts.length === 0) {
  gatewayPorts = Array.from({ length: shards }, (_, index) => String(defaultGatewayPort + index))
}
env.E2E_GATEWAY_PORTS = gatewayPorts.join(',')
env.E2E_GATEWAY_PORT = gatewayPorts[0] || String(defaultGatewayPort)
env.GATEWAY_PORT = env.E2E_GATEWAY_PORT
env.WEB_PORT = env.WEB_PORT || env.E2E_GATEWAY_PORT

const baseDatabaseUrl =
  env.E2E_DATABASE_URL ||
  env.TEST_DATABASE_URL ||
  env.DATABASE_URL ||
  'postgres://postgres:postgres@127.0.0.1:55432/gateway_test?sslmode=disable'

const baseUrl = new URL(baseDatabaseUrl)
const buildDatabaseUrl = (databaseName) => {
  const url = new URL(baseUrl.toString())
  url.pathname = `/${databaseName}`
  return url.toString()
}

const databaseNames = Array.from({ length: shards }, (_, index) =>
  index === 0 ? 'gateway_test' : `gateway_test_${index + 1}`
)

let databaseUrls = parseList(env.E2E_DATABASE_URLS)
if (databaseUrls.length === 0) {
  databaseUrls = databaseNames.map((name) => buildDatabaseUrl(name))
}
env.E2E_DATABASE_URLS = databaseUrls.join(',')
env.E2E_DATABASE_URL = databaseUrls[0]
env.TEST_DATABASE_URL = databaseUrls[0]
env.DATABASE_URL = databaseUrls[0]

await import('./print-e2e-env.mjs')

// Forward any CLI arguments passed to this script to Playwright
const playwrightArgs = process.argv.slice(2)

await new Promise((resolve, reject) => {
  const child = spawn('pnpm', ['exec', 'playwright', 'test', ...playwrightArgs], {
    stdio: 'inherit',
    env,
    shell: process.platform === 'win32',
  })

  child.on('exit', (code, signal) => {
    if (typeof code === 'number') {
      process.exitCode = code
      return resolve()
    }
    const error = new Error(`Playwright exited with signal ${signal ?? 'unknown'}`)
    reject(error)
  })

  child.on('error', (error) => {
    reject(error)
  })
}).catch((error) => {
  console.error('[web:e2e] Failed to launch Playwright:', error)
  process.exitCode = 1
})
