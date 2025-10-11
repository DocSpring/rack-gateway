import type { AxiosRequestConfig, AxiosResponse } from 'axios'

import { getRackGatewayAPI } from '@/api/generated'
import { gatewayAxios } from '@/api/http-client'
import type {
  DbAPIToken,
  DbAuditLog,
  DbUser,
  GetAdminDeployApprovalRequestsParams,
  GetAdminRoles200,
  GetRack200,
  HandlersAuditLogsResponse,
  HandlersBackupCodesResponse,
  HandlersConfirmTOTPEnrollmentRequest,
  HandlersConfirmWebAuthnEnrollmentRequest,
  HandlersCreateAPITokenRequest,
  HandlersCreateAPITokenResponse,
  HandlersCreateUserRequest,
  HandlersDeployApprovalRequestList,
  HandlersDeployApprovalRequestResponse,
  HandlersHealthResponse,
  HandlersInfoResponse,
  HandlersMFAMethodResponse,
  HandlersMFAStatusResponse,
  HandlersRevokeAllSessionsResponse,
  HandlersRevokeSessionResponse,
  HandlersStartTOTPEnrollmentResponse,
  HandlersStartWebAuthnEnrollmentResponse,
  HandlersStartYubiOTPEnrollmentRequest,
  HandlersStartYubiOTPEnrollmentResponse,
  HandlersStatusResponse,
  HandlersTokenPermissionMetadata,
  HandlersTrustedDeviceResponse,
  HandlersUpdateAPITokenRequest,
  HandlersUpdateDeployApprovalRequestStatusRequest,
  HandlersUpdatePreferredMFAMethodRequest,
  HandlersUpdateUserProfileRequest,
  HandlersUpdateUserRolesRequest,
  HandlersUserSessionResponse,
  HandlersUserSummary,
  HandlersVerifyMFARequest,
  HandlersVerifyMFAResponse,
  HandlersVerifyWebAuthnAssertionRequest,
  HandlersWebAuthnAssertionStartResponse,
  HandlersWebAuthnEnrollmentResponse,
} from '@/api/schemas'

const API_PREFIX = '/.gateway/api'

const gateway = getRackGatewayAPI()

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
export type InfoResponse = HandlersInfoResponse
export type HealthResponse = HandlersHealthResponse
export type EnvValuesMap = Record<string, string>
export type StartTOTPEnrollmentResponse = HandlersStartTOTPEnrollmentResponse
export type ConfirmTOTPEnrollmentRequest = HandlersConfirmTOTPEnrollmentRequest
export type VerifyMFARequest = HandlersVerifyMFARequest
export type VerifyMFAResponse = HandlersVerifyMFAResponse
export type BackupCodesResponse = HandlersBackupCodesResponse
export type MFAStatusResponse = HandlersMFAStatusResponse
export type MFAMethod = HandlersMFAMethodResponse
export type TrustedDevice = HandlersTrustedDeviceResponse
export type StartYubiOTPEnrollmentRequest = HandlersStartYubiOTPEnrollmentRequest
export type StartYubiOTPEnrollmentResponse = HandlersStartYubiOTPEnrollmentResponse
export type StartWebAuthnEnrollmentResponse = HandlersStartWebAuthnEnrollmentResponse
export type ConfirmWebAuthnEnrollmentRequest = HandlersConfirmWebAuthnEnrollmentRequest
export type StatusResponse = HandlersStatusResponse
export type DeployApprovalRequest = HandlersDeployApprovalRequestResponse
export type DeployApprovalRequestList = HandlersDeployApprovalRequestList
export type UpdateDeployApprovalRequestStatusRequest =
  HandlersUpdateDeployApprovalRequestStatusRequest

type EnvValuesResponseShape = {
  env?: EnvValuesMap
}

type UpdateEnvResponseShape = {
  env?: EnvValuesMap
  release_id?: string
}

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

export const lockUser = (email: string, reason: string): Promise<StatusResponse> =>
  unwrap(gateway.postAdminUsersEmailLock(email, { reason })) as Promise<StatusResponse>

export const unlockUser = (email: string): Promise<StatusResponse> =>
  unwrap(gateway.postAdminUsersEmailUnlock(email)) as Promise<StatusResponse>

export const listDeployApprovalRequests = (
  params?: GetAdminDeployApprovalRequestsParams
): Promise<DeployApprovalRequestList> => unwrap(gateway.getAdminDeployApprovalRequests(params))

export const approveDeployApprovalRequest = (
  id: string,
  payload?: UpdateDeployApprovalRequestStatusRequest
): Promise<DeployApprovalRequest> =>
  unwrap(gateway.postAdminDeployApprovalRequestsIdApprove(id, payload ?? {}))

export const rejectDeployApprovalRequest = (
  id: string,
  payload?: UpdateDeployApprovalRequestStatusRequest
): Promise<DeployApprovalRequest> =>
  unwrap(gateway.postAdminDeployApprovalRequestsIdReject(id, payload ?? {}))

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

export const getAPIToken = (tokenPublicId: string): Promise<APIToken> =>
  unwrap(gateway.getAdminTokensTokenID(tokenPublicId))

export const createAPIToken = (payload: CreateAPITokenRequest): Promise<CreateAPITokenResponse> =>
  unwrap(gateway.postAdminTokens(payload))

export const updateAPIToken = (
  tokenPublicId: string,
  payload: UpdateAPITokenRequest
): Promise<APIToken> => unwrap(gateway.putAdminTokensTokenID(tokenPublicId, payload))

export const deleteAPIToken = async (tokenPublicId: string): Promise<void> => {
  await unwrap(gateway.deleteAdminTokensTokenID(tokenPublicId))
}

export const getInfo = (): Promise<InfoResponse> => unwrap(gateway.getInfo())

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

export const getMFAStatus = (): Promise<MFAStatusResponse> => unwrap(gateway.getAuthMfaStatus())

export const deleteMFAMethod = (methodId: number): Promise<StatusResponse> =>
  unwrap(gateway.deleteAuthMfaMethodsMethodID(methodId))

export const updateMFAMethod = (
  methodId: number,
  data: { label: string }
): Promise<StatusResponse> => unwrap(gateway.putAuthMfaMethodsMethodID(methodId, data))

export const revokeTrustedDevice = (deviceId: number): Promise<StatusResponse> =>
  unwrap(gateway.deleteAuthMfaTrustedDevicesDeviceID(deviceId))

export const startTOTPEnrollment = (): Promise<StartTOTPEnrollmentResponse> =>
  post<StartTOTPEnrollmentResponse>('/auth/mfa/enroll/totp/start')

export const confirmTOTPEnrollment = (
  payload: ConfirmTOTPEnrollmentRequest
): Promise<VerifyMFAResponse> => post<VerifyMFAResponse>('/auth/mfa/enroll/totp/confirm', payload)

export const startYubiOTPEnrollment = (
  payload: HandlersStartYubiOTPEnrollmentRequest
): Promise<HandlersStartYubiOTPEnrollmentResponse> =>
  post<HandlersStartYubiOTPEnrollmentResponse>('/auth/mfa/enroll/yubiotp/start', payload)

export const startWebAuthnEnrollment = (): Promise<HandlersStartWebAuthnEnrollmentResponse> =>
  post<HandlersStartWebAuthnEnrollmentResponse>('/auth/mfa/enroll/webauthn/start')

export const confirmWebAuthnEnrollment = (
  payload: HandlersConfirmWebAuthnEnrollmentRequest
): Promise<HandlersWebAuthnEnrollmentResponse> =>
  post<HandlersWebAuthnEnrollmentResponse>('/auth/mfa/enroll/webauthn/confirm', payload)

export const verifyCliMfa = (payload: {
  state: string
  method?: string
  code?: string
  session_data?: string
  assertion_response?: string
}): Promise<{ redirect: string }> =>
  gatewayAxios
    .post<{ redirect: string }>('/auth/cli/mfa', payload, {
      headers: { 'Content-Type': 'application/json' },
    })
    .then((res) => res.data)

export const verifyMFA = (payload: VerifyMFARequest): Promise<VerifyMFAResponse> =>
  post<VerifyMFAResponse>('/auth/mfa/verify', payload)

export const startWebAuthnAssertion = (): Promise<HandlersWebAuthnAssertionStartResponse> =>
  post<HandlersWebAuthnAssertionStartResponse>('/auth/mfa/webauthn/assertion/start')

export const verifyWebAuthnAssertion = (
  payload: HandlersVerifyWebAuthnAssertionRequest
): Promise<HandlersVerifyMFAResponse> =>
  post<HandlersVerifyMFAResponse>('/auth/mfa/webauthn/assertion/verify', payload)

export const regenerateBackupCodes = (): Promise<BackupCodesResponse> =>
  post<BackupCodesResponse>('/auth/mfa/backup-codes/regenerate')

export const updatePreferredMFAMethod = (
  payload: HandlersUpdatePreferredMFAMethodRequest
): Promise<HandlersStatusResponse> =>
  gatewayAxios
    .put<HandlersStatusResponse>('/auth/mfa/preferred-method', payload)
    .then((res) => res.data)

export const post = <T = unknown>(
  path: string,
  data?: unknown,
  config?: AxiosRequestConfig
): Promise<T> => gatewayAxios.post<T>(normalizePath(path), data, config).then((res) => res.data)

export const put = <T = unknown>(
  path: string,
  data?: unknown,
  config?: AxiosRequestConfig
): Promise<T> => {
  // Handle primitive JSON values (null, false, true, numbers, strings) by sending as JSON strings
  // This avoids axios treating falsy values as "no data"
  if (
    data === null ||
    data === false ||
    data === true ||
    typeof data === 'number' ||
    typeof data === 'string'
  ) {
    return gatewayAxios
      .request<T>({
        method: 'PUT',
        url: normalizePath(path),
        data: JSON.stringify(data),
        headers: {
          'Content-Type': 'application/json',
          ...config?.headers,
        },
        transformRequest: [], // Don't transform the data, send raw JSON string
        ...config,
      })
      .then((res) => res.data)
  }
  return gatewayAxios.put<T>(normalizePath(path), data, config).then((res) => res.data)
}

export const destroy = <T = unknown>(path: string, config?: AxiosRequestConfig): Promise<T> =>
  gatewayAxios.delete<T>(normalizePath(path), config).then((res) => res.data)

export async function fetchAppEnv(app: string): Promise<EnvValuesMap> {
  const response = await gatewayAxios.get<EnvValuesResponseShape>('env', {
    params: { app },
  })
  return response.data.env ?? {}
}

export async function fetchAppEnvValue(
  app: string,
  key: string,
  includeSecret = false
): Promise<string | null> {
  const response = await gatewayAxios.get<EnvValuesResponseShape>('env', {
    params: {
      app,
      key,
      secrets: includeSecret ? 'true' : undefined,
    },
  })
  const env = response.data.env ?? {}
  if (Object.hasOwn(env, key)) {
    return env[key]
  }
  return null
}

export async function updateAppEnv(
  app: string,
  set: Record<string, string>,
  remove: string[]
): Promise<UpdateEnvResponseShape> {
  const response = await gatewayAxios.put<UpdateEnvResponseShape>('env', {
    app,
    set,
    remove,
  })
  return response.data
}

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
  lockUser,
  unlockUser,
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
  getInfo,
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
