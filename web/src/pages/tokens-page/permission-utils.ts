import type { PermissionGroup, PermissionOption, TokenRoleInfo } from './types'

const WORD_DELIMITER_REGEX = /[-_\s]+/

export function normalizePermissions(perms: string[]): string[] {
  if (!perms || perms.length === 0) {
    return []
  }
  const unique = new Set(perms.map((p) => p.trim().toLowerCase()).filter(Boolean))
  return Array.from(unique).sort()
}

export function permissionsEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) {
    return false
  }
  return a.every((perm, idx) => perm === b[idx])
}

export function findMatchingRole(perms: string[], roles: TokenRoleInfo[]): string | null {
  for (const role of roles) {
    const rolePerms = normalizePermissions(role.permissions)
    if (permissionsEqual(perms, rolePerms)) {
      return role.name
    }
  }
  return null
}

export function buildPermissionGroups(permissions: string[]): PermissionGroup[] {
  const groups = new Map<string, PermissionGroup>()

  for (const permission of permissions) {
    const { groupKey, groupLabel, groupSortKey, actionLabel } = derivePermissionParts(permission)
    const option: PermissionOption = {
      value: permission,
      title: actionLabel,
      description: permission,
      sortKey: actionLabel,
    }

    const existing = groups.get(groupKey)
    if (existing) {
      existing.options.push(option)
    } else {
      groups.set(groupKey, {
        key: groupKey,
        label: groupLabel,
        sortKey: groupSortKey,
        options: [option],
      })
    }
  }

  return Array.from(groups.values())
    .map((group) => ({
      ...group,
      options: group.options.sort((a, b) =>
        a.sortKey.localeCompare(b.sortKey, undefined, {
          sensitivity: 'base',
        })
      ),
    }))
    .sort((a, b) => a.sortKey.localeCompare(b.sortKey, undefined, { sensitivity: 'base' }))
}

function derivePermissionParts(permission: string): {
  groupKey: string
  groupLabel: string
  groupSortKey: string
  actionLabel: string
} {
  if (!permission.includes(':')) {
    return {
      groupKey: 'other',
      groupLabel: 'Other',
      groupSortKey: 'other',
      actionLabel: permission,
    }
  }

  const segments = permission.split(':')
  const resourceRaw = segments[1] || 'other'
  const actionRaw = segments.slice(2).join(':') || '*'

  const groupLabel = humanizeGroup(resourceRaw)
  const groupSortKey = groupLabel.toLowerCase()
  const actionLabel = humanizeAction(actionRaw)

  return {
    groupKey: resourceRaw || 'other',
    groupLabel,
    groupSortKey,
    actionLabel,
  }
}

function humanizeGroup(value: string): string {
  if (!value || value === '*') {
    return 'All'
  }
  return value
    .split(WORD_DELIMITER_REGEX)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function humanizeAction(value: string): string {
  if (!value || value === '*') {
    return 'all'
  }
  return value.replace(/_/g, ' ')
}
