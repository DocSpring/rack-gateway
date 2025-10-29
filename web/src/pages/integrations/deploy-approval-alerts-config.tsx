import { Check, Loader2 } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

import type { SlackChannel } from '@/pages/integrations/slack-types'

type DeployApprovalAlertsConfigProps = {
  enabled: boolean
  channelId: string
  channels: SlackChannel[] | undefined
  isUpdating: boolean
  onChange: (enabled: boolean, channelId: string) => void
}

export function DeployApprovalAlertsConfig({
  enabled,
  channelId,
  channels,
  isUpdating,
  onChange,
}: DeployApprovalAlertsConfigProps) {
  const [localEnabled, setLocalEnabled] = useState(enabled)
  const [localChannelId, setLocalChannelId] = useState(channelId)
  const [hasChanges, setHasChanges] = useState(false)

  const handleEnabledChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    setLocalEnabled(event.target.checked)
    setHasChanges(true)
  }

  const handleChannelChange = (event: React.ChangeEvent<HTMLSelectElement>) => {
    setLocalChannelId(event.target.value)
    setHasChanges(true)
  }

  const handleSave = () => {
    onChange(localEnabled, localChannelId)
    setHasChanges(false)
  }

  const handleReset = () => {
    setLocalEnabled(enabled)
    setLocalChannelId(channelId)
    setHasChanges(false)
  }

  return (
    <div className="space-y-4 rounded-lg border p-4">
      <div className="flex items-start justify-between">
        <div>
          <h4 className="font-medium text-base">Deploy Approval Alerts</h4>
          <p className="text-muted-foreground text-sm">
            Send rich notifications when deploy approval requests are created
          </p>
        </div>
        <label className="relative inline-flex cursor-pointer items-center">
          <input
            checked={localEnabled}
            className="peer sr-only"
            onChange={handleEnabledChange}
            type="checkbox"
          />
          <div className="peer rtl:peer-checked:after:-translate-x-full h-6 w-11 rounded-full bg-gray-200 after:absolute after:start-[2px] after:top-[2px] after:size-5 after:rounded-full after:border after:border-gray-300 after:bg-white after:transition-all after:content-[''] peer-checked:bg-blue-600 peer-checked:after:translate-x-full peer-checked:after:border-white peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:border-gray-600 dark:bg-gray-700 dark:peer-focus:ring-blue-800" />
        </label>
      </div>

      {localEnabled && (
        <div className="space-y-3">
          <div className="space-y-2">
            <Label htmlFor="deploy-approval-channel">Channel</Label>
            <select
              className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:font-medium file:text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
              disabled={isUpdating || !channels || channels.length === 0}
              id="deploy-approval-channel"
              onChange={handleChannelChange}
              value={localChannelId}
            >
              <option value="">Select a channel</option>
              {channels?.map((channel) => (
                <option key={channel.id} value={channel.id}>
                  {channel.name}
                </option>
              ))}
            </select>
            <p className="text-muted-foreground text-xs">
              Choose which channel receives deploy approval request notifications with links to the
              approval page, GitHub PR, and CI pipeline.
            </p>
          </div>

          {hasChanges && (
            <div className="flex gap-2 pt-2">
              <Button disabled={isUpdating || !localChannelId} onClick={handleSave} size="sm">
                {isUpdating ? (
                  <Loader2 className="mr-2 size-4 animate-spin" />
                ) : (
                  <Check className="mr-2 size-4" />
                )}
                Save Changes
              </Button>
              <Button disabled={isUpdating} onClick={handleReset} size="sm" variant="outline">
                Cancel
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
