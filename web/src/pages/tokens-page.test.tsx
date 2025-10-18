import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { format } from 'date-fns'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HttpClientProvider } from '../contexts/http-client-context'
import { StepUpProvider } from '../contexts/step-up-context'
import { api } from '../lib/api'
import type { APIToken } from './tokens-page/index'
import { TokensPage } from './tokens-page/index'

// Test data
const defaultPermissions = [
  'convox:app:list',
  'convox:build:create',
  'convox:build:list',
  'convox:log:read',
  'convox:object:create',
  'convox:process:list',
  'convox:process:start',
  'convox:rack:read',
  'convox:release:create',
  'convox:release:list',
  'convox:release:promote',
]

const mockTokens: APIToken[] = [
  {
    id: 1,
    public_id: 'tok-1',
    name: 'CI/CD Pipeline',
    user_id: 10,
    permissions: defaultPermissions,
    last_used_at: '2024-01-15T00:00:00Z',
    created_at: '2024-01-01T00:00:00Z',
    expires_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 2,
    public_id: 'tok-2',
    name: 'Development Token',
    user_id: 11,
    permissions: ['convox:app:list'],
    last_used_at: null,
    created_at: '2024-01-10T00:00:00Z',
    expires_at: '2023-12-31T00:00:00Z',
  },
]

const mockPermissionMetadata = {
  permissions: [...defaultPermissions, 'convox:app:restart', 'convox:*:*'],
  roles: [
    {
      name: 'viewer',
      label: 'Viewer',
      description: 'Read only',
      permissions: ['convox:app:list'],
    },
    {
      name: 'cicd',
      label: 'CI/CD',
      description: 'Automation',
      permissions: defaultPermissions,
    },
    {
      name: 'admin',
      label: 'Admin',
      description: 'All access',
      permissions: ['convox:*:*'],
    },
  ],
  default_permissions: defaultPermissions,
  user_roles: ['admin'],
  user_permissions: ['convox:*:*'],
}

// Mock API - use same path component uses
vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      get: vi.fn(),
      post: vi.fn(),
      put: vi.fn(),
      delete: vi.fn(),
    },
  }
})

// Mock contexts
vi.mock('../contexts/auth-context', () => ({
  useAuth: vi.fn(),
  AuthProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}))

vi.mock('../contexts/step-up-context', () => ({
  StepUpProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useStepUp: () => ({
    handleStepUpError: vi.fn().mockReturnValue(false),
  }),
}))

// Mock UI components
vi.mock('@radix-ui/react-focus-scope', () => ({
  FocusScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}))

vi.mock('../components/ui/use-toast', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

vi.mock('../components/time-ago', () => ({
  TimeAgo: ({ date }: { date?: string | Date | null }) => {
    if (!date) return <span>—</span>
    const parsed = typeof date === 'string' ? new Date(date) : date
    if (Number.isNaN(parsed.getTime())) return <span>—</span>
    return <span>{format(parsed, 'MMM d, yyyy')}</span>
  },
}))

// Mock clipboard
Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn().mockResolvedValue(undefined),
  },
})

async function createWrapper(
  user = {
    email: 'admin@example.com',
    name: 'Admin User',
    roles: ['admin'],
    integrations: { slack: false, github: false, circleci: false },
  }
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })

  const { useAuth } = await import('../contexts/auth-context')
  vi.mocked(useAuth).mockReturnValue({
    user,
    isAuthenticated: true,
    isLoading: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  })

  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <HttpClientProvider>
        <StepUpProvider>{children}</StepUpProvider>
      </HttpClientProvider>
    </QueryClientProvider>
  )
}

describe('TokensPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Suppress React Query cache restoration warnings
    vi.spyOn(console, 'error').mockImplementation((message) => {
      if (typeof message === 'string' && message.includes('No queryFn was passed')) {
        return
      }
      console.error(message)
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders tokens list', async () => {
    vi.mocked(api.get).mockImplementation((url: string) => {
      if (url.includes('/permissions')) {
        return Promise.resolve(mockPermissionMetadata)
      }
      return Promise.resolve(mockTokens)
    })

    const Wrapper = await createWrapper()
    render(<TokensPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
    })

    expect(screen.getByText('Development Token')).toBeInTheDocument()
    expect(screen.getByText('Expired')).toBeInTheDocument()
    expect(screen.getByText('Active')).toBeInTheDocument()
  })

  it('shows empty state', async () => {
    vi.mocked(api.get).mockResolvedValueOnce(mockPermissionMetadata).mockResolvedValueOnce([])

    const Wrapper = await createWrapper()
    render(<TokensPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText('No API tokens created yet')).toBeInTheDocument()
    })
  })

  it('handles error state', async () => {
    vi.mocked(api.get)
      .mockResolvedValueOnce(mockPermissionMetadata)
      .mockRejectedValueOnce(new Error('API Error'))

    const Wrapper = await createWrapper()
    render(<TokensPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText('Failed to load API tokens')).toBeInTheDocument()
    })
  })

  it('creates a new token', async () => {
    vi.mocked(api.get).mockImplementation((url: string) => {
      if (url.includes('/permissions')) {
        return Promise.resolve(mockPermissionMetadata)
      }
      return Promise.resolve(mockTokens)
    })

    vi.mocked(api.post).mockResolvedValue({
      token: 'gat_new123',
      api_token: {
        id: 3,
        public_id: 'tok-3',
        name: 'New Token',
        user_id: 12,
        permissions: defaultPermissions,
        last_used_at: null,
        created_at: '2024-01-20T00:00:00Z',
        expires_at: '2025-01-20T00:00:00Z',
      },
    })

    const Wrapper = await createWrapper()
    render(<TokensPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Create Token'))
    const nameInput = screen.getByLabelText('Token Name')
    fireEvent.change(nameInput, { target: { value: 'New Token' } })
    fireEvent.click(screen.getByRole('button', { name: /create token/i }))

    await waitFor(() => {
      expect(api.post).toHaveBeenCalledWith('/api/v1/api-tokens', {
        name: 'New Token',
        permissions: defaultPermissions,
      })
    })

    await waitFor(() => {
      expect(screen.getByText('gat_new123')).toBeInTheDocument()
    })
  })

  it('edits a token', async () => {
    vi.mocked(api.get).mockImplementation((url: string) => {
      if (url.includes('/permissions')) {
        return Promise.resolve(mockPermissionMetadata)
      }
      return Promise.resolve(mockTokens)
    })

    vi.mocked(api.put).mockResolvedValue(mockTokens[0])

    const Wrapper = await createWrapper()
    const user = userEvent.setup()
    render(<TokensPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
    })

    const dropdownButton = screen.getByLabelText('Actions for CI/CD Pipeline')
    await user.click(dropdownButton)

    const editMenuItem = await screen.findByText('Edit Token')
    await user.click(editMenuItem)

    await waitFor(() => {
      expect(screen.getByText('Edit API Token')).toBeInTheDocument()
    })

    const dialog = await screen.findByRole('dialog')
    const nameInput = within(dialog).getByLabelText('Token Name')
    await user.clear(nameInput)
    await user.type(nameInput, 'Updated Token')
    await user.click(within(dialog).getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(api.put).toHaveBeenCalled()
    })
  })

  it('deletes a token', async () => {
    vi.mocked(api.get).mockImplementation((url: string) => {
      if (url.includes('/permissions')) {
        return Promise.resolve(mockPermissionMetadata)
      }
      return Promise.resolve(mockTokens)
    })

    vi.mocked(api.delete).mockResolvedValue({})

    const Wrapper = await createWrapper()
    const user = userEvent.setup()
    render(<TokensPage />, { wrapper: Wrapper })

    await waitFor(() => {
      expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
    })

    const dropdownButton = screen.getByLabelText('Actions for CI/CD Pipeline')
    await user.click(dropdownButton)

    const deleteMenuItem = await screen.findByText('Delete Token')
    await user.click(deleteMenuItem)

    const confirmInput = await screen.findByLabelText('Confirmation')
    await user.type(confirmInput, 'DELETE')
    await user.click(screen.getByRole('button', { name: /delete token/i }))

    await waitFor(() => {
      expect(api.delete).toHaveBeenCalledWith('/api/v1/api-tokens/tok-1')
    })
  })
})
