import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { SettingsSetting } from '@/api/schemas'
import { getSettingValue, SourceIndicator } from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/use-toast'
import { useStepUp } from '@/contexts/step-up-context'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { toastAPIError } from '@/lib/error-utils'
import type { GlobalSettingsResponse } from '@/pages/settings/types'

type MfaConfigCardProps = {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}

export function MfaConfigCard({ settings, disabled }: MfaConfigCardProps) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [requireMfa, setRequireMfa] = useState<boolean | null>(null)
  const [trustedDeviceTtl, setTrustedDeviceTtl] = useState<number | null>(null)
  const [stepUpWindow, setStepUpWindow] = useState<number | null>(null)

  const currentRequireMfa = getSettingValue(settings?.mfa_require_all_users, true)
  const currentTrustedDeviceTtl = getSettingValue(settings?.mfa_trusted_device_ttl_days, 30)
  const currentStepUpWindow = getSettingValue(settings?.mfa_step_up_window_minutes, 10)

  const displayRequireMfa = requireMfa ?? currentRequireMfa
  const displayTrustedDeviceTtl = trustedDeviceTtl ?? currentTrustedDeviceTtl
  const displayStepUpWindow = stepUpWindow ?? currentStepUpWindow

  const hasChanges = requireMfa !== null || trustedDeviceTtl !== null || stepUpWindow !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      const updates: Record<string, unknown> = {}
      if (requireMfa !== null) {
        updates.mfa_require_all_users = requireMfa
      }
      if (trustedDeviceTtl !== null) {
        updates.mfa_trusted_device_ttl_days = trustedDeviceTtl
      }
      if (stepUpWindow !== null) {
        updates.mfa_step_up_window_minutes = stepUpWindow
      }
      return await api.put<Record<string, SettingsSetting>>(
        '/api/v1/settings/mfa-configuration',
        updates
      )
    },
    onSuccess: (updatedSettings) => {
      qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
        ...old,
        ...updatedSettings,
      }))
      setRequireMfa(null)
      setTrustedDeviceTtl(null)
      setStepUpWindow(null)
      toast.success('MFA settings updated')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to update MFA settings')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      const keys: string[] = []
      if (settings?.mfa_require_all_users?.source === 'db') {
        keys.push('mfa_require_all_users')
      }
      if (settings?.mfa_trusted_device_ttl_days?.source === 'db') {
        keys.push('mfa_trusted_device_ttl_days')
      }
      if (settings?.mfa_step_up_window_minutes?.source === 'db') {
        keys.push('mfa_step_up_window_minutes')
      }
      if (keys.length > 0) {
        const params = keys.map((key) => `key=${key}`).join('&')
        return await api.delete<Record<string, SettingsSetting>>(
          `/api/v1/settings/mfa-configuration?${params}`
        )
      }
    },
    onSuccess: (updatedSettings) => {
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setRequireMfa(null)
      setTrustedDeviceTtl(null)
      setStepUpWindow(null)
      toast.success('MFA settings cleared')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to clear MFA settings')
    },
  })

  const handleCancel = () => {
    setRequireMfa(null)
    setTrustedDeviceTtl(null)
    setStepUpWindow(null)
  }

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync()
    } catch (error) {
      if (handleStepUpError(error, () => updateMutation.mutateAsync())) {
        return
      }
    }
  }

  const handleClear = async () => {
    try {
      await clearMutation.mutateAsync()
    } catch (error) {
      if (handleStepUpError(error, () => clearMutation.mutateAsync())) {
        return
      }
    }
  }

  const hasDbSettings =
    settings?.mfa_require_all_users?.source === 'db' ||
    settings?.mfa_trusted_device_ttl_days?.source === 'db' ||
    settings?.mfa_step_up_window_minutes?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>MFA Requirements</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <p className="text-muted-foreground text-sm">
          Configure organization-wide multi-factor authentication requirements, including trusted
          device TTL and step-up authentication windows.
        </p>

        <label className="flex items-center gap-3">
          <input
            checked={displayRequireMfa}
            disabled={disabled}
            onChange={(event) => setRequireMfa(event.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Require MFA for all users</span>
          <SourceIndicator setting={settings?.mfa_require_all_users} />
        </label>

        <div className="grid gap-6 md:grid-cols-2">
          <div>
            <Label htmlFor="trusted-device-ttl">Trusted Device TTL (days)</Label>
            <div className="flex items-center gap-2">
              <Input
                disabled={disabled}
                id="trusted-device-ttl"
                min={1}
                onChange={(event) => {
                  const value = Number.parseInt(event.target.value, 10)
                  if (Number.isNaN(value)) {
                    setTrustedDeviceTtl(0)
                  } else {
                    setTrustedDeviceTtl(value)
                  }
                }}
                type="number"
                value={displayTrustedDeviceTtl}
              />
              <SourceIndicator setting={settings?.mfa_trusted_device_ttl_days} />
            </div>
            <p className="mt-1 text-muted-foreground text-xs">
              How long a trusted device remains valid before requiring MFA again
            </p>
          </div>

          <div>
            <Label htmlFor="step-up-window">Step-Up Window (minutes)</Label>
            <div className="flex items-center gap-2">
              <Input
                disabled={disabled}
                id="step-up-window"
                min={1}
                onChange={(event) => {
                  const value = Number.parseInt(event.target.value, 10)
                  if (Number.isNaN(value)) {
                    setStepUpWindow(0)
                  } else {
                    setStepUpWindow(value)
                  }
                }}
                type="number"
                value={displayStepUpWindow}
              />
              <SourceIndicator setting={settings?.mfa_step_up_window_minutes} />
            </div>
            <p className="mt-1 text-muted-foreground text-xs">
              Duration of MFA step-up authentication window for sensitive operations
            </p>
          </div>
        </div>

        <div className="flex justify-end gap-2">
          {hasDbSettings && (
            <Button
              disabled={disabled || clearMutation.isPending}
              onClick={handleClear}
              size="sm"
              variant="outline"
            >
              Clear
            </Button>
          )}
          {hasChanges && (
            <>
              <Button disabled={disabled} onClick={handleCancel} size="sm" variant="outline">
                Cancel
              </Button>
              <Button
                disabled={disabled || updateMutation.isPending}
                onClick={handleSave}
                size="sm"
              >
                Save
              </Button>
            </>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
