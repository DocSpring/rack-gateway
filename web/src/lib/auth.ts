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
  private codeVerifier: string | null = null
  private state: string | null = null

  // Generate cryptographically secure random strings
  private generateRandomString(length: number): string {
    const array = new Uint8Array(length)
    crypto.getRandomValues(array)
    return btoa(String.fromCharCode(...array))
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=/g, '')
  }

  // Generate PKCE challenge
  private async generatePKCEChallenge(verifier: string): Promise<string> {
    const encoder = new TextEncoder()
    const data = encoder.encode(verifier)
    const hash = await crypto.subtle.digest('SHA-256', data)
    return btoa(String.fromCharCode(...new Uint8Array(hash)))
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=/g, '')
  }

  // Start OAuth flow with PKCE
  async startLogin(rack = 'default'): Promise<void> {
    // Generate PKCE parameters
    this.codeVerifier = this.generateRandomString(128)
    this.state = this.generateRandomString(32)
    const codeChallenge = await this.generatePKCEChallenge(this.codeVerifier)

    // Store in session storage for callback
    sessionStorage.setItem('code_verifier', this.codeVerifier)
    sessionStorage.setItem('oauth_state', this.state)
    sessionStorage.setItem('oauth_rack', rack)

    // Get OAuth URL from backend
    const response = await axios.get(`${API_BASE}/.gateway/login/start`, {
      params: {
        code_challenge: codeChallenge,
        state: this.state,
        rack,
        redirect_uri: `${window.location.origin}/auth/callback`,
      },
    })

    // Redirect to Google OAuth
    window.location.href = response.data.authUrl
  }

  // Handle OAuth callback
  async handleCallback(code: string, state: string): Promise<AuthState> {
    // Verify state matches
    const storedState = sessionStorage.getItem('oauth_state')
    if (state !== storedState) {
      throw new Error('Invalid state parameter')
    }

    // Get stored PKCE verifier
    const codeVerifier = sessionStorage.getItem('code_verifier')
    const rack = sessionStorage.getItem('oauth_rack') || 'default'

    if (!codeVerifier) {
      throw new Error('Missing PKCE verifier')
    }

    // Exchange code for token
    const response = await axios.post(`${API_BASE}/.gateway/login/callback`, {
      code,
      code_verifier: codeVerifier,
      rack,
      redirect_uri: `${window.location.origin}/auth/callback`,
    })

    const { token, user } = response.data

    // Store token in cookie
    Cookies.set('gateway_token', token, {
      expires: 30, // 30 days
      secure: window.location.protocol === 'https:',
      sameSite: 'strict',
    })

    // Clean up session storage
    sessionStorage.removeItem('code_verifier')
    sessionStorage.removeItem('oauth_state')
    sessionStorage.removeItem('oauth_rack')

    return {
      user,
      token,
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
