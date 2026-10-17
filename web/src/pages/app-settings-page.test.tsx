import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AppSettingsPage } from './app-settings-page'

const params = vi.hoisted(() => ({ app: 'web' }))
vi.mock('@tanstack/react-router', () => ({
  useParams: () => params,
}))

const mockGet = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, get: mockGet } }
})

vi.mock('@/contexts/auth-context', () => ({
  useAuth: () => ({
    user: { roles: ['admin'], integrations: { github: true, circleci: true, slack: false } },
  }),
}))

const autoApproveFor: Record<string, boolean> = { web: false, api: false }

describe('AppSettingsPage', () => {
  beforeEach(() => {
    params.app = 'web'
    mockGet.mockImplementation((url: string) => {
      const [, , , , app] = url.split('/')
      return Promise.resolve({
        circleci_auto_approve_on_approval: { value: autoApproveFor[app], source: 'db' },
      })
    })
  })

  // Env values are redacted in the audit log unless the app lists the env var here.
  it('shows the env vars whose values audit logs may show', async () => {
    params.app = 'web'
    mockGet.mockResolvedValue({ audit_visible_env_vars: { value: ['RAILS_ENV'], source: 'db' } })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <AppSettingsPage />
      </QueryClientProvider>
    )
    expect(await screen.findByText('Environment Variables Shown in Audit Logs')).toBeInTheDocument()
    expect(screen.getByText('RAILS_ENV')).toBeInTheDocument()
    expect(screen.getByText(/Secret and protected values are always redacted/)).toBeInTheDocument()
  })

  // The app name comes from the page's URL, so it's encoded as one path segment: a crafted link
  // can't point the request at another endpoint.
  it('encodes the app name in the settings request path', async () => {
    params.app = 'web/../../users?role=admin'
    mockGet.mockResolvedValue({})
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <AppSettingsPage />
      </QueryClientProvider>
    )
    await waitFor(() =>
      expect(mockGet).toHaveBeenCalledWith(
        '/api/v1/apps/web%2F..%2F..%2Fusers%3Frole%3Dadmin/settings'
      )
    )
  })

  // An unsaved edit on one app's settings must not show up on another app's (cached) settings page.
  it('drops unsaved edits when switching to another app', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const renderPage = () => (
      <QueryClientProvider client={client}>
        <AppSettingsPage />
      </QueryClientProvider>
    )
    const view = render(renderPage())
    expect(await screen.findByLabelText(/Auto-approve CircleCI job on approval/)).not.toBeChecked()

    params.app = 'api'
    view.rerender(renderPage())
    await waitFor(() => expect(mockGet).toHaveBeenCalledWith('/api/v1/apps/api/settings'))
    const autoApprove = await screen.findByLabelText(/Auto-approve CircleCI job on approval/)
    fireEvent.click(autoApprove) // an unsaved edit on api
    expect(autoApprove).toBeChecked()

    params.app = 'web' // cached, so the page doesn't show its loading state in between
    view.rerender(renderPage())
    await waitFor(() =>
      expect(screen.getByLabelText(/Auto-approve CircleCI job on approval/)).not.toBeChecked()
    )
  })
})
