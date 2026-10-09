import { describe, expect, it, vi } from 'vitest'
import { buildNavigationItems } from './navigation-items'

const noop = vi.fn()

function names(user: Parameters<typeof buildNavigationItems>[0]) {
  return buildNavigationItems(user, noop).map((item) => item.name)
}

describe('buildNavigationItems', () => {
  it('shows a viewer the team directory, their tokens and their own activity', () => {
    const items = buildNavigationItems(
      {
        email: 'viewer@example.com',
        roles: ['viewer'],
        permissions: ['convox:app:list', 'gateway:user:list', 'gateway:api_token:read'],
      },
      noop
    )
    const itemNames = items.map((item) => item.name)
    expect(itemNames).toContain('Users')
    expect(itemNames).toContain('API Tokens')
    expect(itemNames).toContain('My Activity')
    expect(itemNames).not.toContain('Audit Logs')
    expect(itemNames).not.toContain('Deploy Approvals')
    expect(itemNames).not.toContain('Settings')
    expect(items.find((item) => item.name === 'My Activity')?.href).toBe(
      '/users/viewer%40example.com/audit-logs'
    )
  })

  it('shows admins the full audit log and admin pages', () => {
    const itemNames = names({
      email: 'admin@example.com',
      roles: ['admin'],
      permissions: ['convox:*:*', 'gateway:*:*', 'security:*:*'],
    })
    expect(itemNames).toEqual(
      expect.arrayContaining([
        'Users',
        'API Tokens',
        'Deploy Approvals',
        'Audit Logs',
        'Integrations',
        'Settings',
        'Background Jobs',
      ])
    )
    expect(itemNames).not.toContain('My Activity')
  })

  it('hides permission-gated pages when permissions are missing', () => {
    const itemNames = names({ email: 'someone@example.com', roles: [] })
    expect(itemNames).not.toContain('Users')
    expect(itemNames).not.toContain('API Tokens')
    expect(itemNames).toContain('Account Security')
  })
})
