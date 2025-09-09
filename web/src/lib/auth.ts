import type { AxiosError } from 'axios'
import axios from 'axios'
import Cookies from 'js-cookie'

const API_BASE: string = import.meta.env.PROD ? (import.meta.env.VITE_API_BASE_URL ?? '') : ''

export interface User {
  email: string
  name: string
  roles: string[]
  rack?: { name: string; host: string }
}

export interface AuthState {
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
    window.location.href = `${API_BASE}/.gateway/api/auth/web/login`
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
      token: this.getToken(),
      isAuthenticated: true,
    }
  }

  // Get current user (cookie-based auth; no JS access to HttpOnly cookie needed)
  async getCurrentUser(): Promise<User | null> {
    try {
      const response = await axios.get(`${API_BASE}/.gateway/api/me`, {
        withCredentials: true,
      })
      return response.data
    } catch (err: unknown) {
      // Mark for UI to show an error after redirect to login
      const status = (err as AxiosError)?.response?.status
      try {
        if (status === 401) {
          sessionStorage.setItem('auth_error', 'Unauthorized. Please sign in to continue.')
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
  fetch(`${API_BASE}/.gateway/api/auth/web/logout`, { credentials: 'include' })
      .catch((_e) => {
        /* ignore network errors during logout */
      })
      .finally(() => {
        const base = import.meta.env.BASE_URL || '/'
        // Use assign to ease testing under jsdom and avoid Location href setter issues
        window.location.assign(`${base}login`)
      })
  }

  // Get stored token
  getToken(): string | null {
    return Cookies.get('gateway_token') || null
  }
}

export const authService = new AuthService()
