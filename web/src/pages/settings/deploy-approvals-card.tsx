import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { SettingsSetting } from '@/api/schemas'
import {
  getSettingValue,
  hasStoredValue,
  isSetByEnv,
  SourceIndicator,
} from '@/components/settings/source-indicator'
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

interface DeployApprovalsCardProps {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}

interface DeployApprovalValues {
  deploy_approvals_enabled: boolean
  deploy_approval_window_minutes: number
  deploy_approval_require_different_approver: boolean
}

const DEFAULTS: DeployApprovalValues = {
  deploy_approvals_enabled: true,
  deploy_approval_window_minutes: 15,
  deploy_approval_require_different_approver: false,
}

const KEYS = Object.keys(DEFAULTS) as (keyof DeployApprovalValues)[]

export function DeployApprovalsCard({ settings, disabled }: DeployApprovalsCardProps) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [pending, setPending] = useState<Partial<DeployApprovalValues>>({})

  const display = <K extends keyof DeployApprovalValues>(key: K): DeployApprovalValues[K] =>
    pending[key] ?? getSettingValue(settings?.[key], DEFAULTS[key])
  const change = <K extends keyof DeployApprovalValues>(key: K, value: DeployApprovalValues[K]) =>
    setPending((current) => ({ ...current, [key]: value }))
  const isLocked = (key: keyof DeployApprovalValues) => disabled || isSetByEnv(settings?.[key])
  const hasChanges = Object.keys(pending).length > 0
  const storedKeys = KEYS.filter((key) => hasStoredValue(settings?.[key]))

  const applyUpdatedSettings = (updatedSettings: Record<string, SettingsSetting> | undefined) => {
    if (updatedSettings) {
      qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
        ...old,
        ...updatedSettings,
      }))
    }
    setPending({})
  }

  const updateMutation = useMutation({
    mutationFn: async () =>
      await api.put<Record<string, SettingsSetting>>('/api/v1/settings/deploy-approvals', pending),
    onSuccess: (updatedSettings) => {
      applyUpdatedSettings(updatedSettings)
      toast.success('Deploy approval settings updated')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to update deploy approval settings')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      if (storedKeys.length > 0) {
        const params = storedKeys.map((key) => `key=${key}`).join('&')
        return await api.delete<Record<string, SettingsSetting>>(
          `/api/v1/settings/deploy-approvals?${params}`
        )
      }
    },
    onSuccess: (updatedSettings) => {
      applyUpdatedSettings(updatedSettings)
      toast.success('Deploy approval settings cleared')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to clear deploy approval settings')
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
        <CardTitle>Deploy Approvals</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <p className="text-muted-foreground text-sm">
          Configure manual deploy approval workflow for CI/CD pipelines. When enabled, API tokens
          with deploy_with_approval permission require admin approval before deploying.
        </p>

        <label className="flex items-center gap-3">
          <input
            checked={display('deploy_approvals_enabled')}
            disabled={isLocked('deploy_approvals_enabled')}
            onChange={(event) => change('deploy_approvals_enabled', event.target.checked)}
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
              disabled={isLocked('deploy_approval_window_minutes')}
              id="approval-window"
              max={10_080}
              min={1}
              onChange={(event) => {
                const value = Number.parseInt(event.target.value, 10)
                if (!Number.isNaN(value) && value >= 1) {
                  change('deploy_approval_window_minutes', value)
                }
              }}
              type="number"
              value={display('deploy_approval_window_minutes')}
            />
            <SourceIndicator setting={settings?.deploy_approval_window_minutes} />
          </div>
          <p className="mt-1 text-muted-foreground text-xs">
            How long approvals remain valid after admin approval
          </p>
        </div>

        <div>
          <label className="flex items-center gap-3">
            <input
              checked={display('deploy_approval_require_different_approver')}
              disabled={isLocked('deploy_approval_require_different_approver')}
              onChange={(event) =>
                change('deploy_approval_require_different_approver', event.target.checked)
              }
              type="checkbox"
            />
            <span className="font-medium text-sm">Require a different approver</span>
            <SourceIndicator setting={settings?.deploy_approval_require_different_approver} />
          </label>
          <p className="mt-1 text-muted-foreground text-xs">
            Nobody can approve or extend a deploy they requested: the person who created the
            request, or the owner of its API token
          </p>
        </div>

        <div className="flex justify-end gap-2">
          {storedKeys.length > 0 && (
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
                onClick={() => setPending({})}
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
