import type { AxiosError } from 'axios'
import axios from 'axios'
import type { HandlersInfoResponse, HandlersRackSummary } from '@/api/schemas'
import { getCsrfToken } from './csrf'
import { APIRoute } from './routes'

export const SESSION_EXPIRED_MESSAGE = 'Session expired. Please sign in again.'
export const LOGOUT_FAILED_MESSAGE =
  "Couldn't sign out: your session is still active. Please try again."

export interface User {
  email: string
  name: string
  roles: string[]
  // Effective permissions from the user's roles; used only to hide UI the user can't use.
  permissions: string[]
  rack?: { name: string; alias?: string; host: string }
  mfa_enrolled?: boolean
  mfa_required?: boolean
  preferred_mfa_method?: string | null
  recent_step_up_expires_at?: string | null
  has_trusted_device?: boolean
  // The session still owes its login MFA challenge (see GET /api/v1/info)
  mfa_pending?: boolean
  integrations: {
    slack: boolean
    github: boolean
    circleci: boolean
  }
}

class AuthService {
  // (PKCE helpers removed; web flow uses server-side OAuth)

  // Start OAuth flow for web (no PKCE needed). The gateway handles the identity provider's callback.
  startLogin(returnTo?: string): void {
    // Build login URL with optional returnTo parameter
    const loginURL = new URL(APIRoute('auth/web/login'), window.location.origin)
    if (returnTo) {
      loginURL.searchParams.set('returnTo', returnTo)
    }
    // Redirect directly to web login endpoint
    window.location.href = loginURL.toString()
  }

  // Get current user (cookie-based auth; no JS access to HttpOnly cookie needed)
  async getCurrentUser(options: { suppressAuthError?: boolean } = {}): Promise<User | null> {
    try {
      const response = await axios.get<HandlersInfoResponse>(APIRoute('info'), {
        withCredentials: true,
      })
      const { data } = response
      const userInfo = data?.user

      const rack: User['rack'] = normalizeRack(data?.rack)
      const roles = Array.isArray(userInfo?.roles) ? userInfo.roles : []
      const permissions = Array.isArray(userInfo?.permissions) ? userInfo.permissions : []

      const mapped: User = {
        email: userInfo?.email ?? '',
        name: userInfo?.name ?? '',
        roles,
        permissions,
        rack,
        mfa_enrolled: Boolean(userInfo?.mfa_enrolled),
        mfa_required: Boolean(userInfo?.mfa_required),
        preferred_mfa_method: userInfo?.preferred_mfa_method ?? null,
        recent_step_up_expires_at: userInfo?.recent_step_up_expires_at ?? null,
        has_trusted_device: Boolean(userInfo?.has_trusted_device),
        mfa_pending: Boolean(userInfo?.mfa_pending),
        integrations: {
          slack: Boolean(data?.integrations?.slack),
          github: Boolean(data?.integrations?.github),
          circleci: Boolean(data?.integrations?.circleci),
        },
      }

      return mapped
    } catch (err: unknown) {
      // Mark for UI to show an error after redirect to login
      const status = (err as AxiosError)?.response?.status
      try {
        if (status === 401 && !options.suppressAuthError) {
          sessionStorage.setItem('auth_error', SESSION_EXPIRED_MESSAGE)
        }
      } catch {
        /* ignore */
      }
      return null
    }
  }

  // Logout revokes the session on the server (which clears the HttpOnly cookie), then goes to the login page. It
  // resolves false, staying on the page, when the server didn't confirm that the session was revoked.
  async logout(): Promise<boolean> {
    const headers: Record<string, string> = {}
    const csrfToken = getCsrfToken()
    if (csrfToken) {
      headers['X-CSRF-Token'] = csrfToken
    }
    try {
      const response = await fetch(APIRoute('auth/web/logout'), {
        method: 'POST',
        credentials: 'include',
        headers,
      })
      if (!response.ok) {
        return false
      }
    } catch {
      return false
    }
    const base = import.meta.env.BASE_URL || '/'
    // Use assign to ease testing under jsdom and avoid Location href setter issues
    window.location.assign(`${base}login`)
    return true
  }
}

function normalizeRack(summary?: HandlersRackSummary | null): User['rack'] {
  if (!summary) {
    return
  }
  const name = summary.name?.trim() ?? ''
  const aliasValue = summary.alias?.trim() ?? ''
  const host = summary.host?.trim() ?? ''
  if (name === '' && aliasValue === '' && host === '') {
    return
  }
  return {
    name,
    alias: aliasValue === '' ? undefined : aliasValue,
    host,
  }
}

export const authService = new AuthService()
