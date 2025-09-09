#!/usr/bin/env node
const pad = (k) => (k + ':').padEnd(24, ' ')
const env = process.env

const WEB_PORT = env.WEB_PORT || '5173'
const EFFECTIVE_PORT = env.GATEWAY_PORT || env.WEB_PORT || '5173'
const GATEWAY_PORT = env.GATEWAY_PORT || '8447'
const MOCK_OAUTH_PORT = env.MOCK_OAUTH_PORT || '3345'
const VITE_API_BASE_URL = env.VITE_API_BASE_URL || ''
const VITE_DEBUG_PROXY = env.VITE_DEBUG_PROXY || 'false'

console.log('--- E2E Boot Config ---')
console.log(pad('CI'), String(!!env.CI))
console.log(pad('WEB_PORT'), WEB_PORT)
console.log(pad('GATEWAY_PORT'), GATEWAY_PORT)
console.log(pad('MOCK_OAUTH_PORT'), MOCK_OAUTH_PORT)
console.log(pad('VITE_API_BASE_URL'), VITE_API_BASE_URL || '(in-container) http://gateway-api:' + GATEWAY_PORT)
console.log(pad('VITE_DEBUG_PROXY'), VITE_DEBUG_PROXY)
console.log(pad('Playwright baseURL'), `http://localhost:${WEB_PORT}`)
console.log(pad('Effective baseURL'), `http://localhost:${EFFECTIVE_PORT}`)
console.log(pad('Gateway direct URL'), `http://localhost:${GATEWAY_PORT}`)
console.log(pad('Mock OAuth URL'), `http://localhost:${MOCK_OAUTH_PORT}`)
console.log('------------------------')
