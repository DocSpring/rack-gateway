import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { AuditPage } from './audit-page'

// Hoisted regex for Biome performance rule
const FAILED_LOAD_REGEX = /Failed to load audit logs/i

// Mock the API
vi.mock('../lib/api', () => ({
  api: {
    get: vi.fn(),
  },
}))

const mockLogs = [
  {
    id: 1,
    timestamp: '2024-01-15T10:30:00Z',
    user_email: 'admin@example.com',
    user_name: 'Admin User',
    action_type: 'user_management',
    action: 'user.create',
    resource: 'newuser@example.com',
    details: '{}',
    ip_address: '192.168.1.1',
    user_agent: 'Mozilla/5.0',
    status: 'success',
    response_time_ms: 150,
  },
  {
    id: 2,
    timestamp: '2024-01-15T10:25:00Z',
    user_email: 'viewer@example.com',
    user_name: 'Viewer User',
    action_type: 'convox_api',
    action: 'apps.list',
    resource: '/apps',
    details: '',
    ip_address: '192.168.1.2',
    user_agent: 'convox-cli/3.0',
    status: 'success',
    response_time_ms: 75,
  },
  {
    id: 3,
    timestamp: '2024-01-15T10:20:00Z',
    user_email: 'ops@example.com',
    user_name: 'Ops User',
    action_type: 'auth',
    action: 'auth.login',
    resource: '',
    details: '',
    ip_address: '192.168.1.3',
    user_agent: 'Chrome/120',
    status: 'failed',
    response_time_ms: 200,
  },
  {
    id: 4,
    timestamp: '2024-01-15T10:15:00Z',
    user_email: 'hacker@evil.com',
    user_name: '',
    action_type: 'auth',
    action: 'auth.attempt',
    resource: '',
    details: '',
    ip_address: '10.0.0.1',
    user_agent: 'curl/7.68',
    status: 'blocked',
    response_time_ms: 5,
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

describe('AuditPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Audit Log Display', () => {
    it('renders audit logs table', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockLogs)

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
        expect(screen.getByText('viewer@example.com')).toBeInTheDocument()
        expect(screen.getByText('ops@example.com')).toBeInTheDocument()
      })

      // Check actions
      expect(screen.getByText('user.create')).toBeInTheDocument()
      expect(screen.getByText('apps.list')).toBeInTheDocument()
      expect(screen.getByText('auth.login')).toBeInTheDocument()
    })

    it('displays status badges correctly', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockLogs)

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        // There are 2 success statuses in the mock data
        const successBadges = screen.getAllByText('success')
        expect(successBadges).toHaveLength(2)
        expect(screen.getByText('failed')).toBeInTheDocument()
        expect(screen.getByText('blocked')).toBeInTheDocument()
      })
    })

    it('shows empty state when no logs', async () => {
      vi.mocked(api.get).mockResolvedValueOnce([])

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('No audit logs found')).toBeInTheDocument()
      })
    })
  })

  describe('Statistics', () => {
    it('calculates and displays statistics correctly', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockLogs)

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        // Total events
        expect(screen.getByText('4')).toBeInTheDocument()

        // Success rate: 2 success out of 4 = 50%
        expect(screen.getByText('50%')).toBeInTheDocument()

        // Failed/Blocked: 1 failed + 1 blocked = 2
        expect(screen.getByText('2')).toBeInTheDocument()

        // Average response time: (150 + 75 + 200 + 5) / 4 = 107.5 ≈ 108ms
        // There are multiple ms values in the table, just check one exists
        expect(screen.getByText('108ms')).toBeInTheDocument()
      })
    })
  })

  describe('Filtering', () => {
    it('filters by action type', async () => {
      // Mock initial load and filtered load
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockLogs)
        .mockResolvedValueOnce(mockLogs.filter((l) => l.action_type === 'auth'))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Find and click action type filter by its id
      const actionTypeSelect = document.getElementById('action-type')
      if (!actionTypeSelect) {
        throw new Error('Action type select not found')
      }
      fireEvent.click(actionTypeSelect)

      // Select "Authentication"
      const authOption = screen.getByText('Authentication')
      fireEvent.click(authOption)

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledWith(expect.stringContaining('action_type=auth'))
      })
    })

    it('filters by status', async () => {
      // Mock initial load and filtered load
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockLogs)
        .mockResolvedValueOnce(mockLogs.filter((l) => l.status === 'failed'))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Find and click status filter by its id
      const statusSelect = document.getElementById('status')
      if (!statusSelect) {
        throw new Error('Status select not found')
      }
      fireEvent.click(statusSelect)

      // Select "Failed"
      const failedOption = screen.getByText('Failed')
      fireEvent.click(failedOption)

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledWith(expect.stringContaining('status=failed'))
      })
    })

    it('filters by date range', async () => {
      // Mock initial load and filtered load
      vi.mocked(api.get).mockResolvedValueOnce(mockLogs).mockResolvedValueOnce(mockLogs)

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Find and click date range filter by its id
      const dateRangeSelect = document.getElementById('date-range')
      if (!dateRangeSelect) {
        throw new Error('Date range select not found')
      }
      fireEvent.click(dateRangeSelect)

      // Select "Last 24 Hours"
      const last24Option = screen.getByText('Last 24 Hours')
      fireEvent.click(last24Option)

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledWith(expect.stringContaining('range=24h'))
      })
    })

    it('searches by text', async () => {
      // Mock initial load and search results
      vi.mocked(api.get)
        .mockResolvedValueOnce(mockLogs)
        .mockResolvedValueOnce(mockLogs.filter((l) => l.user_email.includes('admin')))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Enter search term
      const searchInput = screen.getByPlaceholderText('User, resource, action...')
      fireEvent.change(searchInput, { target: { value: 'admin' } })

      // Wait for debounce
      await waitFor(
        () => {
          expect(api.get).toHaveBeenCalledWith(expect.stringContaining('search=admin'))
        },
        { timeout: 1000 }
      )
    })
  })

  describe('Actions', () => {
    it('refreshes data when clicking refresh button', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockLogs)

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Clear mock to track new calls
      vi.mocked(api.get).mockClear()
      vi.mocked(api.get).mockResolvedValueOnce(mockLogs)

      // Click refresh
      fireEvent.click(screen.getByText('Refresh'))

      await waitFor(() => {
        expect(api.get).toHaveBeenCalled()
      })
    })

    it('exports CSV when clicking export button', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockLogs)

      // Mock document methods for download
      const createElementSpy = vi.spyOn(document, 'createElement')
      const appendChildSpy = vi.spyOn(document.body, 'appendChild')
      const removeChildSpy = vi.spyOn(document.body, 'removeChild')

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Click export
      fireEvent.click(screen.getByText('Export CSV'))

      expect(createElementSpy).toHaveBeenCalledWith('a')
      expect(appendChildSpy).toHaveBeenCalled()
      expect(removeChildSpy).toHaveBeenCalled()
    })
  })

  describe('Error Handling', () => {
    it('displays error when loading fails', async () => {
      vi.mocked(api.get).mockRejectedValueOnce(new Error('API Error'))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText(FAILED_LOAD_REGEX)).toBeInTheDocument()
      })
    })
  })

  describe('IP Addresses', () => {
    it('displays IP addresses', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(mockLogs)

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('192.168.1.1')).toBeInTheDocument()
        expect(screen.getByText('192.168.1.2')).toBeInTheDocument()
        expect(screen.getByText('192.168.1.3')).toBeInTheDocument()
        expect(screen.getByText('10.0.0.1')).toBeInTheDocument()
      })
    })
  })
})
