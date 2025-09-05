import axios from 'axios'
import Cookies from 'js-cookie'

const API_BASE = '/api'

export interface User {
  email: string
  name: string
  roles: string[]
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
    window.location.href = `${API_BASE}/.gateway/web/login`
  }

  // Handle OAuth callback
  async handleCallback(_code: string, _state: string): Promise<AuthState> {
    // For web flow, the callback is handled server-side
    // This method is called after server redirect with token
    const _rack = sessionStorage.getItem('oauth_rack') || 'default'

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

  // Get current user
  async getCurrentUser(): Promise<User | null> {
    const token = Cookies.get('gateway_token')
    if (!token) {
      return null
    }

    try {
      const response = await axios.get(`${API_BASE}/.gateway/me`, {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      })
      return response.data
    } catch (_error) {
      // Token might be expired
      this.logout()
      return null
    }
  }

  // Logout
  logout(): void {
    Cookies.remove('gateway_token')
    window.location.href = '/'
  }

  // Get stored token
  getToken(): string | null {
    return Cookies.get('gateway_token') || null
  }
}

export const authService = new AuthService()
