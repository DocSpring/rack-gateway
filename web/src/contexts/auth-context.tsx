import type { ReactNode } from 'react'
import { createContext, useContext, useEffect, useState } from 'react'
import type { User } from '../lib/auth'
import { authService } from '../lib/auth'

type AuthContextType = {
  user: User | null
  isLoading: boolean
  isAuthenticated: boolean
  login: (rack?: string) => Promise<void>
  logout: () => void
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
  } catch (_error) {
    return false
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    // Check if user is already logged in
    const suppressAuthError = isLoginRoute()
    authService
      .getCurrentUser({ suppressAuthError })
      .then((currentUser) => {
        setUser(currentUser)
      })
      .catch(() => {
        // Not logged in or token expired
        setUser(null)
      })
      .finally(() => {
        setIsLoading(false)
      })
  }, [])

  const login = async (rack?: string) => {
    await authService.startLogin(rack)
  }

  const logout = () => {
    authService.logout()
    setUser(null)
  }

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        login,
        logout,
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
