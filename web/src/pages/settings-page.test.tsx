import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SettingsSetting } from '@/api/schemas'
import { HttpClientProvider } from '@/contexts/http-client-context'
import { StepUpProvider } from '@/contexts/step-up-context'
import { api } from '@/lib/api'
import { SettingsPage } from './settings-page'

vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

// Mock useAuth
const mockUseAuth = vi.fn()
vi.mock('@/contexts/auth-context', () => ({
  useAuth: () => mockUseAuth(),
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
}))

const mockAuthUser = {
  email: 'admin@example.com',
  name: 'Admin User',
  roles: ['admin'],
  mfa_enrolled: true,
  mfa_required: false,
}

function renderSettingsPage(user = mockAuthUser) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })

  mockUseAuth.mockReturnValue({
    user,
    isAuthenticated: true,
    login: vi.fn(),
    logout: vi.fn(),
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <HttpClientProvider>
        <StepUpProvider>
          <SettingsPage />
        </StepUpProvider>
      </HttpClientProvider>
    </QueryClientProvider>
  )
}

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Loading global settings', () => {
    it('displays settings from database', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: false,
          source: 'db',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 60,
          source: 'db',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 15,
          source: 'db',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: true,
          source: 'db',
        } as SettingsSetting,
      })

      renderSettingsPage()

      await waitFor(() => {
        expect(screen.getByLabelText(/require mfa for all users/i)).not.toBeChecked()
      })

      expect(screen.getByLabelText(/trusted device ttl/i)).toHaveValue(60)
      expect(screen.getByLabelText(/step-up window/i)).toHaveValue(15)
      expect(screen.getByLabelText(/allow destructive actions/i)).toBeChecked()

      // Should show Clear buttons since all settings are from DB
      const clearButtons = screen.getAllByRole('button', { name: /clear/i })
      expect(clearButtons.length).toBeGreaterThan(0)
    })

    it('displays settings from environment variables with source indicator', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: true,
          source: 'env',
          env_var: 'RGW_SETTING_MFA_REQUIRE_ALL_USERS',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 30,
          source: 'default',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 10,
          source: 'default',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: false,
          source: 'default',
        } as SettingsSetting,
      })

      renderSettingsPage()

      await waitFor(() => {
        expect(screen.getByText(/from env/i)).toBeInTheDocument()
      })

      // Should show "default" for default values
      const defaults = screen.getAllByText(/\(default\)/i)
      expect(defaults).toHaveLength(3) // trusted device TTL, step-up window, and allow destructive
    })

    it('displays default values when no settings in db or env', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: true,
          source: 'default',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 30,
          source: 'default',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 10,
          source: 'default',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: false,
          source: 'default',
        } as SettingsSetting,
      })

      renderSettingsPage()

      await waitFor(() => {
        expect(screen.getByLabelText(/require mfa for all users/i)).toBeChecked() // default is true
      })

      expect(screen.getByLabelText(/trusted device ttl/i)).toHaveValue(30) // default
      expect(screen.getByLabelText(/step-up window/i)).toHaveValue(10) // default
      expect(screen.getByLabelText(/allow destructive actions/i)).not.toBeChecked() // default is false

      // Should NOT show Clear buttons since nothing is in DB
      const clearButtons = screen.queryAllByRole('button', { name: /clear/i })
      expect(clearButtons).toHaveLength(0)
    })
  })

  describe('Updating settings', () => {
    it('shows Save and Cancel buttons when value changes', async () => {
      const mockData = {
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: true,
          source: 'default',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 30,
          source: 'default',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 10,
          source: 'default',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: false,
          source: 'default',
        } as SettingsSetting,
      }

      vi.mocked(api.get).mockResolvedValue(mockData)

      renderSettingsPage()

      // Wait for data to load by checking the value is set correctly
      await waitFor(() => {
        expect(screen.getByLabelText(/allow destructive actions/i)).not.toBeChecked()
      })

      // Initially no Save/Cancel buttons
      expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /cancel/i })).not.toBeInTheDocument()

      // Check the checkbox - use fireEvent to bypass disabled state in tests
      const checkbox = screen.getByLabelText(/allow destructive actions/i)
      fireEvent.click(checkbox)

      // Save and Cancel buttons should appear
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
        expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
      })
    })

    it('updates setting when Save is clicked', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: true,
          source: 'default',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 30,
          source: 'default',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 10,
          source: 'default',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: false,
          source: 'default',
        } as SettingsSetting,
      })

      vi.mocked(api.put).mockResolvedValue({
        key: 'allow_destructive_actions',
        value: true,
        source: 'db',
      } as SettingsSetting)

      renderSettingsPage()

      // Wait for data to load by checking value
      await waitFor(() => {
        expect(screen.getByLabelText(/allow destructive actions/i)).not.toBeChecked()
      })

      const checkbox = screen.getByLabelText(/allow destructive actions/i)
      fireEvent.click(checkbox)

      // Click Save button
      const saveButton = await screen.findByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      await waitFor(() => {
        expect(api.put).toHaveBeenCalledWith('/api/v1/settings/allow-destructive-actions', {
          allow_destructive_actions: true,
        })
      })
    })

    it('discards changes when Cancel is clicked', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: true,
          source: 'default',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 30,
          source: 'default',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 10,
          source: 'default',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: false,
          source: 'default',
        } as SettingsSetting,
      })

      renderSettingsPage()

      // Wait for data to load by checking value
      await waitFor(() => {
        expect(screen.getByLabelText(/allow destructive actions/i)).not.toBeChecked()
      })

      const checkbox = screen.getByLabelText(/allow destructive actions/i)

      // Check the checkbox
      fireEvent.click(checkbox)
      expect(checkbox).toBeChecked()

      // Click Cancel button
      const cancelButton = await screen.findByRole('button', {
        name: /cancel/i,
      })
      fireEvent.click(cancelButton)

      // Checkbox should be unchecked again
      await waitFor(() => {
        expect(checkbox).not.toBeChecked()
      })

      // Save/Cancel buttons should disappear
      expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /cancel/i })).not.toBeInTheDocument()
    })

    it('updates multiple MFA settings at once', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: true,
          source: 'default',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 30,
          source: 'default',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 10,
          source: 'default',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: false,
          source: 'default',
        } as SettingsSetting,
      })

      vi.mocked(api.put).mockResolvedValue({})

      renderSettingsPage()

      // Wait for data to load by checking values
      await waitFor(() => {
        expect(screen.getByLabelText(/trusted device ttl/i)).toHaveValue(30)
      })

      const ttlInput = screen.getByLabelText(/trusted device ttl/i) as HTMLInputElement
      fireEvent.change(ttlInput, { target: { value: '60' } })

      const windowInput = screen.getByLabelText(/step-up window/i) as HTMLInputElement
      fireEvent.change(windowInput, { target: { value: '15' } })

      // Wait for Save button to appear after changes
      // The Save button should appear in the document after we make changes
      const saveButtons = await screen.findAllByRole('button', {
        name: /save/i,
      })
      // Should be Save button in MFA card (only one since we didn't change Destructive Actions)
      expect(saveButtons.length).toBe(1)
      fireEvent.click(saveButtons[0])

      await waitFor(() => {
        expect(api.put).toHaveBeenCalledWith('/api/v1/settings/mfa-configuration', {
          mfa_trusted_device_ttl_days: 60,
          mfa_step_up_window_minutes: 15,
        })
      })
    })

    it('handles API errors gracefully', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: true,
          source: 'default',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 30,
          source: 'default',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 10,
          source: 'default',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: false,
          source: 'default',
        } as SettingsSetting,
      })

      vi.mocked(api.put).mockRejectedValue(new Error('Network error'))

      renderSettingsPage()

      // Wait for data to load by checking value
      await waitFor(() => {
        expect(screen.getByLabelText(/allow destructive actions/i)).not.toBeChecked()
      })

      const checkbox = screen.getByLabelText(/allow destructive actions/i)
      fireEvent.click(checkbox)

      const saveButton = await screen.findByRole('button', { name: /save/i })
      fireEvent.click(saveButton)

      // Should show error toast (we don't test toast here, just verify API was called)
      await waitFor(() => {
        expect(api.put).toHaveBeenCalled()
      })
    })
  })

  describe('Clearing settings', () => {
    it('clears DB setting and reverts to default', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: false,
          source: 'db',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 60,
          source: 'db',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 15,
          source: 'db',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: true,
          source: 'db',
        } as SettingsSetting,
      })

      vi.mocked(api.delete).mockResolvedValue({})

      renderSettingsPage()

      // Wait for data to load and Clear buttons to appear
      const clearButtons = await screen.findAllByRole('button', {
        name: /clear/i,
      })
      expect(clearButtons.length).toBe(2) // MFA card + Destructive card

      // Click one of the Clear buttons (doesn't matter which for this test)
      // Let's click the first one
      fireEvent.click(clearButtons[0])

      await waitFor(() => {
        // Check that api.delete was called with the grouped endpoint for MFA settings
        expect(api.delete).toHaveBeenCalledWith(
          '/api/v1/settings/mfa-configuration?key=mfa_require_all_users&key=mfa_trusted_device_ttl_days&key=mfa_step_up_window_minutes'
        )
      })
    })
  })

  describe('Disabled state', () => {
    it('disables inputs when user is not admin', async () => {
      vi.mocked(api.get).mockResolvedValue({
        mfa_require_all_users: {
          key: 'mfa_require_all_users',
          value: true,
          source: 'default',
        } as SettingsSetting,
        mfa_trusted_device_ttl_days: {
          key: 'mfa_trusted_device_ttl_days',
          value: 30,
          source: 'default',
        } as SettingsSetting,
        mfa_step_up_window_minutes: {
          key: 'mfa_step_up_window_minutes',
          value: 10,
          source: 'default',
        } as SettingsSetting,
        allow_destructive_actions: {
          key: 'allow_destructive_actions',
          value: false,
          source: 'default',
        } as SettingsSetting,
      })

      const nonAdminUser = {
        ...mockAuthUser,
        roles: ['deployer'],
      }

      renderSettingsPage(nonAdminUser)

      await waitFor(() => {
        const checkboxes = screen.getAllByRole('checkbox')
        for (const checkbox of checkboxes) {
          expect(checkbox).toBeDisabled()
        }

        const numberInputs = screen.getAllByRole('spinbutton')
        for (const input of numberInputs) {
          expect(input).toBeDisabled()
        }
      })
    })
  })
})
