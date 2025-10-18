import type { APIToken as APITokenModel } from '../../lib/generated/gateway-types'

export type APIToken = APITokenModel

export type TokenRoleInfo = {
  name: string
  label: string
  description: string
  permissions: string[]
}

export type TokenPermissionMetadata = {
  permissions: string[]
  roles: TokenRoleInfo[]
  default_permissions: string[]
  user_roles: string[]
  user_permissions: string[]
}

export type PermissionOption = {
  value: string
  title: string
  description: string
  sortKey: string
}

export type PermissionGroup = {
  key: string
  label: string
  sortKey: string
  options: PermissionOption[]
}

export const TOKEN_FORM_FIELDS = ['name', 'permissions'] as const
