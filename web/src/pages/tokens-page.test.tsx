import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { format } from 'date-fns'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthProvider } from '../contexts/auth-context'
import { api } from '../lib/api'
import type { APIToken } from './tokens-page'
import { TokensPage } from './tokens-page'

const CREATE_TOKEN_RE = /Create Token/i
const COPY_TOKEN_NOW_RE = /Copy this token now/i
const DELETE_TOKEN_RE = /Delete Token/i

// Mock the API while preserving exported constants such as AVAILABLE_ROLES
vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api')
  return {
    ...actual,
    api: {
      get: vi.fn(),
      post: vi.fn(),
      delete: vi.fn(),
      put: vi.fn(),
    },
  }
})

// Mock sonner
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

vi.mock('../components/time-ago', () => ({
  TimeAgo: ({ date }: { date?: string | Date | null }) => {
    if (!date) {
      return <span>—</span>
    }
    const parsed = typeof date === 'string' ? new Date(date) : date
    if (Number.isNaN(parsed.getTime())) {
      return <span>—</span>
    }
    return <span>{format(parsed, 'MMM d, yyyy')}</span>
  },
}))

// Mock useAuth globally to control roles
const mockUseAuth = vi.fn()
vi.mock('../contexts/auth-context', () => ({
  useAuth: () => mockUseAuth(),
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
}))

// Mock clipboard API
Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn().mockResolvedValue(undefined),
  },
})

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
    name: 'CI/CD Pipeline',
    user_id: 10,
    permissions: defaultPermissions,
    last_used_at: '2024-01-15T00:00:00Z',
    created_at: '2024-01-01T00:00:00Z',
    expires_at: '2026-01-01T00:00:00Z', // Active - expires in future
  },
  {
    id: 2,
    name: 'Development Token',
    user_id: 11,
    permissions: ['convox:app:list'],
    last_used_at: null,
    created_at: '2024-01-10T00:00:00Z',
    expires_at: '2023-12-31T00:00:00Z', // Expired
  },
]

const APP_LIST_REGEX = /convox:app:list/i
const RELEASE_PROMOTE_REGEX = /convox:release:promote/i
const RACK_UPDATE_REGEX = /convox:rack:update/i
const WILDCARD_REGEX = /convox:\*:\*/i
const ALL_HEADING_REGEX = /^All$/i
const APP_RESTART_REGEX = /convox:app:restart/i
const SAVE_BUTTON_REGEX = /save/i

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

const createWrapper = (user = { email: 'admin@example.com', roles: ['admin'] }) => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })

  // Set up the mock for this test
  mockUseAuth.mockReturnValue({
    user,
    isAuthenticated: true,
    login: vi.fn(),
    logout: vi.fn(),
  })

  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>{children}</AuthProvider>
    </QueryClientProvider>
  )
}

describe('TokensPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Token List', () => {
    it('renders tokens list', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
        expect(screen.getByText('Development Token')).toBeInTheDocument()
      })

      // Check for badges - Development Token is expired, CI/CD Pipeline is active
      // Just check they exist, don't check the count since there might be duplicates
      expect(screen.getByText('Expired')).toBeInTheDocument()
      expect(screen.getByText('Active')).toBeInTheDocument()
    })

    it('shows empty state when no tokens', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockPermissionMetadata).mockResolvedValueOnce([])

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('No API tokens created yet')).toBeInTheDocument()
      })
    })

    it('displays last used date', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Jan 15, 2024')).toBeInTheDocument() // Last used
        expect(screen.getByText('Never')).toBeInTheDocument() // Never used
      })
    })

    it('renders gracefully when API returns null or non-array', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(null as unknown as APIToken[])

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('No API tokens created yet')).toBeInTheDocument()
      })
    })
  })

  describe('Token Creation', () => {
    it('opens create token dialog', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByText('Create Token'))

      expect(screen.getByText('Create API Token')).toBeInTheDocument()
      expect(screen.getByLabelText('Token Name')).toBeInTheDocument()
      expect(screen.getByRole('checkbox', { name: APP_LIST_REGEX })).toBeChecked()
      expect(screen.getByRole('checkbox', { name: WILDCARD_REGEX })).toBeInTheDocument()
      expect(screen.queryByText(ALL_HEADING_REGEX, { selector: 'p' })).not.toBeInTheDocument()
    })

    it('creates a new token and displays it', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)
        .mockResolvedValueOnce([
          ...mockTokens,
          {
            id: 3,
            name: 'New Token',
            user_id: 12,
            permissions: defaultPermissions,
            last_used_at: null,
            created_at: '2024-01-20T00:00:00Z',
            expires_at: '2026-01-20T00:00:00Z',
          },
        ])
      vi.mocked(api.post).mockResolvedValueOnce({
        token: 'gat_abc123xyz456',
        api_token: {
          id: 3,
          name: 'New Token',
          user_id: 12,
          permissions: defaultPermissions,
          last_used_at: null,
          created_at: '2024-01-20T00:00:00Z',
          expires_at: '2025-01-20T00:00:00Z',
        },
      })

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      // Open dialog
      fireEvent.click(screen.getByText('Create Token'))

      // Enter token name
      const nameInput = screen.getByLabelText('Token Name')
      fireEvent.change(nameInput, { target: { value: 'New Token' } })

      // Submit
      fireEvent.click(screen.getByRole('button', { name: CREATE_TOKEN_RE }))

      await waitFor(() => {
        expect(api.post).toHaveBeenCalledWith('/.gateway/api/admin/tokens', {
          name: 'New Token',
          permissions: defaultPermissions,
        })
      })

      // Should show the token
      await waitFor(() => {
        expect(screen.getByText('gat_abc123xyz456')).toBeInTheDocument()
        expect(screen.getByText(COPY_TOKEN_NOW_RE)).toBeInTheDocument()
      })
    })

    it('copies token to clipboard', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)
        .mockResolvedValueOnce([
          ...mockTokens,
          {
            id: 3,
            name: 'New Token',
            user_id: 12,
            permissions: defaultPermissions,
            last_used_at: null,
            created_at: '2024-01-20T00:00:00Z',
            expires_at: '2026-01-20T00:00:00Z',
          },
        ])
      vi.mocked(api.post).mockResolvedValueOnce({
        token: 'gat_abc123xyz456',
        api_token: {
          id: 3,
          name: 'New Token',
          user_id: 12,
          permissions: defaultPermissions,
          last_used_at: null,
          created_at: '2024-01-20T00:00:00Z',
          expires_at: '2025-01-20T00:00:00Z',
        },
      })

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      // Create token
      fireEvent.click(screen.getByText('Create Token'))
      const nameInput = screen.getByLabelText('Token Name')
      fireEvent.change(nameInput, { target: { value: 'New Token' } })
      fireEvent.click(screen.getByRole('button', { name: CREATE_TOKEN_RE }))

      await waitFor(() => {
        expect(screen.getByText('gat_abc123xyz456')).toBeInTheDocument()
      })

      // Copy token
      fireEvent.click(screen.getByText('Copy Token'))

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('gat_abc123xyz456')
    })

    it('applies role shortcut selections', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)
        .mockResolvedValueOnce([
          ...mockTokens,
          {
            id: 4,
            name: 'Viewer Token',
            user_id: 13,
            permissions: ['convox:app:list'],
            last_used_at: null,
            created_at: '2024-01-25T00:00:00Z',
            expires_at: null,
          },
        ])
      vi.mocked(api.post).mockResolvedValueOnce({
        token: 'gat_viewer123',
        api_token: {
          id: 4,
          name: 'Viewer Token',
          user_id: 13,
          permissions: ['convox:app:list'],
          last_used_at: null,
          created_at: '2024-01-25T00:00:00Z',
          expires_at: '2025-01-25T00:00:00Z',
        },
      })

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByText('Create Token'))
      fireEvent.click(screen.getByText('Viewer'))

      expect(screen.getByRole('checkbox', { name: APP_LIST_REGEX })).toBeChecked()
      expect(screen.getByRole('checkbox', { name: RELEASE_PROMOTE_REGEX })).not.toBeChecked()

      const nameInput = screen.getByLabelText('Token Name')
      fireEvent.change(nameInput, { target: { value: 'Viewer Token' } })
      fireEvent.click(screen.getByRole('button', { name: CREATE_TOKEN_RE }))

      await waitFor(() => {
        expect(api.post).toHaveBeenCalledWith('/.gateway/api/admin/tokens', {
          name: 'Viewer Token',
          permissions: ['convox:app:list'],
        })
      })
    })

    it('disables permissions that exceed current user roles', async () => {
      const deployerMetadata = {
        ...mockPermissionMetadata,
        permissions: [...mockPermissionMetadata.permissions, 'convox:rack:update'],
        user_roles: ['deployer'],
        user_permissions: defaultPermissions,
      }

      vi.mocked(api.get).mockResolvedValueOnce(deployerMetadata).mockResolvedValueOnce(mockTokens)

      const Wrapper = createWrapper({
        email: 'deployer@example.com',
        roles: ['deployer'],
      })
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByText('Create Token'))

      const restrictedCheckbox = screen.getByRole('checkbox', {
        name: RACK_UPDATE_REGEX,
      })
      expect(restrictedCheckbox).toBeDisabled()
    })

    it('validates token name is not empty', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      // Open dialog
      fireEvent.click(screen.getByText('Create Token'))

      // Submit without entering name
      fireEvent.click(screen.getByRole('button', { name: CREATE_TOKEN_RE }))

      // Should not call API
      expect(api.post).not.toHaveBeenCalled()
    })
  })

  describe('Date Rendering', () => {
    it('handles missing or invalid dates without crashing', async () => {
      const badTokens: APIToken[] = [
        {
          id: 999,
          name: 'Bad Token',
          user_id: 42,
          permissions: [],
          last_used_at: null,
          created_at: '',
          expires_at: null,
        },
      ]
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(badTokens)

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Bad Token')).toBeInTheDocument()
      })

      // Should show placeholders
      expect(screen.getAllByText('—').length).toBeGreaterThan(0)
      expect(screen.getByText('Never')).toBeInTheDocument()
    })
  })

  describe('Token Deletion', () => {
    it('deletes a token', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)
        .mockResolvedValueOnce(mockTokens.filter((t) => t.id !== 1)) // After deletion
      vi.mocked(api.delete).mockResolvedValueOnce({})

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      // Find delete button for first token by looking for the trash icon button
      const rows = screen.getAllByRole('row')
      // Find the row with CI/CD Pipeline
      const pipelineRow = rows.find((row) => row.textContent?.includes('CI/CD Pipeline'))
      const deleteButton = pipelineRow?.querySelector('button')

      if (!deleteButton) {
        throw new Error('Delete button not found')
      }
      fireEvent.click(deleteButton)
      // Confirm modal: type DELETE and confirm
      const confirmInput = await screen.findByLabelText('Confirmation')
      fireEvent.change(confirmInput, { target: { value: 'DELETE' } })
      fireEvent.click(screen.getByRole('button', { name: DELETE_TOKEN_RE }))

      await waitFor(() => {
        expect(api.delete).toHaveBeenCalledWith('/.gateway/api/admin/tokens/1')
      })
    })
  })

  describe('Error Handling', () => {
    it('displays error when loading fails', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockRejectedValueOnce(new Error('API Error'))

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Failed to load API tokens')).toBeInTheDocument()
      })
    })

    it('shows loading state', () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockImplementationOnce(
          () =>
            new Promise(() => {
              /* never resolves in this test */
            })
        )

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      // Check for loading spinner by class
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })

    it('handles token creation error', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)
      vi.mocked(api.post).mockRejectedValueOnce(new Error('Creation failed'))

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      // Try to create token
      fireEvent.click(screen.getByText('Create Token'))
      const nameInput = screen.getByLabelText('Token Name')
      fireEvent.change(nameInput, { target: { value: 'New Token' } })
      fireEvent.click(screen.getByRole('button', { name: CREATE_TOKEN_RE }))

      await waitFor(() => {
        expect(api.post).toHaveBeenCalled()
      })

      // Dialog should remain open on error
      expect(screen.getByText('Create API Token')).toBeInTheDocument()
    })

    it('handles token deletion error', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockPermissionMetadata)
        .mockResolvedValueOnce(mockTokens)
      vi.mocked(api.delete).mockRejectedValueOnce(new Error('Deletion failed'))

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      // Try to delete token
      const rows = screen.getAllByRole('row')
      const pipelineRow = rows.find((row) => row.textContent?.includes('CI/CD Pipeline'))
      const deleteButton = pipelineRow?.querySelector('button')

      if (!deleteButton) {
        throw new Error('Delete button not found')
      }
      fireEvent.click(deleteButton)
      const confirmInput = await screen.findByLabelText('Confirmation')
      fireEvent.change(confirmInput, { target: { value: 'DELETE' } })
      fireEvent.click(screen.getByRole('button', { name: DELETE_TOKEN_RE }))

      await waitFor(() => {
        expect(api.delete).toHaveBeenCalled()
      })

      // Token should still be visible after failed deletion
      expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
    })
    // moved to top-level
  })

  describe('Token Editing', () => {
    it('allows updating token name and permissions', async () => {
      const getMock = vi.mocked(api.get)
      getMock.mockImplementation((url: string) => {
        if (url.includes('/tokens/permissions')) {
          return Promise.resolve(mockPermissionMetadata as unknown as APIToken[])
        }
        return Promise.resolve(mockTokens)
      })
      vi.mocked(api.put).mockResolvedValueOnce(mockTokens[0])

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText('Edit Token CI/CD Pipeline'))

      await waitFor(() => {
        expect(screen.getByText('Edit API Token')).toBeInTheDocument()
      })

      const nameInput = screen.getByLabelText('Token Name')
      fireEvent.change(nameInput, { target: { value: 'Updated Token' } })

      const restartOption = screen.getByText(APP_RESTART_REGEX)
      fireEvent.click(restartOption)

      fireEvent.click(screen.getByRole('button', { name: SAVE_BUTTON_REGEX }))

      await waitFor(() => {
        expect(api.put).toHaveBeenCalledTimes(1)
      })

      const [, payload] = vi.mocked(api.put).mock.calls[0]
      const castPayload = payload as { name: string; permissions: string[] }
      expect(castPayload).toMatchObject({ name: 'Updated Token' })
      expect(castPayload.permissions).toContain('convox:app:restart')
    })
  })
})
