import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { SettingsSetting } from '@/api/schemas'
import { getSettingValue, SourceIndicator } from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { useStepUp } from '@/contexts/step-up-context'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { toastAPIError } from '@/lib/error-utils'
import type { GlobalSettingsResponse } from '@/pages/settings/types'

type DestructiveActionsCardProps = {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}

export function DestructiveActionsCard({ settings, disabled }: DestructiveActionsCardProps) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [allowDestructive, setAllowDestructive] = useState<boolean | null>(null)

  const currentValue = getSettingValue(settings?.allow_destructive_actions, false)
  const displayValue = allowDestructive ?? currentValue
  const hasChanges = allowDestructive !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      if (allowDestructive === null) {
        return
      }
      return await api.put<Record<string, SettingsSetting>>(
        '/api/v1/settings/allow-destructive-actions',
        {
          allow_destructive_actions: allowDestructive,
        }
      )
    },
    onSuccess: (updatedSettings) => {
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setAllowDestructive(null)
      toast.success('Setting updated')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to update setting')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () =>
      await api.delete<Record<string, SettingsSetting>>(
        '/api/v1/settings/allow-destructive-actions?key=allow_destructive_actions'
      ),
    onSuccess: (updatedSettings) => {
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setAllowDestructive(null)
      toast.success('Setting cleared')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to clear setting')
    },
  })

  const handleCancel = () => {
    setAllowDestructive(null)
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

  const hasDbSetting = settings?.allow_destructive_actions?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Destructive Actions</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <p className="text-muted-foreground text-sm">
          Control whether users can perform destructive operations such as deleting apps or racks
          without additional approvals.
        </p>

        <label className="flex items-center gap-3">
          <input
            checked={displayValue}
            disabled={disabled}
            onChange={(event) => setAllowDestructive(event.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Allow destructive actions</span>
          <SourceIndicator setting={settings?.allow_destructive_actions} />
        </label>

        <div className="flex justify-end gap-2">
          {hasDbSetting && (
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
