import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { vi, describe, it, expect, beforeEach } from 'vitest'
import { UsersPage } from './users-page'
import { AuthProvider } from '../contexts/auth-context'
import { api } from '../lib/api'

// Mock the API
vi.mock('../lib/api', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

// Mock sonner
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
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
      <BrowserRouter>
        <AuthProvider>{children}</AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}

describe('UsersPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Admin User', () => {
    it('renders users list for admin', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockUsers)
      
      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Admin User')).toBeInTheDocument()
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })

      expect(screen.getByText('Add User')).toBeInTheDocument()
    })

    it('opens add user dialog when clicking Add User', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockUsers)
      
      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Admin User')).toBeInTheDocument()
      })

      // Click the Add User button
      const addUserButton = screen.getByRole('button', { name: /Add User/i })
      fireEvent.click(addUserButton)

      // Check for the dialog title
      const dialogTitle = await screen.findByRole('heading', { name: /Add User/i })
      expect(dialogTitle).toBeInTheDocument()
      expect(screen.getByLabelText('Email')).toBeInTheDocument()
      expect(screen.getByLabelText('Name')).toBeInTheDocument()
    })

    it('creates a new user', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockUsers)
      vi.mocked(api.post).mockResolvedValueOnce({})
      
      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Admin User')).toBeInTheDocument()
      })

      // Open dialog
      const addUserButton = screen.getByRole('button', { name: /Add User/i })
      fireEvent.click(addUserButton)

      // Fill form
      const emailInput = screen.getByLabelText('Email')
      const nameInput = screen.getByLabelText('Name')
      
      fireEvent.change(emailInput, { target: { value: 'newuser@example.com' } })
      fireEvent.change(nameInput, { target: { value: 'New User' } })

      // The formData is already initialized with ['viewer'] role by default in handleAddUser
      // So we don't need to select it

      // Submit - find all buttons with "Add User" text and click the last one (in dialog)
      const addUserButtons = screen.getAllByRole('button', { name: /Add User/i })
      const submitButton = addUserButtons[addUserButtons.length - 1]
      fireEvent.click(submitButton)

      await waitFor(() => {
        expect(api.post).toHaveBeenCalledWith('/.gateway/admin/users', {
          email: 'newuser@example.com',
          name: 'New User',
          roles: ['viewer'],
        })
      })
    })

    it('updates user roles', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockUsers)
      vi.mocked(api.put).mockResolvedValueOnce({})
      
      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })

      // Click edit on viewer user
      const rows = screen.getAllByRole('row')
      const viewerRow = rows.find(row => row.textContent?.includes('viewer@example.com'))
      if (!viewerRow) throw new Error('Viewer row not found')
      
      // Find the edit button within that row (first button with Edit2 icon)
      const buttons = within(viewerRow).getAllByRole('button')
      const editButton = buttons[0] // First button is edit
      fireEvent.click(editButton)

      await waitFor(() => {
        expect(screen.getByText('Edit User')).toBeInTheDocument()
      })

      // Toggle admin role - find the checkbox for admin role
      const checkboxes = screen.getAllByRole('checkbox')
      const adminCheckbox = checkboxes.find(cb => cb.closest('div')?.textContent?.includes('Administrator'))
      if (adminCheckbox) fireEvent.click(adminCheckbox)

      // Submit
      fireEvent.click(screen.getByRole('button', { name: /Update User/i }))

      await waitFor(() => {
        expect(api.put).toHaveBeenCalledWith(
          '/.gateway/admin/users/viewer@example.com/roles',
          { roles: expect.arrayContaining(['viewer']) }
        )
      })
    })

    it('suspends a user', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockUsers)
      vi.mocked(api.put).mockResolvedValueOnce({})
      
      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })

      // Find suspend button for viewer user
      const rows = screen.getAllByRole('row')
      const viewerRow = rows.find(row => row.textContent?.includes('viewer@example.com'))
      if (!viewerRow) throw new Error('Viewer row not found')
      
      // Find the suspend button within that row (middle button with UserX icon)
      const buttons = within(viewerRow).getAllByRole('button')
      const suspendButton = buttons[1] // Middle button is suspend
      fireEvent.click(suspendButton)

      await waitFor(() => {
        expect(api.put).toHaveBeenCalledWith(
          '/.gateway/admin/users/viewer@example.com/suspend',
          { suspended: true }
        )
      })
    })

    it('deletes a user with confirmation', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockUsers)
        .mockResolvedValueOnce(mockUsers.filter(u => u.email !== 'viewer@example.com')) // After deletion
      vi.mocked(api.delete).mockResolvedValueOnce({})
      
      // Mock window.confirm
      const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValueOnce(true)
      
      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Viewer User')).toBeInTheDocument()
      })

      // Find delete button for viewer user
      const rows = screen.getAllByRole('row')
      const viewerRow = rows.find(row => row.textContent?.includes('viewer@example.com'))
      if (!viewerRow) throw new Error('Viewer row not found')
      
      // Find the delete button within that row (has Trash2 icon)
      const buttons = within(viewerRow).getAllByRole('button')
      const deleteButton = buttons[buttons.length - 1] // Last button is delete
      fireEvent.click(deleteButton)

      expect(confirmSpy).toHaveBeenCalledWith('Are you sure you want to delete viewer@example.com?')

      await waitFor(() => {
        expect(api.delete).toHaveBeenCalledWith('/.gateway/admin/users/viewer@example.com')
      })
    })

    it('prevents deleting own account', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockUsers)
      
      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Admin User')).toBeInTheDocument()
      })

      // Find delete button for admin user (current user)
      const rows = screen.getAllByRole('row')
      const adminRow = rows.find(row => row.textContent?.includes('admin@example.com'))
      const deleteButton = adminRow?.querySelector('button[disabled]')

      expect(deleteButton).toBeDisabled()
    })
  })

  describe('Non-Admin User', () => {
    it('shows access denied for viewer user', async () => {
      // Mock the API to return empty array to avoid undefined error
      vi.mocked(api.get).mockResolvedValueOnce([])
      
      const Wrapper = createWrapper({ email: 'viewer@example.com', roles: ['viewer'] })
      render(<UsersPage />, { wrapper: Wrapper })

      expect(screen.getByText('Access Denied')).toBeInTheDocument()
      expect(screen.getByText(/don't have permission/)).toBeInTheDocument()
    })

    it('shows access denied for ops user', async () => {
      // Mock the API to return empty array to avoid undefined error
      vi.mocked(api.get).mockResolvedValueOnce([])
      
      const Wrapper = createWrapper({ email: 'ops@example.com', roles: ['ops'] })
      render(<UsersPage />, { wrapper: Wrapper })

      expect(screen.getByText('Access Denied')).toBeInTheDocument()
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
      vi.mocked(api.get).mockImplementation(() => new Promise(() => {})) // Never resolves
      
      const Wrapper = createWrapper()
      render(<UsersPage />, { wrapper: Wrapper })

      // Check for loading spinner by class
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })
  })
})