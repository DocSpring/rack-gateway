import { describe, expect, it } from 'vitest'
import { hasPermission, PERMISSIONS, permissionMatches } from './permissions'

const ADMIN = ['convox:*:*', 'gateway:*:*', 'security:*:*']
const VIEWER = ['convox:app:list', 'gateway:user:list', 'gateway:api_token:read']

describe('permissions', () => {
  it('matches exact permissions and per-segment wildcards', () => {
    expect(permissionMatches('gateway:user:list', 'gateway:user:list')).toBe(true)
    expect(permissionMatches('gateway:*:*', 'gateway:user:update')).toBe(true)
    expect(permissionMatches('gateway:user:*', 'gateway:user:update')).toBe(true)
    expect(permissionMatches('convox:*:*', 'gateway:user:update')).toBe(false)
    expect(permissionMatches('gateway:user', 'gateway:user:list')).toBe(false)
  })

  it('gives admins every gateway permission', () => {
    for (const permission of Object.values(PERMISSIONS)) {
      expect(hasPermission(ADMIN, permission)).toBe(true)
    }
  })

  it('limits viewers to the directory and their own tokens', () => {
    expect(hasPermission(VIEWER, PERMISSIONS.userList)).toBe(true)
    expect(hasPermission(VIEWER, PERMISSIONS.apiTokenRead)).toBe(true)
    expect(hasPermission(VIEWER, PERMISSIONS.userRead)).toBe(false)
    expect(hasPermission(VIEWER, PERMISSIONS.auditLogRead)).toBe(false)
    expect(hasPermission(VIEWER, PERMISSIONS.apiTokenCreate)).toBe(false)
    expect(hasPermission(undefined, PERMISSIONS.userList)).toBe(false)
  })
})
