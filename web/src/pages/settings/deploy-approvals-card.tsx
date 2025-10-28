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

type DeployApprovalsCardProps = {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}

export function DeployApprovalsCard({ settings, disabled }: DeployApprovalsCardProps) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [windowMinutes, setWindowMinutes] = useState<number | null>(null)

  const currentEnabled = getSettingValue(settings?.deploy_approvals_enabled, true)
  const currentWindowMinutes = getSettingValue(settings?.deploy_approval_window_minutes, 15)

  const displayEnabled = enabled ?? currentEnabled
  const displayWindowMinutes = windowMinutes ?? currentWindowMinutes
  const hasChanges = enabled !== null || windowMinutes !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      const updates: Record<string, unknown> = {}
      if (enabled !== null) {
        updates.deploy_approvals_enabled = enabled
      }
      if (windowMinutes !== null) {
        updates.deploy_approval_window_minutes = windowMinutes
      }
      return await api.put<Record<string, SettingsSetting>>(
        '/api/v1/settings/deploy-approvals',
        updates
      )
    },
    onSuccess: (updatedSettings) => {
      qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
        ...old,
        ...updatedSettings,
      }))
      setEnabled(null)
      setWindowMinutes(null)
      toast.success('Deploy approval settings updated')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to update deploy approval settings')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      const keys: string[] = []
      if (settings?.deploy_approvals_enabled?.source === 'db') {
        keys.push('deploy_approvals_enabled')
      }
      if (settings?.deploy_approval_window_minutes?.source === 'db') {
        keys.push('deploy_approval_window_minutes')
      }
      if (keys.length > 0) {
        const params = keys.map((key) => `key=${key}`).join('&')
        return await api.delete<Record<string, SettingsSetting>>(
          `/api/v1/settings/deploy-approvals?${params}`
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
      setEnabled(null)
      setWindowMinutes(null)
      toast.success('Deploy approval settings cleared')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to clear deploy approval settings')
    },
  })

  const handleCancel = () => {
    setEnabled(null)
    setWindowMinutes(null)
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
    settings?.deploy_approvals_enabled?.source === 'db' ||
    settings?.deploy_approval_window_minutes?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Deploy Approvals</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <p className="text-muted-foreground text-sm">
          Configure manual deploy approval workflow for CI/CD pipelines. When enabled, API tokens
          with deploy_with_approval permission require admin approval before deploying.
        </p>

        <label className="flex items-center gap-3">
          <input
            checked={displayEnabled}
            disabled={disabled}
            onChange={(event) => setEnabled(event.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Enable deploy approvals</span>
          <SourceIndicator setting={settings?.deploy_approvals_enabled} />
        </label>

        <div>
          <Label htmlFor="approval-window">Approval Window (minutes)</Label>
          <div className="flex items-center gap-2">
            <Input
              className="w-24"
              disabled={disabled}
              id="approval-window"
              min={1}
              onChange={(event) => {
                const value = Number.parseInt(event.target.value, 10)
                if (!Number.isNaN(value) && value >= 1) {
                  setWindowMinutes(value)
                }
              }}
              type="number"
              value={displayWindowMinutes}
            />
            <SourceIndicator setting={settings?.deploy_approval_window_minutes} />
          </div>
          <p className="mt-1 text-muted-foreground text-xs">
            How long approvals remain valid after admin approval
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
