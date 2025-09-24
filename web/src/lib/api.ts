import type { AxiosRequestConfig, AxiosResponse } from 'axios'

import { getConvoxGatewayAPI } from '@/api/generated'
import { gatewayAxios } from '@/api/http-client'
import type {
  DbAPIToken,
  DbAuditLog,
  DbUser,
  GetAdminRoles200,
  GetRack200,
  HandlersAuditLogsResponse,
  HandlersCreateAPITokenRequest,
  HandlersCreateAPITokenResponse,
  HandlersCreateUserRequest,
  HandlersCurrentUserResponse,
  HandlersHealthResponse,
  HandlersRevokeAllSessionsResponse,
  HandlersRevokeSessionResponse,
  HandlersTokenPermissionMetadata,
  HandlersUpdateAPITokenRequest,
  HandlersUpdateUserProfileRequest,
  HandlersUpdateUserRolesRequest,
  HandlersUserSessionResponse,
  HandlersUserSummary,
} from '@/api/schemas'

const API_PREFIX = '/.gateway/api'

const gateway = getConvoxGatewayAPI()

type GatewayResponse<T> = Promise<AxiosResponse<T>>

const unwrap = async <T>(promise: GatewayResponse<T>): Promise<T> => {
  const response = await promise
  return response.data
}

const normalizePath = (path: string): string => {
  const withoutPrefix = path.startsWith(API_PREFIX) ? path.slice(API_PREFIX.length) : path
  if (withoutPrefix === '' || withoutPrefix === '/') {
    return '/'
  }
  return withoutPrefix.startsWith('/') ? withoutPrefix : `/${withoutPrefix}`
}

export type GatewayUser = DbUser
export type UserConfig = { name: string; roles: string[] }
export type CreateUserRequest = HandlersCreateUserRequest
export type UpdateUserProfileRequest = HandlersUpdateUserProfileRequest
export type UpdateUserRolesRequest = HandlersUpdateUserRolesRequest
export type UserSummary = HandlersUserSummary
export type UserSessionSummary = HandlersUserSessionResponse
export type RevokeSessionResponse = HandlersRevokeSessionResponse
export type RevokeAllSessionsResponse = HandlersRevokeAllSessionsResponse
export type AuditLogsResponse = HandlersAuditLogsResponse
export type AuditLogEntry = DbAuditLog
export type TokenPermissionMetadata = HandlersTokenPermissionMetadata
export type CreateAPITokenRequest = HandlersCreateAPITokenRequest
export type CreateAPITokenResponse = HandlersCreateAPITokenResponse
export type APIToken = DbAPIToken
export type UpdateAPITokenRequest = HandlersUpdateAPITokenRequest
export type RackInfo = GetRack200
export type CurrentUserResponse = HandlersCurrentUserResponse
export type HealthResponse = HandlersHealthResponse

export const AVAILABLE_ROLES = {
  viewer: {
    name: 'viewer',
    description: 'Read-only access to apps, processes, and logs',
  },
  ops: {
    name: 'ops',
    description: 'Restart apps, view environments, manage processes',
  },
  deployer: {
    name: 'deployer',
    description: 'Full deployment permissions including env vars',
  },
  admin: {
    name: 'admin',
    description: 'Complete access to all operations',
  },
} as const

export type RoleName = keyof typeof AVAILABLE_ROLES

export const listUsers = (): Promise<GatewayUser[]> => unwrap(gateway.getAdminUsers())

export const getUser = (email: string): Promise<GatewayUser> =>
  unwrap(gateway.getAdminUsersEmail(email))

export const createUser = (payload: CreateUserRequest): Promise<UserSummary> =>
  unwrap(gateway.postAdminUsers(payload))

export const updateUserProfile = (
  currentEmail: string,
  payload: UpdateUserProfileRequest
): Promise<UserSummary> => unwrap(gateway.putAdminUsersEmail(currentEmail, payload))

export const updateUserRoles = (
  email: string,
  payload: UpdateUserRolesRequest
): Promise<UserSummary> => unwrap(gateway.putAdminUsersEmailRoles(email, payload))

export const deleteUser = async (email: string): Promise<void> => {
  await unwrap(gateway.deleteAdminUsersEmail(email))
}

export const listUserSessions = (email: string): Promise<UserSessionSummary[]> =>
  unwrap(gateway.getAdminUsersEmailSessions(email))

export const revokeUserSession = (
  email: string,
  sessionId: number
): Promise<RevokeSessionResponse> =>
  unwrap(gateway.postAdminUsersEmailSessionsSessionIDRevoke(email, sessionId))

export const revokeAllUserSessions = (email: string): Promise<RevokeAllSessionsResponse> =>
  unwrap(gateway.postAdminUsersEmailSessionsRevokeAll(email))

export type AuditLogQuery = Partial<{
  search: string
  action_type: string
  resource_type: string
  status: string
  page: number
  limit: number
  start: string
  end: string
  user: string
  range: string
  user_id: string
}>

export const listAuditLogs = (params: AuditLogQuery): Promise<AuditLogsResponse> =>
  unwrap(gateway.getAdminAudit(params))

export const exportAuditLogs = (
  params: Parameters<typeof gateway.getAdminAuditExport>[0]
): Promise<Blob> => unwrap(gateway.getAdminAuditExport(params))

export type RolesResponse = GetAdminRoles200

export const listRoles = (): Promise<RolesResponse> => unwrap(gateway.getAdminRoles())

export const getTokenPermissionMetadata = (): Promise<TokenPermissionMetadata> =>
  unwrap(gateway.getAdminTokensPermissions())

export const listAPITokens = (): Promise<APIToken[]> => unwrap(gateway.getAdminTokens())

export const getAPIToken = (tokenId: number): Promise<APIToken> =>
  unwrap(gateway.getAdminTokensTokenID(tokenId))

export const createAPIToken = (payload: CreateAPITokenRequest): Promise<CreateAPITokenResponse> =>
  unwrap(gateway.postAdminTokens(payload))

export const updateAPIToken = (
  tokenId: number,
  payload: UpdateAPITokenRequest
): Promise<APIToken> => unwrap(gateway.putAdminTokensTokenID(tokenId, payload))

export const deleteAPIToken = async (tokenId: number): Promise<void> => {
  await unwrap(gateway.deleteAdminTokensTokenID(tokenId))
}

export const getCurrentUser = (): Promise<CurrentUserResponse> => unwrap(gateway.getMe())

export const getHealth = (): Promise<HealthResponse> => unwrap(gateway.getHealth())

export const getRackInfo = (): Promise<RackInfo> => unwrap(gateway.getRack())
// Deprecated alias maintained for backward compatibility
export const getUserSessions = listUserSessions

export const getConvoxInstances = <T = unknown>(): Promise<T> =>
  gatewayAxios.get<T>('/convox/instances').then((res) => res.data)

export const getConvoxApps = <T = unknown>(): Promise<T> =>
  gatewayAxios.get<T>('/convox/apps').then((res) => res.data)

export const getConvoxApp = <T = unknown>(path: string): Promise<T> =>
  gatewayAxios.get<T>(normalizePath(path)).then((res) => res.data)

export const get = <T = unknown>(path: string, config?: AxiosRequestConfig): Promise<T> =>
  gatewayAxios.get<T>(normalizePath(path), config).then((res) => res.data)

export const post = <T = unknown>(
  path: string,
  data?: unknown,
  config?: AxiosRequestConfig
): Promise<T> => gatewayAxios.post<T>(normalizePath(path), data, config).then((res) => res.data)

export const put = <T = unknown>(
  path: string,
  data?: unknown,
  config?: AxiosRequestConfig
): Promise<T> => gatewayAxios.put<T>(normalizePath(path), data, config).then((res) => res.data)

export const destroy = <T = unknown>(path: string, config?: AxiosRequestConfig): Promise<T> =>
  gatewayAxios.delete<T>(normalizePath(path), config).then((res) => res.data)

export const api = {
  listUsers,
  getUser,
  createUser,
  updateUserProfile,
  updateUserRoles,
  deleteUser,
  listUserSessions,
  revokeUserSession,
  revokeAllUserSessions,
  getUserSessions: listUserSessions,
  listAuditLogs,
  exportAuditLogs,
  listRoles,
  getTokenPermissionMetadata,
  listAPITokens,
  getAPIToken,
  createAPIToken,
  updateAPIToken,
  deleteAPIToken,
  getCurrentUser,
  getHealth,
  getRackInfo,
  getConvoxInstances,
  getConvoxApps,
  getConvoxApp,
  get,
  post,
  put,
  delete: destroy,
}
