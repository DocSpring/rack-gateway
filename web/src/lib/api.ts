import type { AxiosInstance } from 'axios'
import axios from 'axios'
import { authService } from './auth'

const API_BASE = '/api'

export interface UserConfig {
  name: string
  roles: string[]
}

export interface GatewayConfig {
  domain: string
  users: Record<string, UserConfig>
}

// Hardcoded roles - these are defined in the Go binary
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
  private client: AxiosInstance

  constructor() {
    this.client = axios.create({
      baseURL: API_BASE,
      headers: {
        'Content-Type': 'application/json',
      },
    })

    // Add auth interceptor
    this.client.interceptors.request.use((config) => {
      const token = authService.getToken()
      if (token) {
        config.headers.Authorization = `Bearer ${token}`
      }
      return config
    })

    // Add response interceptor for auth errors
    this.client.interceptors.response.use(
      (response) => response,
      (error) => {
        if (error.response?.status === 401) {
          authService.logout()
        }
        return Promise.reject(error)
      }
    )
  }

  // Get gateway configuration (domain + users)
  async getConfig(): Promise<GatewayConfig> {
    const response = await this.client.get('/v1/admin/config')
    return response.data
  }

  // Update entire gateway configuration
  async updateConfig(config: GatewayConfig): Promise<void> {
    await this.client.put('/v1/admin/config', config)
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

  // Health check
  async healthCheck(): Promise<{ status: string; version: string }> {
    const response = await this.client.get('/.gateway/health')
    return response.data
  }
}

export const apiService = new ApiService()
