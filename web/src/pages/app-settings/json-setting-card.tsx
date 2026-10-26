import { useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useState } from 'react'

import {
  getSettingValue,
  hasStoredValue,
  isSetByEnv,
  SourceIndicator,
} from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { apiPath } from '@/lib/api-path'

import type { AppSettingsResponse } from '@/pages/app-settings/types'
import { extractErrorMessage } from '@/pages/app-settings/utils'

interface JsonSettingCardProps {
  app: string
  settings: AppSettingsResponse | undefined
  disabled: boolean
  settingKey: string
  pathSegment: string
  title: string
  description: ReactNode
  // label names the value's shape, e.g. "JSON object (service name → regex)".
  label: string
  placeholder: string
}

// JsonSettingCard edits an app setting whose value is JSON (an object, or a list of objects) in a text area. The
// gateway checks the value's shape and refuses one it can't use.
export function JsonSettingCard({
  app,
  settings,
  disabled,
  settingKey,
  pathSegment,
  title,
  description,
  label,
  placeholder,
}: JsonSettingCardProps) {
  const qc = useQueryClient()
  const [value, setValue] = useState<string | null>(null)

  const setting = settings?.[settingKey]
  const currentValue = getSettingValue<unknown>(setting, null)

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
      let parsed: unknown
      try {
        parsed = JSON.parse(value)
      } catch (error) {
        throw new Error('Invalid JSON format', { cause: error })
      }
      await api.put(apiPath`/api/v1/apps/${app}/settings/${pathSegment}`, parsed)
    },
    onSuccess: async () => {
      // Refetch before dropping local edits, so the card never shows the old cached value
      await qc.invalidateQueries({ queryKey: ['appSettings', app] })
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
      await api.delete(apiPath`/api/v1/apps/${app}/settings/${pathSegment}`)
    },
    onSuccess: async () => {
      // Refetch before dropping local edits, so the card never shows the old cached value
      await qc.invalidateQueries({ queryKey: ['appSettings', app] })
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

  const hasDbSetting = hasStoredValue(setting)
  const inputId = `setting-${pathSegment}`

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 pb-6">
        <p className="text-muted-foreground text-sm">{description}</p>
        <div>
          <Label htmlFor={inputId}>{label}</Label>
          <div className="flex flex-col gap-2">
            <textarea
              className="min-h-32 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
              disabled={disabled || isSetByEnv(setting)}
              id={inputId}
              onChange={(event) => setValue(event.target.value)}
              placeholder={placeholder}
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
