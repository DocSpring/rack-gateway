import { useQueryClient } from '@tanstack/react-query'
import {
  getSettingValue,
  hasStoredValue,
  isSetByEnv,
  SourceIndicator,
} from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { apiPath } from '@/lib/api-path'

import type { AppSettingsResponse } from '@/pages/app-settings/types'
import { extractErrorMessage } from '@/pages/app-settings/utils'

const settingKey = 'require_command_justification'

interface CommandJustificationCardProps {
  app: string
  settings: AppSettingsResponse | undefined
  disabled: boolean
}

// CommandJustificationCard turns require_command_justification on or off. A change saves at once.
export function CommandJustificationCard({
  app,
  settings,
  disabled,
}: CommandJustificationCardProps) {
  const qc = useQueryClient()
  const setting = settings?.[settingKey]
  const required = getSettingValue<boolean>(setting, false)

  const onSaved = async (message: string) => {
    await qc.invalidateQueries({ queryKey: ['appSettings', app] })
    toast.success(message)
  }
  const onFailed = (fallback: string) => (error: unknown) => {
    toast.error(extractErrorMessage(error) ?? fallback)
  }

  const updateMutation = useMutation({
    mutationFn: async (value: boolean) => {
      await api.put(apiPath`/api/v1/apps/${app}/settings/require-command-justification`, value)
    },
    onSuccess: () => onSaved('Setting updated'),
    onError: onFailed('Failed to update setting'),
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      await api.delete(apiPath`/api/v1/apps/${app}/settings/require-command-justification`)
    },
    onSuccess: () => onSaved('Setting cleared'),
    onError: onFailed('Failed to clear setting'),
  })

  const toggleDisabled = disabled || isSetByEnv(setting) || updateMutation.isPending

  return (
    <Card>
      <CardHeader>
        <CardTitle>Command Justifications</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 pb-6">
        <p className="text-muted-foreground text-sm">
          Every command run in this app (<code>rack-gateway run</code> and <code>exec</code>) must
          say why it&apos;s run and what it does, with <code>--justification</code>. The audit log
          records it with the command. A command run under a deploy approval is justified by the
          approval&apos;s message.
        </p>
        <label
          className={`flex items-center gap-3 ${toggleDisabled ? 'cursor-not-allowed opacity-50' : ''}`}
        >
          <input
            checked={required}
            disabled={toggleDisabled}
            onChange={(event) => updateMutation.mutate(event.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Require a justification for every command</span>
          <SourceIndicator setting={setting} />
        </label>
        {hasStoredValue(setting) && (
          <div className="flex justify-end">
            <Button
              disabled={disabled || clearMutation.isPending}
              onClick={() => clearMutation.mutate()}
              size="sm"
              variant="outline"
            >
              Clear
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
