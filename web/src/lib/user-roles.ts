import type { RoleName } from './api'

const ROLE_PRIORITY: RoleName[] = ['admin', 'deployer', 'ops', 'viewer']

export function pickPrimaryRole(roles: string[] = []): RoleName {
  for (const role of ROLE_PRIORITY) {
    if (roles.includes(role)) {
      return role
    }
  }
  const first = roles[0]
  if (first && ROLE_PRIORITY.includes(first as RoleName)) {
    return first as RoleName
  }
  return 'viewer'
}

export { ROLE_PRIORITY }
