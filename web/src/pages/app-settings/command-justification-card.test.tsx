import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SettingsSetting } from '@/api/schemas'
import { CommandJustificationCard } from './command-justification-card'
import type { AppSettingsResponse } from './types'

const mockPut = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, put: mockPut } }
})

function renderCard(settings: AppSettingsResponse, gatewayWide?: SettingsSetting) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <CommandJustificationCard
        app="web"
        disabled={false}
        gatewayWide={gatewayWide}
        settings={settings}
      />
    </QueryClientProvider>
  )
}

const gatewayWideNote = /The gateway-wide setting requires a justification/

const checkbox = () =>
  screen.getByRole('checkbox', { name: /Require a justification for every command/ })

describe('CommandJustificationCard', () => {
  beforeEach(() => {
    mockPut.mockReset()
    mockPut.mockResolvedValue({})
  })

  // Ticking the box saves the setting at once.
  it('saves the setting when the box is ticked', async () => {
    renderCard({
      require_command_justification: { value: false, source: 'default' },
    } as AppSettingsResponse)
    expect(checkbox()).not.toBeChecked()
    fireEvent.click(checkbox())
    await waitFor(() =>
      expect(mockPut).toHaveBeenCalledWith(
        '/api/v1/apps/web/settings/require-command-justification',
        true
      )
    )
  })

  // A setting the deployment config defines (an env var) can't be changed here: the API refuses to.
  it('is read-only when an environment variable defines the setting', () => {
    renderCard({
      require_command_justification: {
        value: true,
        source: 'env',
        env_var: 'RGW_APP_WEB_SETTING_REQUIRE_COMMAND_JUSTIFICATION',
      },
    } as AppSettingsResponse)
    expect(checkbox()).toBeChecked()
    expect(checkbox()).toBeDisabled()
  })

  // With the gateway-wide setting on, the app requires one whatever its own setting says, and the card says so.
  it('says when the gateway-wide setting requires a justification', () => {
    const appOff = {
      require_command_justification: { value: false, source: 'default' },
    } as AppSettingsResponse
    const { unmount } = renderCard(appOff, { value: false, source: 'default' } as SettingsSetting)
    expect(screen.queryByText(gatewayWideNote)).not.toBeInTheDocument()
    unmount()

    renderCard(appOff, { value: true, source: 'env' } as SettingsSetting)
    expect(screen.getByText(gatewayWideNote)).toBeInTheDocument()
    expect(checkbox()).not.toBeChecked()
  })
})
