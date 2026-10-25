import { useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useState } from 'react'
import type { SettingsSetting } from '@/api/schemas'
import {
  getSettingValue,
  hasStoredValue,
  isSetByEnv,
  SourceIndicator,
} from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { useStepUp } from '@/contexts/step-up-context'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { toastAPIError } from '@/lib/error-utils'
import type { GlobalSettingsResponse } from '@/pages/settings/types'

interface GlobalToggleCardProps {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
  // settingKey is a global setting that is its own group, saved at /api/v1/settings/<settingKey with dashes>.
  settingKey: string
  title: string
  description: ReactNode
  label: string
}

// GlobalToggleCard turns one true-or-false global setting on or off, with Save and Cancel, and Clear for a stored
// value.
export function GlobalToggleCard({
  settings,
  disabled,
  settingKey,
  title,
  description,
  label,
}: GlobalToggleCardProps) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [pending, setPending] = useState<boolean | null>(null)
  const setting = settings?.[settingKey]
  const path = `/api/v1/settings/${settingKey.replaceAll('_', '-')}`

  const displayValue = pending ?? getSettingValue(setting, false)
  const hasChanges = pending !== null

  const onSaved =
    (message: string) => (updatedSettings: Record<string, SettingsSetting> | undefined) => {
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setPending(null)
      toast.success(message)
    }

  const updateMutation = useMutation({
    mutationFn: async () => {
      if (pending === null) {
        return
      }
      return await api.put<Record<string, SettingsSetting>>(path, { [settingKey]: pending })
    },
    onSuccess: onSaved('Setting updated'),
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to update setting')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () =>
      await api.delete<Record<string, SettingsSetting>>(`${path}?key=${settingKey}`),
    onSuccess: onSaved('Setting cleared'),
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to clear setting')
    },
  })

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync()
    } catch (error) {
      handleStepUpError(error, () => updateMutation.mutateAsync())
    }
  }

  const handleClear = async () => {
    try {
      await clearMutation.mutateAsync()
    } catch (error) {
      handleStepUpError(error, () => clearMutation.mutateAsync())
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <p className="text-muted-foreground text-sm">{description}</p>

        <label className="flex items-center gap-3">
          <input
            checked={displayValue}
            disabled={disabled || isSetByEnv(setting)}
            onChange={(event) => setPending(event.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">{label}</span>
          <SourceIndicator setting={setting} />
        </label>

        <div className="flex justify-end gap-2">
          {hasStoredValue(setting) && (
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
              <Button
                disabled={disabled}
                onClick={() => setPending(null)}
                size="sm"
                variant="outline"
              >
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
