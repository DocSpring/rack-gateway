import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { createAppRouter } from './app'

// Mock AuthProvider to avoid network calls in tests
vi.mock('./contexts/auth-context', () => ({
  AuthProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useAuth: () => ({
    user: null,
    isAuthenticated: false,
    isLoading: false,
    login: vi.fn(),
    logout: vi.fn(),
  }),
}))

describe('App Router', () => {
  it('renders Login page at /.gateway/web/login', async () => {
    const router = createAppRouter('/.gateway/web')
    window.history.pushState({}, '', '/.gateway/web/login')
    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    const btn = await screen.findByTestId('login-cta')
    expect(btn).toBeInTheDocument()
  })
})
