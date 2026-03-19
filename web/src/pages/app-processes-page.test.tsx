import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppProcessesPage } from './app-processes-page'

vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ app: 'rack-gateway' }),
}))

const mockFetchAppProcesses = vi.fn()
vi.mock('../lib/app-runtime', () => ({
  fetchAppProcesses: (app: string) => mockFetchAppProcesses(app),
}))

const mockApiDelete = vi.fn()
vi.mock('../lib/api', () => ({
  api: {
    delete: (...args: unknown[]) => mockApiDelete(...args),
  },
}))

const mockToast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
}))
vi.mock('../components/ui/use-toast', () => ({ toast: mockToast }))
vi.mock('@/components/ui/use-toast', () => ({ toast: mockToast }))

const mockUseAuth = vi.fn()
vi.mock('../contexts/auth-context', () => ({
  useAuth: () => mockUseAuth(),
}))

function renderWithClient(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })

  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

describe('AppProcessesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders process rows without stop actions for viewers', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['viewer'] } })
    mockFetchAppProcesses.mockResolvedValue([
      {
        id: 'p-worker-gj-1',
        service: 'worker-gj',
        status: 'running',
        release: 'R1',
        started: '2026-03-20T00:00:00Z',
      },
    ])

    renderWithClient(<AppProcessesPage />)

    await waitFor(() => expect(mockFetchAppProcesses).toHaveBeenCalledWith('rack-gateway'))
    expect(await screen.findByText('p-worker-gj-1')).toBeInTheDocument()
    expect(screen.queryByRole('columnheader', { name: 'Actions' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('stop-process-p-worker-gj-1')).not.toBeInTheDocument()
  })

  it('allows privileged users to stop a process and refreshes the list', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['ops'] } })
    mockFetchAppProcesses
      .mockResolvedValueOnce([
        {
          id: 'p-worker-gj-1',
          service: 'worker-gj',
          status: 'running',
          release: 'R1',
          started: '2026-03-20T00:00:00Z',
        },
        {
          id: 'p-web-1',
          service: 'web',
          status: 'running',
          release: 'R1',
          started: '2026-03-20T01:00:00Z',
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'p-web-1',
          service: 'web',
          status: 'running',
          release: 'R1',
          started: '2026-03-20T01:00:00Z',
        },
      ])
    mockApiDelete.mockResolvedValue({})

    renderWithClient(<AppProcessesPage />)

    const stopButton = await screen.findByTestId('stop-process-p-worker-gj-1')
    fireEvent.click(stopButton)

    await waitFor(() =>
      expect(mockApiDelete).toHaveBeenCalledWith(
        '/api/v1/convox/apps/rack-gateway/processes/p-worker-gj-1'
      )
    )
    await waitFor(() => expect(mockFetchAppProcesses).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByText('p-worker-gj-1')).not.toBeInTheDocument())
    expect(mockToast.success).toHaveBeenCalledWith('Stopped process p-worker-gj-1')
  })
})
