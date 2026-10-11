import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { StringArrayCard } from './string-array-card'
import type { AppSettingsResponse } from './types'

function renderCard(settings: AppSettingsResponse) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <StringArrayCard
        app="web"
        description="Protected env vars"
        disabled={false}
        pathSegment="protected-env-vars"
        settingKey="protected_env_vars"
        settings={settings}
        title="Protected Environment Variables"
      />
    </QueryClientProvider>
  )
}

describe('StringArrayCard', () => {
  // A refetch can bring a list changed elsewhere (another admin, the CLI). Without local edits the card shows it,
  // rather than a stale copy that Save would write back.
  it('shows the latest list when the settings change and nothing was edited', () => {
    const client = new QueryClient()
    const card = (value: string[]) => (
      <QueryClientProvider client={client}>
        <StringArrayCard
          app="web"
          description="Protected env vars"
          disabled={false}
          pathSegment="protected-env-vars"
          settingKey="protected_env_vars"
          settings={{ protected_env_vars: { value, source: 'db' } } as AppSettingsResponse}
          title="Protected Environment Variables"
        />
      </QueryClientProvider>
    )
    const { rerender } = render(card(['DATABASE_URL']))
    rerender(card(['DATABASE_URL', 'SECRET_KEY_BASE']))
    expect(screen.getByText('SECRET_KEY_BASE')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  })

  // A setting the deployment config defines (an env var) is read-only in the UI: the API refuses to change it.
  it('is read-only when an environment variable defines the setting', () => {
    renderCard({
      protected_env_vars: {
        value: ['DATABASE_URL'],
        source: 'env',
        env_var: 'RGW_APP_WEB_SETTING_PROTECTED_ENV_VARS',
      },
    } as AppSettingsResponse)
    expect(screen.getByText('(from env var, read-only)')).toBeInTheDocument()
    for (const input of screen.getAllByRole('textbox')) {
      expect(input).toBeDisabled()
    }
  })

  // A stored value an env var overrides has no effect, but would apply again if the variable were removed.
  it('offers to clear a stored value that the environment variable overrides', () => {
    renderCard({
      protected_env_vars: {
        value: ['DATABASE_URL'],
        source: 'env',
        env_var: 'RGW_APP_WEB_SETTING_PROTECTED_ENV_VARS',
        stored_value_ignored: true,
      },
    } as AppSettingsResponse)
    expect(
      screen.getByText('(from env var, read-only; a stored value is ignored)')
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear' })).toBeInTheDocument()
  })

  it('is editable when the value is stored in the database', () => {
    renderCard({
      protected_env_vars: {
        value: ['DATABASE_URL'],
        source: 'db',
        env_var: 'RGW_APP_WEB_SETTING_PROTECTED_ENV_VARS',
      },
    } as AppSettingsResponse)
    for (const input of screen.getAllByRole('textbox')) {
      expect(input).toBeEnabled()
    }
  })
})
