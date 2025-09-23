import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthProvider } from '../contexts/auth-context'
import { api } from '../lib/api'
import { UsersPage } from './users-page'

const ADMIN_LABEL_RE = /^Admin\b/i

const ADD_USER_RE = /Add User/i
const UPDATE_USER_RE = /Save Changes/i

// Mock the API
vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api')
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

vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, children, ...props }: { to?: unknown; children?: ReactNode }) => (
    <a href={typeof to === 'string' ? to : '#'} {...props}>
      {children}
    </a>
  ),
}))

// Mock toast controller
vi.mock('@/components/ui/use-toast', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
}))

// Mock useAuth globally
const mockUseAuth = vi.fn()
vi.mock('../contexts/auth-context', () => ({
  useAuth: () => mockUseAuth(),
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
}))

const mockUsers = [
  {
    email: 'admin@example.com',
    name: 'Admin User',
    roles: ['admin'],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    suspended: false,
  },
  {
    email: 'viewer@example.com',
    name: 'Viewer User',
    roles: ['viewer'],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    suspended: false,
  },
]

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

describe('UsersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Admin User', () => {
    it('renders users list for admin', async () => {
      vi.mocked(api.get).mockResolvedValue(mockUsers)

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Admin User')).toBeInTheDocument()
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })

      expect(screen.getByText('Add User')).toBeInTheDocument()
    })

    it('opens add user dialog when clicking Add User', async () => {
      vi.mocked(api.get).mockResolvedValue(mockUsers)

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Admin User')).toBeInTheDocument()
      })

      // Click the Add User button
      const addUserButton = screen.getByRole('button', { name: ADD_USER_RE })
      fireEvent.click(addUserButton)

      // Check for the dialog title
      const dialogTitle = await screen.findByRole('heading', { name: ADD_USER_RE })
      expect(dialogTitle).toBeInTheDocument()
      expect(screen.getByLabelText('Email')).toBeInTheDocument()
      expect(screen.getByLabelText('Name')).toBeInTheDocument()
    })

    it('creates a new user', async () => {
      vi.mocked(api.get).mockResolvedValue(mockUsers)
      vi.mocked(api.post).mockResolvedValueOnce({})

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Admin User')).toBeInTheDocument()
      })

      // Open dialog
      const addUserButton = screen.getByRole('button', { name: ADD_USER_RE })
      fireEvent.click(addUserButton)

      // Fill form
      const emailInput = screen.getByLabelText('Email')
      const nameInput = screen.getByLabelText('Name')

      fireEvent.change(emailInput, { target: { value: 'newuser@example.com' } })
      fireEvent.change(nameInput, { target: { value: 'New User' } })

      // The formData is already initialized with ['viewer'] role by default in handleAddUser
      // So we don't need to select it

      // Submit - find all buttons with "Add User" text and click the last one (in dialog)
      const addUserButtons = screen.getAllByRole('button', { name: ADD_USER_RE })
      const submitButton = addUserButtons.at(-1)
      if (!submitButton) {
        throw new Error('Submit button not found')
      }
      fireEvent.click(submitButton)

      await waitFor(() => {
        expect(api.post).toHaveBeenCalledWith('/.gateway/api/admin/users', {
          email: 'newuser@example.com',
          name: 'New User',
          roles: ['viewer'],
        })
      })
    })

    it('updates user roles', async () => {
      vi.mocked(api.get).mockResolvedValue(mockUsers)
      vi.mocked(api.put).mockResolvedValueOnce({})

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })

      // Click edit on viewer user
      const rows = screen.getAllByRole('row')
      const viewerRow = rows.find((row) => row.textContent?.includes('viewer@example.com'))
      if (!viewerRow) {
        throw new Error('Viewer row not found')
      }

      // Find the edit button within that row (first button with Edit2 icon)
      const buttons = within(viewerRow).getAllByRole('button')
      const editButton = buttons[0] // First button is edit
      fireEvent.click(editButton)

      await waitFor(() => {
        expect(screen.getByText('Edit User')).toBeInTheDocument()
      })

      // Select admin role via radio
      const adminRadio = screen.getByRole('radio', { name: ADMIN_LABEL_RE })
      fireEvent.click(adminRadio)

      // Submit
      fireEvent.click(screen.getByRole('button', { name: UPDATE_USER_RE }))

      await waitFor(() => {
        expect(api.put).toHaveBeenCalledWith('/.gateway/api/admin/users/viewer@example.com/roles', {
          roles: ['admin'],
        })
      })
    })

    // suspend/unsuspend removed from UI; no test required

    it('deletes a user with confirmation', async () => {
      const mockedGet = vi.mocked(api.get)
      mockedGet
        .mockResolvedValueOnce(mockUsers)
        .mockResolvedValueOnce(mockUsers.filter((u) => u.email !== 'viewer@example.com'))
      mockedGet.mockResolvedValue(mockUsers)
      vi.mocked(api.delete).mockResolvedValueOnce({})

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })

      // Find delete button for viewer user
      const rows = screen.getAllByRole('row')
      const viewerRow = rows.find((row) => row.textContent?.includes('viewer@example.com'))
      if (!viewerRow) {
        throw new Error('Viewer row not found')
      }

      // Find the delete button within that row (has Trash2 icon)
      const buttons = within(viewerRow).getAllByRole('button')
      const deleteButton = buttons.at(-1) // Last button is delete
      if (!deleteButton) {
        throw new Error('Delete button not found')
      }
      fireEvent.click(deleteButton)

      const dialog = await screen.findByRole('dialog')
      const confirmationInput = within(dialog).getByLabelText(/confirmation/i)
      fireEvent.change(confirmationInput, { target: { value: 'DELETE' } })

      const confirmDeleteButton = within(dialog).getByRole('button', { name: /Delete User/i })
      fireEvent.click(confirmDeleteButton)

      await waitFor(() => {
        expect(api.delete).toHaveBeenCalledWith('/.gateway/api/admin/users/viewer@example.com')
      })
    })

    it('prevents deleting own account', async () => {
      vi.mocked(api.get).mockResolvedValue(mockUsers)

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Admin User')).toBeInTheDocument()
      })

      // Find delete button for admin user (current user)
      const rows = screen.getAllByRole('row')
      const adminRow = rows.find((row) => row.textContent?.includes('admin@example.com'))
      const deleteButton = adminRow?.querySelector('button[disabled]')

      expect(deleteButton).toBeDisabled()
    })

    it('renders users without dates safely', async () => {
      const users = [
        {
          email: 'nodates@example.com',
          name: 'No Dates',
          roles: ['admin'],
          // created_at and updated_at intentionally empty to simulate missing
          created_at: '',
          updated_at: '',
          suspended: false,
        },
      ]
      vi.mocked(api.get).mockResolvedValue(users)

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('No Dates')).toBeInTheDocument()
      })

      // Should not crash; placeholders should render
      expect(screen.getAllByText('-').length).toBeGreaterThan(0)
    })
  })

  describe('Non-Admin User', () => {
    it('renders list for viewer without admin actions', async () => {
      vi.mocked(api.get).mockResolvedValue(mockUsers)

      const Wrapper = createWrapper({ email: 'viewer@example.com', roles: ['viewer'] })
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })
      // No Add User button for non-admin
      expect(screen.queryByRole('button', { name: ADD_USER_RE })).toBeNull()
      // No action buttons column
      const rows = screen.getAllByRole('row')
      const viewerRow = rows.find((row) => row.textContent?.includes('viewer@example.com'))
      if (!viewerRow) {
        throw new Error('Viewer row not found')
      }
      expect(within(viewerRow).queryByRole('button')).toBeNull()
    })

    it('renders list for ops without admin actions', async () => {
      vi.mocked(api.get).mockResolvedValue(mockUsers)

      const Wrapper = createWrapper({ email: 'ops@example.com', roles: ['ops'] })
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })
      expect(screen.queryByRole('button', { name: ADD_USER_RE })).toBeNull()
      const rows = screen.getAllByRole('row')
      const adminRow = rows.find((row) => row.textContent?.includes('admin@example.com'))
      if (!adminRow) {
        throw new Error('Admin row not found')
      }
      expect(within(adminRow).queryByRole('button')).toBeNull()
    })
  })

  describe('Error Handling', () => {
    it('displays error when API fails', async () => {
      vi.mocked(api.get).mockRejectedValueOnce(new Error('API Error'))

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Failed to load users')).toBeInTheDocument()
      })
    })

    it('shows loading state', () => {
      vi.mocked(api.get).mockImplementation(
        () =>
          new Promise(() => {
            /* never resolves in this test */
          })
      )

      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      // Check for loading spinner by class
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
    // moved to top-level
  })
})
