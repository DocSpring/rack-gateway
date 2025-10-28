import type { RoleName, UpdateUserRequest } from '@/lib/api'

export type User = {
  id?: number
  email: string
  name: string
  roles: string[]
  created_at: string
  updated_at: string
  suspended: boolean
  created_by_email?: string
  created_by_name?: string
  locked_at?: string
  locked_reason?: string
  locked_by_user_id?: number
}

// CI/CD role is intentionally omitted here; it is reserved for automation tokens only.
export const AVAILABLE_ROLES = {
  viewer: {
    label: 'Viewer',
    description: 'Read-only access to apps and logs',
    className: 'bg-zinc-600 text-white',
  },
  ops: {
    label: 'Operations',
    description: 'Can manage processes and access systems',
    className: 'bg-green-600 text-white',
  },
  deployer: {
    label: 'Deployer',
    description: 'Can deploy apps and manage configurations',
    className: 'bg-blue-600 text-white',
  },
  admin: {
    label: 'Administrator',
    description: 'Full access to all resources',
    className: 'bg-purple-600 text-white',
  },
} as const

export function isUserLocked(user: User): boolean {
  return Boolean(user.locked_at)
}

export function canModifyUser(user: User, currentUserEmail?: string): boolean {
  return user.email !== currentUserEmail
}

type UserUpdatePlan =
  | { type: 'none' }
  | { type: 'nameOnly'; name: string }
  | { type: 'full'; payload: UpdateUserRequest }

export function determineUserUpdatePlan(
  original: User,
  values: { email: string; name: string; role: RoleName }
): UserUpdatePlan {
  const desiredRoles: RoleName[] = [values.role]
  const changedEmail = values.email !== original.email
  const changedName = values.name !== original.name
  const rolesChanged =
    original.roles.length !== desiredRoles.length ||
    desiredRoles.some((role) => !original.roles.includes(role))

  if (!changedEmail && changedName && !rolesChanged) {
    return { type: 'nameOnly', name: values.name }
  }

  if (!(changedEmail || changedName || rolesChanged)) {
    return { type: 'none' }
  }

  const payload: UpdateUserRequest = {}
  if (changedEmail) {
    payload.email = values.email
  }
  if (changedName) {
    payload.name = values.name
  }
  if (rolesChanged) {
    payload.roles = desiredRoles
  }

  return { type: 'full', payload }
}
