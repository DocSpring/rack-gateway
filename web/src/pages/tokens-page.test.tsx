import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { TokensPage } from './tokens-page'

const CREATE_TOKEN_RE = /Create Token/i
const COPY_TOKEN_NOW_RE = /Copy this token now/i

// Mock the API
vi.mock('../lib/api', () => ({
  api: {
    get: vi.fn(),
    post: vi.fn(),
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

// Mock clipboard API
Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn().mockResolvedValue(undefined),
  },
})

const mockTokens = [
  {
    id: 'token-1',
    name: 'CI/CD Pipeline',
    last_used: '2024-01-15T00:00:00Z',
    created_at: '2024-01-01T00:00:00Z',
    expires_at: '2026-01-01T00:00:00Z', // Active - expires in future
  },
  {
    id: 'token-2',
    name: 'Development Token',
    last_used: null,
    created_at: '2024-01-10T00:00:00Z',
    expires_at: '2023-12-31T00:00:00Z', // Expired
  },
]

const createWrapper = () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })

  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>{children}</BrowserRouter>
    </QueryClientProvider>
  )
}

describe('TokensPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Token List', () => {
    it('renders tokens list', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockTokens)

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
      vi.mocked(api.get).mockResolvedValueOnce([])

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('No API tokens created yet')).toBeInTheDocument()
      })
    })

    it('displays last used date', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockTokens)

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Jan 15, 2024')).toBeInTheDocument() // Last used
        expect(screen.getByText('Never')).toBeInTheDocument() // Never used
      })
    })
  })

  describe('Token Creation', () => {
    it('opens create token dialog', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockTokens)

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByText('Create Token'))

      expect(screen.getByText('Create API Token')).toBeInTheDocument()
      expect(screen.getByLabelText('Token Name')).toBeInTheDocument()
    })

    it('creates a new token and displays it', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockTokens)
        .mockResolvedValueOnce([
          ...mockTokens,
          {
            id: 'new-token',
            name: 'New Token',
            last_used: null,
            created_at: '2024-01-20T00:00:00Z',
            expires_at: '2026-01-20T00:00:00Z',
          },
        ])
      vi.mocked(api.post).mockResolvedValueOnce({
        id: 'new-token',
        name: 'New Token',
        token: 'gat_abc123xyz456',
        created_at: '2024-01-20T00:00:00Z',
        expires_at: '2025-01-20T00:00:00Z',
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
        expect(api.post).toHaveBeenCalledWith('/.gateway/admin/tokens', {
          name: 'New Token',
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
        .mockResolvedValueOnce(mockTokens)
        .mockResolvedValueOnce([
          ...mockTokens,
          {
            id: 'new-token',
            name: 'New Token',
            last_used: null,
            created_at: '2024-01-20T00:00:00Z',
            expires_at: '2026-01-20T00:00:00Z',
          },
        ])
      vi.mocked(api.post).mockResolvedValueOnce({
        id: 'new-token',
        name: 'New Token',
        token: 'gat_abc123xyz456',
        created_at: '2024-01-20T00:00:00Z',
        expires_at: '2025-01-20T00:00:00Z',
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

    it('validates token name is not empty', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockTokens)

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

  describe('Token Deletion', () => {
    it('deletes a token', async () => {
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockTokens)
        .mockResolvedValueOnce(mockTokens.filter((t) => t.id !== 'token-1')) // After deletion
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

      await waitFor(() => {
        expect(api.delete).toHaveBeenCalledWith('/.gateway/admin/tokens/token-1')
      })
    })
  })

  describe('Error Handling', () => {
    it('displays error when loading fails', async () => {
      vi.mocked(api.get).mockRejectedValueOnce(new Error('API Error'))

      const Wrapper = createWrapper()
      render(<TokensPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('Failed to load API tokens')).toBeInTheDocument()
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
      render(<TokensPage />, { wrapper: Wrapper })

      // Check for loading spinner by class
      const spinner = document.querySelector('.animate-spin')
      expect(spinner).toBeInTheDocument()
    })

    it('handles token creation error', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockTokens)
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
      vi.mocked(api.get).mockResolvedValueOnce(mockTokens)
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

      await waitFor(() => {
        expect(api.delete).toHaveBeenCalled()
      })

      // Token should still be visible after failed deletion
      expect(screen.getByText('CI/CD Pipeline')).toBeInTheDocument()
    })
    // moved to top-level
  })
})
