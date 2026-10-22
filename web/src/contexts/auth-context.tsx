import type { ReactNode } from 'react'
import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { toast } from '@/components/ui/use-toast'
import type { User } from '../lib/auth'
import { authService, LOGOUT_FAILED_MESSAGE } from '../lib/auth'

interface AuthContextType {
  user: User | null
  isLoading: boolean
  isAuthenticated: boolean
  login: (returnTo?: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<User | null>
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)
const SANITIZE_PATH_REGEX = /\/+/g
const SANITIZE_PATH_TRAILING_REGEX = /\/+$/

const normalizePathname = (value: string) => {
  if (!value) {
    return '/'
  }
  const sanitized = value
    .replace(SANITIZE_PATH_REGEX, '/')
    .replace(SANITIZE_PATH_TRAILING_REGEX, '')
  return sanitized === '' ? '/' : sanitized
}

const isLoginRoute = () => {
  if (typeof window === 'undefined') {
    return false
  }
  try {
    const base = import.meta.env.BASE_URL ?? '/'
    const normalizedBase = base.endsWith('/') ? base : `${base}/`
    const loginPath = normalizePathname(`${normalizedBase}login`)
    const current = normalizePathname(window.location.pathname)
    return current === loginPath
  } catch {
    return false
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  const fetchUser = useCallback(async (options: { suppressAuthError?: boolean } = {}) => {
    const currentUser = await authService.getCurrentUser(options)
    setUser(currentUser)
    return currentUser
  }, [])

  useEffect(() => {
    // Check if user is already logged in
    const suppressAuthError = isLoginRoute()
    fetchUser({ suppressAuthError })
      .catch(() => {
        /* swallow fetch errors; getCurrentUser already suppresses 401 */
      })
      .finally(() => {
        setIsLoading(false)
      })
  }, [fetchUser])

  const login = async (returnTo?: string) => {
    await authService.startLogin(returnTo)
  }

  const logout = async () => {
    if (await authService.logout()) {
      setUser(null)
      return
    }
    toast.error(LOGOUT_FAILED_MESSAGE)
  }

  const refresh = useCallback(async () => fetchUser({ suppressAuthError: true }), [fetchUser])

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: Boolean(user),
        login,
        logout,
        refresh,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}
