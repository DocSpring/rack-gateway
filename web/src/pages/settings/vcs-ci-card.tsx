import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { SettingsSetting } from '@/api/schemas'
import { getSettingValue, SourceIndicator } from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/use-toast'
import { useStepUp } from '@/contexts/step-up-context'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { toastAPIError } from '@/lib/error-utils'
import type { GlobalSettingsResponse } from '@/pages/settings/types'

type VcsCiCardProps = {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}

export function VcsCiCard({ settings, disabled }: VcsCiCardProps) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [vcsProvider, setVcsProvider] = useState<string | null>(null)
  const [ciProvider, setCiProvider] = useState<string | null>(null)

  const currentVcsProvider = getSettingValue(settings?.default_vcs_provider, 'github')
  const currentCiProvider = getSettingValue(settings?.default_ci_provider, 'circleci')

  const displayVcsProvider = vcsProvider ?? currentVcsProvider
  const displayCiProvider = ciProvider ?? currentCiProvider

  const hasChanges = vcsProvider !== null || ciProvider !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      const updates: Record<string, unknown> = {}
      if (vcsProvider !== null) {
        updates.default_vcs_provider = vcsProvider
      }
      if (ciProvider !== null) {
        updates.default_ci_provider = ciProvider
      }
      return await api.put<Record<string, SettingsSetting>>(
        '/api/v1/settings/vcs-and-ci-defaults',
        updates
      )
    },
    onSuccess: (updatedSettings) => {
      qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
        ...old,
        ...updatedSettings,
      }))
      setVcsProvider(null)
      setCiProvider(null)
      toast.success('Provider settings updated')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to update settings')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      const keys: string[] = []
      if (settings?.default_vcs_provider?.source === 'db') {
        keys.push('default_vcs_provider')
      }
      if (settings?.default_ci_provider?.source === 'db') {
        keys.push('default_ci_provider')
      }
      if (keys.length > 0) {
        const params = keys.map((key) => `key=${key}`).join('&')
        return await api.delete<Record<string, SettingsSetting>>(
          `/api/v1/settings/vcs-and-ci-defaults?${params}`
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
      setVcsProvider(null)
      setCiProvider(null)
      toast.success('Provider settings cleared')
    },
    onError: (error: unknown) => {
      toastAPIError(error, 'Failed to clear settings')
    },
  })

  const handleCancel = () => {
    setVcsProvider(null)
    setCiProvider(null)
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
    settings?.default_vcs_provider?.source === 'db' ||
    settings?.default_ci_provider?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Default VCS &amp; CI Providers</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <p className="text-muted-foreground text-sm">
          Configure default version control and CI providers used for deploy approval flows. These
          defaults can be overridden per app.
        </p>

        <div className="grid gap-6 md:grid-cols-2">
          <div>
            <Label htmlFor="vcs-provider">VCS Provider</Label>
            <div className="flex items-center gap-2">
              <select
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                disabled={disabled}
                id="vcs-provider"
                onChange={(event) => setVcsProvider(event.target.value || null)}
                value={displayVcsProvider}
              >
                <option value="github">GitHub</option>
              </select>
              <SourceIndicator setting={settings?.default_vcs_provider} />
            </div>
          </div>

          <div>
            <Label htmlFor="ci-provider">CI Provider</Label>
            <div className="flex items-center gap-2">
              <select
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                disabled={disabled}
                id="ci-provider"
                onChange={(event) => setCiProvider(event.target.value || null)}
                value={displayCiProvider}
              >
                <option value="circleci">CircleCI</option>
              </select>
              <SourceIndicator setting={settings?.default_ci_provider} />
            </div>
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
