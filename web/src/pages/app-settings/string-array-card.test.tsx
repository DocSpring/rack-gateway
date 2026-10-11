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
