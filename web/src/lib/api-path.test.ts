import { describe, expect, it } from 'vitest'
import { apiPath, pathSegment } from './api-path'

describe('pathSegment', () => {
  it('encodes a value as exactly one path segment', () => {
    expect(pathSegment('viewer@example.com')).toBe('viewer%40example.com')
    expect(pathSegment('web/../../users')).toBe('web%2F..%2F..%2Fusers')
    expect(pathSegment('web?role=admin#x')).toBe('web%3Frole%3Dadmin%23x')
    expect(pathSegment(42)).toBe('42')
  })

  // The browser resolves "." and ".." segments however they're encoded.
  it('refuses values that would be read as dot segments, or no segment at all', () => {
    for (const value of ['', '.', '..']) {
      expect(() => pathSegment(value)).toThrow('Invalid API path segment')
    }
  })
})

describe('apiPath', () => {
  it('encodes every interpolated value', () => {
    const app = 'web/../users'
    const processId = 'p-1?x=1'
    expect(apiPath`/api/v1/convox/apps/${app}/processes/${processId}`).toBe(
      '/api/v1/convox/apps/web%2F..%2Fusers/processes/p-1%3Fx%3D1'
    )
    expect(apiPath`/api/v1/jobs/${7}/retry`).toBe('/api/v1/jobs/7/retry')
  })

  it('returns a path without values unchanged', () => {
    expect(apiPath`/api/v1/convox/apps`).toBe('/api/v1/convox/apps')
  })

  it('refuses a dot segment', () => {
    const app = '..'
    expect(() => apiPath`/api/v1/apps/${app}/settings`).toThrow('Invalid API path segment')
  })
})
