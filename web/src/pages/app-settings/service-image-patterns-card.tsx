import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { getSettingValue, SourceIndicator } from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'

import type { AppSettingsResponse } from '@/pages/app-settings/types'
import { extractErrorMessage } from '@/pages/app-settings/utils'

type ServiceImagePatternsCardProps = {
  app: string
  settings: AppSettingsResponse | undefined
  disabled: boolean
}

export function ServiceImagePatternsCard({
  app,
  settings,
  disabled,
}: ServiceImagePatternsCardProps) {
  const qc = useQueryClient()
  const [value, setValue] = useState<string | null>(null)

  const setting = settings?.service_image_patterns
  const currentValue = getSettingValue<Record<string, string> | null>(setting, null)

  let displayValue = ''
  if (value !== null) {
    displayValue = value
  } else if (currentValue) {
    displayValue = JSON.stringify(currentValue, null, 2)
  }

  const hasChanges = value !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      if (value === null) {
        return
      }
      try {
        const parsed = JSON.parse(value)
        await api.put(`/api/v1/apps/${app}/settings/service-image-patterns`, parsed)
      } catch {
        throw new Error('Invalid JSON format')
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appSettings', app] })
      setValue(null)
      toast.success('Setting updated')
    },
    onError: (error: unknown) => {
      const message = extractErrorMessage(error)
      toast.error(message ?? 'Failed to update setting')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      await api.delete(`/api/v1/apps/${app}/settings/service-image-patterns`)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appSettings', app] })
      setValue(null)
      toast.success('Setting cleared')
    },
    onError: (error: unknown) => {
      const message = extractErrorMessage(error)
      toast.error(message ?? 'Failed to clear setting')
    },
  })

  const handleCancel = () => {
    setValue(null)
  }

  const handleSave = () => {
    updateMutation.mutate()
  }

  const handleClear = () => {
    clearMutation.mutate()
  }

  const hasDbSetting = setting?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Service Image Patterns</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 pb-6">
        <p className="text-muted-foreground text-sm">
          Per-service regex patterns for validating Docker images in convox.yml. Validates build
          commands to ensure only images matching the pattern are allowed.
        </p>
        <div>
          <Label htmlFor="service-image-patterns">JSON object (service name → regex)</Label>
          <div className="flex flex-col gap-2">
            <textarea
              className="min-h-32 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
              disabled={disabled}
              id="service-image-patterns"
              onChange={(event) => setValue(event.target.value)}
              placeholder='{"web": "^ghcr.io/myorg/myapp:{{GIT_COMMIT}}$", "worker": "^.*$"}'
              value={displayValue}
            />
            <SourceIndicator setting={setting} />
          </div>
        </div>

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
