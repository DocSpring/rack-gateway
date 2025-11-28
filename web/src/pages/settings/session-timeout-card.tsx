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

type SessionTimeoutCardProps = {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}

export function SessionTimeoutCard({ settings, disabled }: SessionTimeoutCardProps) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [timeoutMinutes, setTimeoutMinutes] = useState<number | null>(null)

  const currentTimeoutMinutes = getSettingValue(settings?.session_timeout_minutes, 5)
  const displayTimeoutMinutes = timeoutMinutes ?? currentTimeoutMinutes
  const hasChanges = timeoutMinutes !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      const updates: Record<string, unknown> = {}
      if (timeoutMinutes !== null) {
        updates.session_timeout_minutes = timeoutMinutes
      }
      return await api.put<Record<string, SettingsSetting>>(
        '/api/v1/settings/session-configuration',
        updates
      )
    },
    onSuccess: (updatedSettings) => {
      qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
        ...old,
        ...updatedSettings,
      }))
      setTimeoutMinutes(null)
      toast.success('Session timeout updated')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to update session timeout')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      const keys: string[] = []
      if (settings?.session_timeout_minutes?.source === 'db') {
        keys.push('session_timeout_minutes')
      }
      if (keys.length > 0) {
        const params = keys.map((key) => `key=${key}`).join('&')
        return await api.delete<Record<string, SettingsSetting>>(
          `/api/v1/settings/session-configuration?${params}`
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
      setTimeoutMinutes(null)
      toast.success('Session timeout cleared')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to clear session timeout')
    },
  })

  const handleCancel = () => {
    setTimeoutMinutes(null)
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

  const hasDbSettings = settings?.session_timeout_minutes?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Session Timeout</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <p className="text-muted-foreground text-sm">
          Configure the sliding inactivity timeout for browser sessions. Users will be logged out
          after this period of inactivity.
        </p>

        <div>
          <Label htmlFor="session-timeout">Timeout (minutes)</Label>
          <div className="flex items-center gap-2">
            <Input
              className="w-24"
              disabled={disabled}
              id="session-timeout"
              min={1}
              onChange={(event) => {
                const value = Number.parseInt(event.target.value, 10)
                if (!Number.isNaN(value) && value >= 1) {
                  setTimeoutMinutes(value)
                }
              }}
              type="number"
              value={displayTimeoutMinutes}
            />
            <SourceIndicator setting={settings?.session_timeout_minutes} />
          </div>
          <p className="mt-1 text-muted-foreground text-xs">
            Sessions are automatically extended when users are active
          </p>
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
