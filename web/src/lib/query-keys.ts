/**
 * Centralized query keys for React Query.
 * Using consistent keys across the app ensures cache invalidation works correctly.
 */

export const QUERY_KEYS = {
  MFA_STATUS: ['mfa-status'],
  USER: ['user'] as const,
  USER_SESSIONS: ['userSessions'] as const,
  USER_AUDIT_LOGS: ['userAuditLogs'] as const,
  USERS: ['users'] as const,
  TOKENS: ['tokens'] as const,
  APP_ENV: ['app-env'] as const,
  APP_PROCESSES: ['app-processes'] as const,
  APP_SERVICES: ['app-services'] as const,
  SLACK_INTEGRATION: ['slack-integration'] as const,
  DEPLOY_APPROVAL_REQUEST: ['deploy-approval-request'] as const,
  DEPLOY_APPROVAL_REQUESTS: ['deploy-approval-requests'] as const,
} as const
