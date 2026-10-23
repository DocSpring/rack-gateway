import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../lib/api'
import { IntegrationsPage } from './integrations-page'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    api: { ...actual.api, get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  }
})

vi.mock('../contexts/auth-context', () => ({
  useAuth: () => ({
    user: {
      email: 'admin@example.com',
      name: 'Admin User',
      roles: ['admin'],
      integrations: { slack: true, github: false, circleci: false },
    },
  }),
}))

vi.mock('../components/ui/use-toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const INTEGRATION_PATH = '/api/v1/integrations/slack'
const CHANNELS_PATH = '/api/v1/integrations/slack/channels'
const PATTERN_PLACEHOLDER = 'e.g., mfa.*, deploy-approval-request.*'

function security(...actions: string[]) {
  return { security: { id: 'C123', name: 'security', actions } }
}

function integration(channelActions: ReturnType<typeof security>) {
  return {
    id: 1,
    workspace_id: 'T123',
    workspace_name: 'Workspace',
    channel_actions: channelActions,
    alert_deploy_approvals_enabled: false,
    alert_deploy_approvals_channel_id: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

// mockIntegrationReads answers the first integration read with the given configuration and later ones with
// whatever the test resolves them to.
function mockIntegrationReads(initial: ReturnType<typeof security>) {
  const pending: { resolve?: (value: unknown) => void } = {}
  const refetch = new Promise((resolve) => {
    pending.resolve = resolve
  })
  let reads = 0
  vi.mocked(api.get).mockImplementation((url: string) => {
    if (url === INTEGRATION_PATH) {
      reads += 1
      return reads === 1 ? Promise.resolve(integration(initial)) : refetch
    }
    return Promise.resolve({ channels: [{ id: 'C123', name: 'security' }] })
  })
  return {
    reads: () => reads,
    resolveRefetch: (channelActions: ReturnType<typeof security>) =>
      pending.resolve?.(integration(channelActions)),
  }
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return render(<IntegrationsPage />, { wrapper })
}

describe('IntegrationsPage Slack channels', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('removes a pattern by value and sends the configuration it was removed from', async () => {
    mockIntegrationReads(security('login.*_failed', 'audit_log.verify'))
    vi.mocked(api.put).mockResolvedValue(undefined)
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Remove audit_log.verify' }))

    await waitFor(() => {
      expect(api.put).toHaveBeenCalledWith(CHANNELS_PATH, {
        integration_id: 1,
        channel_actions: security('login.*_failed'),
        previous_channel_actions: security('login.*_failed', 'audit_log.verify'),
      })
    })
  })

  it('keeps the patterns disabled until the saved configuration is loaded', async () => {
    const reads = mockIntegrationReads(security('login.*_failed'))
    vi.mocked(api.put).mockResolvedValue(undefined)
    renderPage()

    const input = await screen.findByPlaceholderText(PATTERN_PLACEHOLDER)
    await userEvent.type(input, 'mfa.verify_failed{Enter}')

    await waitFor(() => expect(reads.reads()).toBe(2))
    expect(screen.getByPlaceholderText(PATTERN_PLACEHOLDER)).toBeDisabled()

    reads.resolveRefetch(security('login.*_failed', 'mfa.verify_failed'))
    await waitFor(() => expect(screen.getByPlaceholderText(PATTERN_PLACEHOLDER)).not.toBeDisabled())
    expect(screen.getByText('mfa.verify_failed')).toBeInTheDocument()
  })

  it('reloads the configuration when it changed since it was loaded', async () => {
    const reads = mockIntegrationReads(security('login.*_failed', 'audit_log.verify'))
    vi.mocked(api.put).mockRejectedValue({
      response: { status: 409, data: { error: 'slack_channels_changed' } },
    })
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'Remove audit_log.verify' }))

    await waitFor(() => expect(reads.reads()).toBe(2))
  })
})
