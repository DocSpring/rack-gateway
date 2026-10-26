import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { JsonSettingCard } from './json-setting-card'
import type { AppSettingsResponse } from './types'

const mockPut = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, put: mockPut } }
})

const toastError = vi.hoisted(() => vi.fn())
vi.mock('@/components/ui/use-toast', () => ({
  toast: { error: toastError, success: vi.fn() },
}))

function renderCard(settings: AppSettingsResponse = {}) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <JsonSettingCard
        app="web"
        description="The commands a CI/CD token can run."
        disabled={false}
        label="JSON list of commands"
        pathSegment="approved-deploy-commands"
        placeholder='[{"command": "rake db:migrate"}]'
        settingKey="approved_deploy_commands"
        settings={settings}
        title="Approved Deploy Commands"
      />
    </QueryClientProvider>
  )
}

function save(json: string) {
  fireEvent.change(screen.getByLabelText('JSON list of commands'), { target: { value: json } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
}

describe('JsonSettingCard', () => {
  beforeEach(() => {
    mockPut.mockReset()
    toastError.mockReset()
  })

  it('shows the current value as indented JSON', () => {
    renderCard({
      approved_deploy_commands: {
        value: [{ command: 'rake db:migrate', write: true }],
        source: 'db',
      },
    } as AppSettingsResponse)
    expect(screen.getByLabelText('JSON list of commands')).toHaveValue(
      JSON.stringify([{ command: 'rake db:migrate', write: true }], null, 2)
    )
  })

  it('reports invalid JSON without calling the API', async () => {
    renderCard()
    save('[{not json')
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('Invalid JSON format'))
    expect(mockPut).not.toHaveBeenCalled()
  })

  // Only the parse is about the JSON: a failed request must show the server's error, not "Invalid JSON format".
  it("saves the parsed value to the setting's path, and shows the API's error when that fails", async () => {
    mockPut.mockRejectedValue(new Error('unknown field "comand"'))
    renderCard()
    save('[{"command": "rake db:migrate", "env": ["STRIPE_KEY"]}]')
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('unknown field "comand"'))
    expect(mockPut).toHaveBeenCalledWith('/api/v1/apps/web/settings/approved-deploy-commands', [
      { command: 'rake db:migrate', env: ['STRIPE_KEY'] },
    ])
    expect(toastError).not.toHaveBeenCalledWith('Invalid JSON format')
  })
})
