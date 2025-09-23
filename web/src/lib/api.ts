import type { AxiosInstance } from 'axios'
import axios from 'axios'
import { toast } from '@/components/ui/use-toast'
import { authService } from './auth'
import { ensureCsrfToken, getCsrfToken } from './csrf'

// In production builds (gateway serves UI), allow overriding API base URL.
// In dev, keep empty to let Vite proxy handle '/.gateway/api/*'.
const API_BASE: string = import.meta.env.PROD ? (import.meta.env.VITE_API_BASE_URL ?? '') : ''

export type CreateUserRequest = {
  email: string
  name: string
  roles: string[]
}

export type UpdateUserProfileRequest = {
  email: string
  name: string
}

export type GatewayUser = {
  id: number
  email: string
  name: string
  roles: string[]
  created_at: string
  updated_at: string
  suspended: boolean
  created_by_email?: string
  created_by_name?: string
}

export type UserSessionSummary = {
  id: number
  created_at: string
  last_seen_at: string
  expires_at: string
  ip_address?: string
  user_agent?: string
  metadata?: Record<string, unknown>
}

export type AuditLogEntry = {
  id: number
  timestamp: string
  user_email: string
  user_name: string
  action_type: string
  action: string
  resource: string
  resource_type?: string
  status: string
  details?: string
  ip_address?: string
  user_agent?: string
  http_status?: number
  response_time_ms: number
}

// Hardcoded roles - these are defined in the Go binary.
// The cicd automation role is intentionally omitted; it is exposed only for API tokens.
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

class ApiService {
  private readonly client: AxiosInstance

  constructor() {
    this.client = axios.create({
      baseURL: API_BASE,
      withCredentials: true,
      headers: {
        'Content-Type': 'application/json',
      },
    })

    // Add auth interceptor
    this.client.interceptors.request.use(async (config) => {
      // Attach CSRF token for unsafe admin requests
      try {
        const method = (config.method || 'get').toUpperCase()
        const url = config.url || ''
        if (method !== 'GET' && url.startsWith('/.gateway/api/admin')) {
          let csrf = getCsrfToken()
          if (!csrf) {
            csrf = await ensureCsrfToken()
          }
          if (csrf) {
            config.headers['X-CSRF-Token'] = csrf
          }
        }
      } catch (_e) {
        /* ignore */
      }
      return config
    })

    // Add response interceptor for auth errors
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: acceptable branching for interceptor
    const onResponseError = (error: unknown) => {
      const err = error as {
        response?: { status?: number; data?: unknown }
        config?: { url?: string; method?: string }
      }
      if (err.response?.status === 401) {
        // Persist an auth error message to show after redirect
        try {
          sessionStorage.setItem('auth_error', 'Your session has expired. Please sign in again.')
        } catch (_e) {
          /* ignore */
        }
        // Best-effort immediate toast (may disappear on navigation)
        try {
          toast.error('Your session has expired. Please sign in again.')
        } catch (_e) {
          /* ignore */
        }
        authService.logout()
      }
      // If CSRF missing/invalid, try to refresh once
      if (err.response?.status === 403 && err.response?.data) {
        try {
          const url: string = err.config?.url || ''
          const method: string = (err.config?.method || 'get').toUpperCase()
          if (method !== 'GET' && url.startsWith('/.gateway/api/admin')) {
            ensureCsrfToken().catch(() => {
              /* ignore */
            })
          }
        } catch (_e) {
          /* ignore */
        }
      }
      return Promise.reject(err)
    }
    this.client.interceptors.response.use((response) => response, onResponseError)
  }

  // Get gateway configuration (domain + users)
  async getConfig(): Promise<GatewayConfig> {
    const response = await this.client.get('/.gateway/api/admin/config')
    return response.data
  }

  // Update entire gateway configuration
  async updateConfig(config: GatewayConfig): Promise<void> {
    await this.client.put('/.gateway/api/admin/config', config)
  }

  // Add or update a user
  async saveUser(email: string, user: UserConfig): Promise<void> {
    const config = await this.getConfig()
    config.users[email] = user
    await this.updateConfig(config)
  }

  // Delete a user
  async deleteUser(email: string): Promise<void> {
    const config = await this.getConfig()
    delete config.users[email]
    await this.updateConfig(config)
  }

  async getUser(email: string): Promise<GatewayUser> {
    const response = await this.client.get(`/.gateway/api/admin/users/${encodeURIComponent(email)}`)
    return response.data
  }

  async getUserSessions(email: string): Promise<UserSessionSummary[]> {
    const response = await this.client.get(
      `/.gateway/api/admin/users/${encodeURIComponent(email)}/sessions`
    )
    return response.data
  }

  async revokeUserSession(email: string, sessionId: number): Promise<{ revoked: boolean }> {
    const response = await this.client.post(
      `/.gateway/api/admin/users/${encodeURIComponent(email)}/sessions/${sessionId}/revoke`
    )
    return response.data
  }

  async revokeAllUserSessions(email: string): Promise<{ revoked_count: number }> {
    const response = await this.client.post(
      `/.gateway/api/admin/users/${encodeURIComponent(email)}/sessions/revoke_all`
    )
    return response.data
  }

  async getUserAuditLogs(email: string, limit = 10): Promise<AuditLogEntry[]> {
    const response = await this.client.get('/.gateway/api/admin/audit', {
      params: { user: email, limit, page: 1, range: '7d' },
    })
    const data = response.data as { logs?: AuditLogEntry[] }
    return data.logs ?? []
  }

  // Generic HTTP methods for direct API access
  async get<T = unknown>(url: string): Promise<T> {
    const response = await this.client.get(url)
    return response.data
  }

  async post<T = unknown>(url: string, data?: unknown): Promise<T> {
    const response = await this.client.post(url, data)
    return response.data
  }

  async put<T = unknown>(url: string, data?: unknown): Promise<T> {
    const response = await this.client.put(url, data)
    return response.data
  }

  async delete<T = unknown>(url: string): Promise<T> {
    const response = await this.client.delete(url)
    return response.data
  }

  // Health check
  async healthCheck(): Promise<{ status: string; version: string }> {
    const response = await this.client.get('/.gateway/api/health')
    return response.data
  }
}

export const apiService = new ApiService()
export const api = apiService
