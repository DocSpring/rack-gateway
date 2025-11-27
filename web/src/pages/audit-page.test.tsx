import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { DEFAULT_PER_PAGE } from '../lib/constants'
import { AuditPage } from './audit-page'

// Hoisted regex for Biome performance rule
const FAILED_LOAD_REGEX = /Failed to load audit logs/i
const PAGE_FIVE_REGEX = /page=5/
const PAGE_TWO_REGEX = /page=2/

// Mock the API
vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      get: vi.fn(),
    },
  }
})

const mockLogs = [
  {
    id: 1,
    timestamp: '2024-01-15T10:30:00Z',
    user_email: 'admin@example.com',
    user_name: 'Admin User',
    action_type: 'users',
    action: 'user.create',
    resource: 'newuser@example.com',
    resource_type: 'user',
    details: '{}',
    ip_address: '192.168.1.1',
    user_agent: 'Mozilla/5.0',
    status: 'success',
    response_time_ms: 150,
    event_count: 1,
  },
  {
    id: 2,
    timestamp: '2024-01-15T10:25:00Z',
    user_email: 'viewer@example.com',
    user_name: 'Viewer User',
    action_type: 'convox',
    action: 'apps.list',
    resource: '/apps',
    resource_type: 'app',
    details: '',
    ip_address: '192.168.1.2',
    user_agent: 'convox-cli/3.0',
    status: 'success',
    response_time_ms: 75,
    event_count: 4,
  },
  {
    id: 3,
    timestamp: '2024-01-15T10:20:00Z',
    user_email: 'ops@example.com',
    user_name: 'Ops User',
    action_type: 'auth',
    action: 'auth.login',
    resource: '',
    resource_type: 'auth',
    details: '',
    ip_address: '192.168.1.3',
    user_agent: 'Chrome/120',
    status: 'failed',
    response_time_ms: 200,
    event_count: 1,
  },
  {
    id: 4,
    timestamp: '2024-01-15T10:15:00Z',
    user_email: 'hacker@evil.com',
    user_name: '',
    action_type: 'auth',
    action: 'auth.attempt',
    resource: '',
    resource_type: 'auth',
    details: '',
    ip_address: '10.0.0.1',
    user_agent: 'curl/7.68',
    status: 'blocked',
    response_time_ms: 5,
    event_count: 1,
  },
  {
    id: 5,
    timestamp: '2024-01-15T10:10:00Z',
    user_email: 'cibot@example.com',
    user_name: 'CI Bot Owner',
    api_token_id: 42,
    api_token_name: 'CI Deploy Token',
    action_type: 'convox',
    action: 'build.read',
    resource: 'docspring',
    resource_type: 'app',
    details: '{"method":"GET"}',
    ip_address: '203.0.113.10',
    user_agent: 'convox.go/dev',
    status: 'denied',
    response_time_ms: 1,
    event_count: 1,
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
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

const makeResponse = (
  logs: typeof mockLogs,
  options?: Partial<{ total: number; page: number; limit: number }>
) => ({
  logs,
  total: options?.total ?? logs.length,
  page: options?.page ?? 1,
  limit: options?.limit ?? DEFAULT_PER_PAGE,
})

describe('AuditPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    window.history.replaceState(null, '', '/')
  })

  describe('Audit Log Display', () => {
    it('renders audit logs table', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
        expect(screen.getByText('viewer@example.com')).toBeInTheDocument()
        expect(screen.getByText('ops@example.com')).toBeInTheDocument()
        expect(screen.getByText('CI Deploy Token')).toBeInTheDocument()
      })

      // Check actions
      expect(screen.getByText('user.create')).toBeInTheDocument()
      expect(screen.getByText('apps.list')).toBeInTheDocument()
      expect(screen.getByText('auth.login')).toBeInTheDocument()
      expect(screen.getByText('×4')).toBeInTheDocument()
      expect(screen.queryByText('×1')).not.toBeInTheDocument()
      expect(screen.getAllByText('API Token')).not.toHaveLength(0)
      expect(screen.getByText(/Owner: cibot@example.com/)).toBeInTheDocument()
    })

    it('displays aggregated logs with last_seen instead of timestamp', async () => {
      const aggregatedLogs = [
        {
          id: 101,
          // No timestamp field
          last_seen: '2024-02-20T12:00:00Z',
          first_seen: '2024-02-20T11:00:00Z',
          user_email: 'agg@example.com',
          action_type: 'test',
          action: 'aggregated.event',
          status: 'success',
          event_count: 10,
          avg_response_time_ms: 50,
          response_time_ms: 50,
        },
      ]
      // @ts-expect-error - aggregated logs mock is partial
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(aggregatedLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('agg@example.com')).toBeInTheDocument()
      })

      // Verify time is displayed (TimeAgo usually renders "x time ago" or date)
      // Since we mocked the date to Feb 20, 2024, it should render a date or relative time.
      // We just check if the row rendered without error and contains the action
      expect(screen.getByText('aggregated.event')).toBeInTheDocument()

      // Click to open detail dialog and check timestamp
      const row = screen.getByText('aggregated.event').closest('tr')
      expect(row).not.toBeNull()
      if (row) fireEvent.click(row)

      await waitFor(() => {
        expect(screen.getByText('Audit Log')).toBeInTheDocument()
      })

      // Check that the detail dialog shows the timestamp from last_seen
      expect(screen.getByText(/2024-02-20T12:00:00.000Z/)).toBeInTheDocument()

      // Check that response time is displayed (using avg)
      const responseTimeLabel = screen.getByText('Response Time:')
      expect(responseTimeLabel.parentElement).toHaveTextContent('50 ms (avg)')
    })

    it('shows event count in detail dialog', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('apps.list')).toBeInTheDocument()
      })

      const actionBadge = screen.getByText('apps.list')
      const row = actionBadge.closest('tr')
      expect(row).not.toBeNull()
      if (!row) {
        throw new Error('expected table row for apps.list')
      }
      fireEvent.click(row)

      await waitFor(() => {
        expect(screen.getByText('Audit Log')).toBeInTheDocument()
      })

      expect(screen.getByTestId('audit-event-count')).toHaveTextContent('4')
    })

    it('renders API token metadata in detail dialog', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('CI Deploy Token')).toBeInTheDocument()
      })

      const tokenCell = screen.getByText('CI Deploy Token')
      const tokenRow = tokenCell.closest('tr')
      expect(tokenRow).not.toBeNull()
      if (!tokenRow) {
        throw new Error('expected table row for token entry')
      }
      fireEvent.click(tokenRow)

      await waitFor(() => {
        expect(screen.getByText('Audit Log')).toBeInTheDocument()
      })

      const dialog = screen.getByRole('dialog')
      expect(within(dialog).getByText('Token:')).toBeInTheDocument()
      expect(within(dialog).getByText('CI Deploy Token')).toBeInTheDocument()

      expect(
        within(dialog).getByText((_, element) => {
          if (!element) {
            return false
          }
          return element.textContent === 'Token ID: 42'
        })
      ).toBeInTheDocument()
      expect(
        within(dialog).getByText((_, element) => {
          if (!element) {
            return false
          }
          return element.textContent === 'Owner: cibot@example.com (CI Bot Owner)'
        })
      ).toBeInTheDocument()
    })

    it('displays status badges correctly', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

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
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse([]))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('No audit logs found')).toBeInTheDocument()
      })
    })
  })

  describe('Statistics', () => {
    it('calculates and displays statistics correctly', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        const totalLogsCard = screen.getByText('Total Logs').parentElement?.parentElement
        if (!totalLogsCard) {
          throw new Error('Total Logs card not found')
        }
        expect(totalLogsCard).toHaveTextContent('5')

        const successRateCard = screen.getByText('Success Rate').parentElement?.parentElement
        if (!successRateCard) {
          throw new Error('Success Rate card not found')
        }
        expect(successRateCard).toHaveTextContent('63%')

        const failedCard = screen.getByText('Failed/Denied').parentElement?.parentElement
        if (!failedCard) {
          throw new Error('Failed/Denied card not found')
        }
        expect(failedCard).toHaveTextContent('3')

        const responseCard = screen.getByText('Avg Response Time').parentElement?.parentElement
        if (!responseCard) {
          throw new Error('Avg Response Time card not found')
        }
        expect(responseCard).toHaveTextContent('82ms')
      })
    })
  })

  describe('Filtering', () => {
    it('filters by action type', async () => {
      // Mock initial load and filtered load
      vi.mocked(api.get)
        .mockResolvedValueOnce(makeResponse(mockLogs))
        .mockResolvedValueOnce(makeResponse(mockLogs.filter((l) => l.action_type === 'auth')))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Find and change action type filter by its id
      const actionTypeSelect = document.getElementById('action-type')
      if (!actionTypeSelect) {
        throw new Error('Action type select not found')
      }

      // Select "Authentication" (value is 'auth')
      fireEvent.change(actionTypeSelect, { target: { value: 'auth' } })

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledWith(expect.stringContaining('action_type=auth'))
      })
    })

    it('filters by resource type', async () => {
      // Mock initial load and filtered load
      vi.mocked(api.get)
        .mockResolvedValueOnce(makeResponse(mockLogs))
        .mockResolvedValueOnce(makeResponse(mockLogs.filter((l) => l.resource_type === 'user')))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Find and change resource type filter by its id
      const rtSelect = document.getElementById('resource-type')
      if (!rtSelect) {
        throw new Error('Resource type select not found')
      }

      // Select "API Token" (value is 'api_token')
      fireEvent.change(rtSelect, { target: { value: 'api_token' } })

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledWith(expect.stringContaining('resource_type=api_token'))
      })
    })

    it('filters by status', async () => {
      // Mock initial load and filtered load
      vi.mocked(api.get)
        .mockResolvedValueOnce(makeResponse(mockLogs))
        .mockResolvedValueOnce(makeResponse(mockLogs.filter((l) => l.status === 'failed')))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Find and change status filter by its id
      const statusSelect = document.getElementById('status')
      if (!statusSelect) {
        throw new Error('Status select not found')
      }

      // Select "Failed" (value is 'failed')
      fireEvent.change(statusSelect, { target: { value: 'failed' } })

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledWith(expect.stringContaining('status=failed'))
      })
    })

    it('filters by date range', async () => {
      // Mock initial load and filtered load
      vi.mocked(api.get)
        .mockResolvedValueOnce(makeResponse(mockLogs))
        .mockResolvedValueOnce(makeResponse(mockLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Find and change date range filter by its id
      const dateRangeSelect = document.getElementById('date-range')
      if (!dateRangeSelect) {
        throw new Error('Date range select not found')
      }

      // Select "Last 24 Hours" (value is '24h')
      fireEvent.change(dateRangeSelect, { target: { value: '24h' } })

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledWith(expect.stringContaining('range=24h'))
      })
    })

    it('searches by text', async () => {
      // Mock initial load and search results
      vi.mocked(api.get)
        .mockResolvedValueOnce(makeResponse(mockLogs))
        .mockResolvedValueOnce(makeResponse(mockLogs.filter((l) => l.user_email.includes('admin'))))

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

  describe('URL synchronization', () => {
    it('respects the page query param on initial load', async () => {
      window.history.replaceState(null, '', '/?page=3')
      vi.mocked(api.get).mockResolvedValueOnce(
        makeResponse(mockLogs, {
          total: 350,
          page: 3,
          limit: DEFAULT_PER_PAGE,
        })
      )

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledWith(expect.stringContaining('page=3'))
      })

      await waitFor(() => {
        expect(window.location.search).toBe('?page=3')
      })
    })

    it('clamps the page to the available total when the response has fewer pages', async () => {
      window.history.replaceState(null, '', '/?page=5')
      vi.mocked(api.get)
        .mockResolvedValueOnce(makeResponse(mockLogs, { total: 20, page: 5 }))
        .mockResolvedValueOnce(makeResponse(mockLogs, { total: 20, page: 2 }))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(
        () => {
          expect(api.get).toHaveBeenCalledWith(expect.stringMatching(PAGE_FIVE_REGEX))
        },
        { timeout: 2000 }
      )

      await waitFor(
        () => {
          expect(api.get).toHaveBeenCalledWith(expect.stringMatching(PAGE_TWO_REGEX))
        },
        { timeout: 2000 }
      )

      await waitFor(() => {
        expect(window.location.search).toBe('?page=2')
      })
    })

    it('syncs filters into the URL search params', async () => {
      vi.mocked(api.get).mockImplementation(async () => makeResponse(mockLogs))
      const replaceSpy = vi.spyOn(window.history, 'replaceState')
      const Wrapper = createWrapper()
      try {
        render(<AuditPage />, { wrapper: Wrapper })

        await waitFor(() => {
          expect(api.get).toHaveBeenCalled()
        })

        const actionTypeSelect = document.getElementById('action-type')
        if (!actionTypeSelect) {
          throw new Error('Action type select not found')
        }
        fireEvent.change(actionTypeSelect, { target: { value: 'auth' } })

        await waitFor(() => {
          expect(window.location.search).toBe('?action_type=auth')
        })
      } finally {
        replaceSpy.mockRestore()
      }
    })
  })

  describe('Custom range', () => {
    it('applies custom bounds to API requests and URL params', async () => {
      vi.mocked(api.get).mockResolvedValue(makeResponse(mockLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(api.get).toHaveBeenCalledTimes(1)
      })

      const dateRangeSelect = document.getElementById('date-range')
      if (!dateRangeSelect) {
        throw new Error('Date range select not found')
      }
      fireEvent.change(dateRangeSelect, { target: { value: 'custom' } })

      const startDateInput = screen.getByLabelText('Start') as HTMLInputElement
      const startTimeInput = screen.getByLabelText('Start time') as HTMLInputElement
      const endDateInput = screen.getByLabelText('End') as HTMLInputElement
      const endTimeInput = screen.getByLabelText('End time') as HTMLInputElement

      fireEvent.change(startDateInput, { target: { value: '2025-01-15' } })
      fireEvent.change(startTimeInput, { target: { value: '09:30' } })
      fireEvent.change(endDateInput, { target: { value: '2025-01-16' } })
      fireEvent.change(endTimeInput, { target: { value: '11:15' } })

      await waitFor(() => {
        const lastCall = vi.mocked(api.get).mock.calls.at(-1)?.[0]
        expect(lastCall).toBeDefined()
        const decoded = decodeURIComponent(String(lastCall))
        expect(decoded).toContain('range=custom')
        expect(decoded).toContain('start=')
        expect(decoded).toContain('end=')
      })

      expect(window.location.search).toContain('range=custom')
      expect(window.location.search).toContain('start=')
      expect(window.location.search).toContain('end=')
    })

    it('hydrates inputs from custom range query params', async () => {
      window.history.replaceState(
        null,
        '',
        '/?range=custom&start=2025-01-10T12:00&end=2025-01-11T08:45'
      )
      vi.mocked(api.get).mockResolvedValue(makeResponse(mockLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(api.get).toHaveBeenCalled()
      })

      const startDateInput = screen.getByLabelText('Start') as HTMLInputElement
      const startTimeInput = screen.getByLabelText('Start time') as HTMLInputElement
      const endDateInput = screen.getByLabelText('End') as HTMLInputElement
      const endTimeInput = screen.getByLabelText('End time') as HTMLInputElement

      expect(startDateInput.value).toBe('2025-01-10')
      expect(startTimeInput.value).toBe('12:00')
      expect(endDateInput.value).toBe('2025-01-11')
      expect(endTimeInput.value).toBe('08:45')

      const lastCall = vi.mocked(api.get).mock.calls.at(-1)?.[0]
      expect(lastCall).toBeDefined()
      const decoded = decodeURIComponent(String(lastCall))
      expect(decoded).toContain('range=custom')
      expect(decoded).toContain('start=')
      expect(decoded).toContain('end=')
    })
  })

  describe('Actions', () => {
    it('refreshes data when clicking refresh button', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

      const Wrapper = createWrapper()
      render(<AuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      })

      // Clear mock to track new calls
      vi.mocked(api.get).mockClear()
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

      // Click refresh
      fireEvent.click(screen.getByText('Refresh'))

      await waitFor(() => {
        expect(api.get).toHaveBeenCalled()
      })
    })

    it('exports CSV when clicking export button', async () => {
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

      // Mock document methods for download
      const createElementSpy = vi.spyOn(document, 'createElement')
      const appendChildSpy = vi.spyOn(document.body, 'appendChild')
      const removeChildSpy = vi.spyOn(document.body, 'removeChild')
      // Prevent jsdom from attempting navigation on anchor.click()
      const anchorClickSpy = vi
        .spyOn(HTMLAnchorElement.prototype, 'click')
        .mockImplementation(() => {
          /* suppress navigation in test */
        })

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
      anchorClickSpy.mockRestore()
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
      vi.mocked(api.get).mockResolvedValueOnce(makeResponse(mockLogs))

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
