import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppEnvPage } from './app-env-page'

const MASK = '********************'

vi.mock('@tanstack/react-router', () => ({
  useParams: () => ({ app: 'myapp' }),
}))

const mockFetchAppEnv = vi.fn()
const mockFetchAppEnvValue = vi.fn()
const mockUpdateAppEnv = vi.fn()

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api')
  return {
    ...actual,
    fetchAppEnv: (app: string) => mockFetchAppEnv(app),
    fetchAppEnvValue: (app: string, key: string, includeSecret?: boolean) =>
      mockFetchAppEnvValue(app, key, includeSecret),
    updateAppEnv: (app: string, set: Record<string, string>, remove: string[]) =>
      mockUpdateAppEnv(app, set, remove),
  }
})

const mockToast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
}))
vi.mock('@/components/ui/use-toast', () => ({ toast: mockToast }))

const mockUseAuth = vi.fn()
vi.mock('../contexts/auth-context', () => ({
  useAuth: () => mockUseAuth(),
}))

function renderWithClient(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

describe('AppEnvPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetchAppEnv.mockResolvedValue({ FOO: 'bar', SECRET_KEY: MASK })
    mockToast.success.mockReset()
    mockToast.error.mockReset()
    mockToast.warning.mockReset()
    mockToast.info.mockReset()
  })

  it('renders environment variables in read-only mode for non-editors', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['viewer'] } })

    renderWithClient(<AppEnvPage />)

    await waitFor(() => expect(mockFetchAppEnv).toHaveBeenCalledWith('myapp'))

    const addButton = await screen.findByRole('button', {
      name: /Add Variable/i,
    })
    expect(addButton).toBeDisabled()

    const keyInput = (await screen.findByDisplayValue('FOO')) as HTMLInputElement
    expect(keyInput.disabled).toBe(true)

    const valueInput = (await screen.findByDisplayValue('bar')) as HTMLInputElement
    expect(valueInput.disabled).toBe(true)

    const secretInput = screen.getByPlaceholderText('********') as HTMLInputElement
    expect(secretInput.type).toBe('password')
    expect(secretInput.disabled).toBe(true)
  })

  it('allows editors to update environment values and saves changes', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['deployer'] } })
    mockUpdateAppEnv.mockResolvedValue({
      env: { FOO: 'baz', SECRET_KEY: MASK },
    })

    renderWithClient(<AppEnvPage />)

    await waitFor(() => expect(mockFetchAppEnv).toHaveBeenCalled())

    const valueInput = await screen.findByDisplayValue('bar')
    fireEvent.change(valueInput, { target: { value: 'baz' } })

    const saveButton = screen.getByRole('button', { name: /Save Changes/i })
    fireEvent.click(saveButton)

    await waitFor(() => expect(mockUpdateAppEnv).toHaveBeenCalledWith('myapp', { FOO: 'baz' }, []))
  })

  it('sends removed keys when deleting variables', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['deployer'] } })
    mockUpdateAppEnv.mockResolvedValue({ env: {} })

    renderWithClient(<AppEnvPage />)

    await waitFor(() => expect(mockFetchAppEnv).toHaveBeenCalled())

    const deleteButtons = await screen.findAllByLabelText('Delete env var')
    fireEvent.click(deleteButtons[0])

    const saveButton = screen.getByRole('button', { name: /Save Changes/i })
    fireEvent.click(saveButton)

    await waitFor(() => expect(mockUpdateAppEnv).toHaveBeenCalledWith('myapp', {}, ['FOO']))
  })

  it('reveals secret values on demand and hides again', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['admin'] } })
    mockFetchAppEnvValue.mockResolvedValue('shhh')

    renderWithClient(<AppEnvPage />)

    await waitFor(() => expect(mockFetchAppEnv).toHaveBeenCalled())

    const initialValueInputs = await screen.findAllByLabelText('Environment value')
    expect((initialValueInputs[1] as HTMLInputElement).type).toBe('password')

    const revealButtons = await screen.findAllByRole('button', {
      name: /Reveal secret/i,
    })
    fireEvent.click(revealButtons[0])

    await waitFor(() =>
      expect(mockFetchAppEnvValue).toHaveBeenCalledWith('myapp', 'SECRET_KEY', true)
    )

    await waitFor(() => expect(screen.getByDisplayValue('shhh')).toBeInTheDocument())
    await waitFor(() => {
      const revealedInputs = screen.getAllByLabelText('Environment value')
      expect((revealedInputs[1] as HTMLInputElement).type).toBe('text')
    })

    const hideButton = screen.getByRole('button', { name: /Hide secret/i })
    fireEvent.click(hideButton)

    await waitFor(() => {
      const hiddenInputs = screen.getAllByLabelText('Environment value')
      expect((hiddenInputs[1] as HTMLInputElement).type).toBe('password')
    })
    const showButton = screen.getByRole('button', { name: /Reveal secret/i })
    expect(showButton).toBeEnabled()
  })

  it('shows an error when masked sentinel is used for a new secret', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['admin'] } })
    mockFetchAppEnv.mockResolvedValueOnce({})
    const maskedError = new Error('Masked secret value submitted without an existing secret.')
    mockUpdateAppEnv.mockRejectedValueOnce(maskedError)

    renderWithClient(<AppEnvPage />)

    await waitFor(() => expect(mockFetchAppEnv).toHaveBeenCalled())

    const addButton = await screen.findByRole('button', {
      name: /Add Variable/i,
    })
    fireEvent.click(addButton)

    const keyInputs = screen.getAllByLabelText('Environment key')
    fireEvent.change(keyInputs[0], { target: { value: 'SECRET_KEY' } })

    const valueInputs = screen.getAllByLabelText('Environment value')
    fireEvent.change(valueInputs[0], { target: { value: MASK } })

    const saveButton = screen.getByRole('button', { name: /Save Changes/i })
    fireEvent.click(saveButton)

    await waitFor(() =>
      expect(mockUpdateAppEnv).toHaveBeenCalledWith('myapp', { SECRET_KEY: MASK }, [])
    )
    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith(maskedError.message))
  })

  it('reverts local changes after cancel', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['deployer'] } })
    mockFetchAppEnv.mockResolvedValueOnce({ FOO: 'bar', SECRET_KEY: MASK })
    mockFetchAppEnv.mockResolvedValueOnce({ FOO: 'bar', SECRET_KEY: MASK })

    renderWithClient(<AppEnvPage />)

    const fooInput = await screen.findByDisplayValue('bar')
    fireEvent.change(fooInput, { target: { value: 'baz' } })

    const cancelButton = screen.getByRole('button', { name: /Cancel/i })
    fireEvent.click(cancelButton)

    await waitFor(() => expect(mockFetchAppEnv).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.getByDisplayValue('bar')).toBeInTheDocument())
    expect(screen.queryByDisplayValue('baz')).toBeNull()
  })

  it('syncs with backend after save completes', async () => {
    mockUseAuth.mockReturnValue({ user: { roles: ['deployer'] } })
    mockFetchAppEnv.mockResolvedValueOnce({ FOO: 'bar', SECRET_KEY: MASK })
    mockFetchAppEnv.mockResolvedValueOnce({ FOO: 'baz', SECRET_KEY: MASK })
    mockUpdateAppEnv.mockResolvedValue({
      env: { FOO: 'baz', SECRET_KEY: MASK },
      release_id: 'R2',
    })

    renderWithClient(<AppEnvPage />)

    const fooInput = await screen.findByDisplayValue('bar')
    fireEvent.change(fooInput, { target: { value: 'baz' } })

    const saveButton = screen.getByRole('button', { name: /Save Changes/i })
    fireEvent.click(saveButton)

    await waitFor(() => expect(mockUpdateAppEnv).toHaveBeenCalled())
    await waitFor(() => expect(mockFetchAppEnv).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.getByDisplayValue('baz')).toBeInTheDocument())
  })
})
