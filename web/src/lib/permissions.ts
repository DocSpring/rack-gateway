// Permission checks for showing or hiding UI. The gateway enforces every request; these only keep
// people from seeing controls they can't use. Permissions are `scope:resource:action`, and `*`
// matches any one segment, as in the gateway's API token checks.

export const PERMISSIONS = {
  userList: 'gateway:user:list',
  userRead: 'gateway:user:read',
  userCreate: 'gateway:user:create',
  userUpdate: 'gateway:user:update',
  userDelete: 'gateway:user:delete',
  auditLogRead: 'gateway:audit_log:read',
  apiTokenRead: 'gateway:api_token:read',
  apiTokenCreate: 'gateway:api_token:create',
  apiTokenUpdate: 'gateway:api_token:update',
  apiTokenDelete: 'gateway:api_token:delete',
  apiTokenManage: 'gateway:api_token:manage',
  deployApprovalList: 'gateway:deploy_approval_request:list',
} as const

export function permissionMatches(granted: string, wanted: string): boolean {
  if (granted === wanted) {
    return true
  }
  const grantedParts = granted.split(':')
  const wantedParts = wanted.split(':')
  if (grantedParts.length !== 3 || wantedParts.length !== 3) {
    return false
  }
  return grantedParts.every((part, i) => part === '*' || part === wantedParts[i])
}

export function hasPermission(permissions: readonly string[] | undefined, wanted: string): boolean {
  return (permissions ?? []).some((granted) => permissionMatches(granted, wanted))
}
