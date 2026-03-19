import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppServicesPage } from './app-services-page'

vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ app: 'rack-gateway' }),
}))

const mockFetchAppServices = vi.fn()
const mockFetchAppProcesses = vi.fn()
vi.mock('../lib/app-runtime', async () => {
  const actual = await vi.importActual<typeof import('../lib/app-runtime')>('../lib/app-runtime')
  return {
    ...actual,
    fetchAppServices: (app: string) => mockFetchAppServices(app),
    fetchAppProcesses: (app: string) => mockFetchAppProcesses(app),
  }
})

const mockApiPut = vi.fn()
vi.mock('../lib/api', () => ({
  api: {
    put: (...args: unknown[]) => mockApiPut(...args),
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

describe('AppServicesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders service scale and process counts without edit controls for viewers', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['viewer'] } })
    mockFetchAppServices.mockResolvedValue([
      { name: 'web', count: 3, cpu: 256, memory: 512 },
      { name: 'worker-gj', count: 1, cpu: 256, memory: 512 },
    ])
    mockFetchAppProcesses.mockResolvedValue([
      {
        id: 'p-web-1',
        service: 'web',
        status: 'running',
        release: 'R1',
      },
      {
        id: 'p-web-2',
        service: 'web',
        status: 'running',
        release: 'R1',
      },
      {
        id: 'p-worker-gj-1',
        service: 'worker-gj',
        status: 'running',
        release: 'R1',
      },
    ])

    renderWithClient(<AppServicesPage />)

    await waitFor(() => expect(mockFetchAppServices).toHaveBeenCalledWith('rack-gateway'))
    await waitFor(() => expect(mockFetchAppProcesses).toHaveBeenCalledWith('rack-gateway'))

    const workerRow = await screen.findByTestId('service-row-worker-gj')
    expect(within(workerRow).getByText('worker-gj')).toBeInTheDocument()
    expect(within(workerRow).getAllByText('1')).toHaveLength(2)
    expect(screen.queryByTestId('service-edit-worker-gj')).not.toBeInTheDocument()
  })

  it('allows deployers to scale a service and refreshes the table', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['deployer'] } })
    mockFetchAppServices
      .mockResolvedValueOnce([{ name: 'worker-gj', count: 1, cpu: 256, memory: 512 }])
      .mockResolvedValueOnce([{ name: 'worker-gj', count: 3, cpu: 256, memory: 512 }])
    mockFetchAppProcesses
      .mockResolvedValueOnce([
        {
          id: 'p-worker-gj-1',
          service: 'worker-gj',
          status: 'running',
          release: 'R1',
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'p-worker-gj-1',
          service: 'worker-gj',
          status: 'running',
          release: 'R1',
        },
        {
          id: 'p-worker-gj-2',
          service: 'worker-gj',
          status: 'running',
          release: 'R1',
        },
        {
          id: 'p-worker-gj-3',
          service: 'worker-gj',
          status: 'running',
          release: 'R1',
        },
      ])
    mockApiPut.mockResolvedValue({})

    renderWithClient(<AppServicesPage />)

    const editButton = await screen.findByTestId('service-edit-worker-gj')
    fireEvent.click(editButton)

    const scaleInput = screen.getByLabelText('Scale for worker-gj')
    fireEvent.change(scaleInput, { target: { value: '3' } })
    fireEvent.click(screen.getByTestId('service-save-worker-gj'))

    await waitFor(() =>
      expect(mockApiPut).toHaveBeenCalledWith(
        '/api/v1/convox/apps/rack-gateway/services/worker-gj',
        undefined,
        { params: { count: 3 } }
      )
    )
    await waitFor(() => expect(mockFetchAppServices).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(mockFetchAppProcesses).toHaveBeenCalledTimes(2))

    const workerRow = await screen.findByTestId('service-row-worker-gj')
    await waitFor(() => expect(within(workerRow).getAllByText('3')).toHaveLength(2))
    expect(mockToast.success).toHaveBeenCalledWith('Scaled worker-gj to 3')
  })
})
