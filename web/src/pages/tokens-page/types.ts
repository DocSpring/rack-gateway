import type { APIToken as APITokenModel } from '../../lib/generated/gateway-types'

export type APIToken = APITokenModel

export interface TokenRoleInfo {
  name: string
  label: string
  description: string
  permissions: string[]
}

export interface TokenPermissionMetadata {
  permissions: string[]
  roles: TokenRoleInfo[]
  default_permissions: string[]
  user_roles: string[]
  user_permissions: string[]
}

export interface PermissionOption {
  value: string
  title: string
  description: string
  sortKey: string
}

export interface PermissionGroup {
  key: string
  label: string
  sortKey: string
  options: PermissionOption[]
}
