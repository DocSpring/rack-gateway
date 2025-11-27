#!/usr/bin/env node
const pad = (k) => `${k}:`.padEnd(24, ' ')
const env = process.env

const parseList = (value) =>
  value
    ? value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
    : []

const shards = env.WEB_E2E_SHARDS || (env.CI ? '1' : '7')
const gatewayPortsList = parseList(
  env.E2E_GATEWAY_PORTS ||
    env.E2E_GATEWAY_PORT ||
    env.TEST_GATEWAY_PORT ||
    env.GATEWAY_PORT ||
    env.WEB_PORT
)
const firstGatewayPort = gatewayPortsList[0] || env.GATEWAY_PORT || '9447'
const webPort = env.WEB_PORT || firstGatewayPort || '5223'
const gatewayPort = firstGatewayPort || env.GATEWAY_PORT || '8447'
const MOCK_OAUTH_PORT = env.TEST_MOCK_OAUTH_PORT || env.MOCK_OAUTH_PORT || '3345'
const VITE_API_BASE_URL = env.VITE_API_BASE_URL || ''
const VITE_DEBUG_PROXY = env.VITE_DEBUG_PROXY || 'false'
const databaseUrlsList = parseList(env.E2E_DATABASE_URLS || env.E2E_DATABASE_URL)

console.log('--- E2E Boot Config ---')
console.log(pad('CI'), String(Boolean(env.CI)))
console.log(pad('WEB_E2E_SHARDS'), shards)
console.log(pad('Gateway ports'), gatewayPortsList.join(', ') || gatewayPort)
console.log(pad('WEB_PORT'), webPort)
console.log(pad('GATEWAY_PORT'), gatewayPort)
console.log(pad('MOCK_OAUTH_PORT'), MOCK_OAUTH_PORT)
console.log(
  pad('VITE_API_BASE_URL'),
  VITE_API_BASE_URL || `(in-container) http://gateway-api:${gatewayPort}`
)
console.log(pad('VITE_DEBUG_PROXY'), VITE_DEBUG_PROXY)
console.log(pad('Playwright baseURL'), `http://localhost:${webPort}`)
console.log(pad('Gateway direct URL'), `http://localhost:${gatewayPort}`)
console.log(pad('Mock OAuth URL'), `http://localhost:${MOCK_OAUTH_PORT}`)
if (databaseUrlsList.length > 0) {
  console.log(pad('Database URLs'), databaseUrlsList.join(', '))
}
console.log('------------------------')
