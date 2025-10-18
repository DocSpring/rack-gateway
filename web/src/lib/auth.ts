import type { AxiosError } from 'axios'
import axios from 'axios'
import type { HandlersInfoResponse, HandlersRackSummary } from '@/api/schemas'
import { APIRoute } from './routes'

export const SESSION_EXPIRED_MESSAGE = 'Session expired. Please sign in again.'

export type User = {
  email: string
  name: string
  roles: string[]
  rack?: { name: string; alias?: string; host: string }
  mfa_enrolled?: boolean
  mfa_required?: boolean
  preferred_mfa_method?: string | null
  recent_step_up_expires_at?: string | null
  has_trusted_device?: boolean
  integrations: {
    slack: boolean
    github: boolean
    circleci: boolean
  }
}

type AuthState = {
  user: User | null
  token: string | null
  isAuthenticated: boolean
}

class AuthService {
  // (PKCE helpers removed; web flow uses server-side OAuth)

  // Start OAuth flow for web (no PKCE needed)
  startLogin(rack = 'default'): void {
    // Store rack for callback
    sessionStorage.setItem('oauth_rack', rack)
    // Redirect directly to web login endpoint
    window.location.href = APIRoute('auth/web/login')
  }

  // Handle OAuth callback
  async handleCallback(_code: string, _state: string): Promise<AuthState> {
    // For web flow, the callback is handled server-side
    // This method is called after server redirect with token
    // rack selection is only used server-side; just clear any prior selection

    // The server should have set a cookie or returned a token
    // We just need to fetch the current user
    const user = await this.getCurrentUser()
    if (!user) {
      throw new Error('Failed to get user after OAuth callback')
    }

    // Clean up session storage
    sessionStorage.removeItem('oauth_rack')

    return {
      user,
      token: null,
      isAuthenticated: true,
    }
  }

  // Get current user (cookie-based auth; no JS access to HttpOnly cookie needed)
  async getCurrentUser(options: { suppressAuthError?: boolean } = {}): Promise<User | null> {
    try {
      const response = await axios.get<HandlersInfoResponse>(APIRoute('info'), {
        withCredentials: true,
      })
      const data = response.data
      const userInfo = data?.user

      const rack: User['rack'] = normalizeRack(data?.rack)
      const roles = Array.isArray(userInfo?.roles) ? userInfo.roles : []

      const mapped: User = {
        email: userInfo?.email ?? '',
        name: userInfo?.name ?? '',
        roles,
        rack,
        mfa_enrolled: Boolean(userInfo?.mfa_enrolled),
        mfa_required: Boolean(userInfo?.mfa_required),
        preferred_mfa_method: userInfo?.preferred_mfa_method ?? null,
        recent_step_up_expires_at: userInfo?.recent_step_up_expires_at ?? null,
        has_trusted_device: Boolean(userInfo?.has_trusted_device),
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
      } catch (_e) {
        /* ignore */
      }
      return null
    }
  }

  // Logout
  logout(): void {
    // Request server-side logout to clear HttpOnly cookie, then go to login
    fetch(APIRoute('auth/web/logout'), { credentials: 'include' })
      .catch((_e) => {
        /* ignore network errors during logout */
      })
      .finally(() => {
        const base = import.meta.env.BASE_URL || '/'
        // Use assign to ease testing under jsdom and avoid Location href setter issues
        window.location.assign(`${base}login`)
      })
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
