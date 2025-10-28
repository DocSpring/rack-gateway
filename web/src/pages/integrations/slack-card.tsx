import { Loader2, Trash2 } from 'lucide-react'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

import { AddChannelButton } from '@/pages/integrations/add-channel-button'
import { ChannelConfigCard } from '@/pages/integrations/channel-config-card'
import type { SlackChannel, SlackIntegration } from '@/pages/integrations/types'

type SlackCardProps = {
  integration: SlackIntegration | null | undefined
  channels: SlackChannel[] | undefined
  slackConfigured: boolean
  isConnecting: boolean
  connectPending: boolean
  disconnectPending: boolean
  updatePending: boolean
  testPending: boolean
  onConnect: () => void
  onDisconnect: () => void
  onUpdateChannel: (key: string, channelId: string, channelName: string) => void
  onAddAction: (key: string, action: string) => void
  onRemoveAction: (key: string, actionIndex: number) => void
  onRemoveChannel: (key: string) => void
  onAddChannel: (channelName: string) => void
  onTestNotification: (channelId: string) => void
}

export function SlackCard({
  integration,
  channels,
  slackConfigured,
  isConnecting,
  connectPending,
  disconnectPending,
  updatePending,
  testPending,
  onConnect,
  onDisconnect,
  onUpdateChannel,
  onAddAction,
  onRemoveAction,
  onRemoveChannel,
  onAddChannel,
  onTestNotification,
}: SlackCardProps) {
  if (!slackConfigured) {
    return null
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <svg className="size-6" fill="currentColor" viewBox="0 0 24 24">
                <title>Slack logo</title>
                <path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zM6.313 15.165a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zM8.834 6.313a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zM17.688 8.834a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.165 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zM15.165 17.688a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z" />
              </svg>
              Slack
            </CardTitle>
            {integration && (
              <CardDescription>
                Connected to <strong>{integration.workspace_name}</strong>
              </CardDescription>
            )}
          </div>
          {integration ? (
            <Button
              disabled={disconnectPending}
              onClick={onDisconnect}
              size="sm"
              variant="destructive"
            >
              {disconnectPending ? (
                <Loader2 className="mr-2 size-4 animate-spin" />
              ) : (
                <Trash2 className="mr-2 size-4" />
              )}
              Disconnect
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent>
        {integration ? (
          <div className="space-y-6">
            <Alert>
              <AlertDescription>
                Slack is connected. Configure which channels receive notifications below.
              </AlertDescription>
            </Alert>

            <div className="space-y-4">
              <h3 className="font-semibold text-lg">Channel Configuration</h3>

              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                {Object.entries(integration.channel_actions).map(([key, config]) => (
                  <ChannelConfigCard
                    channels={channels || []}
                    config={config}
                    configKey={key}
                    isTesting={testPending}
                    isUpdating={updatePending}
                    key={key}
                    onAddAction={onAddAction}
                    onRemoveAction={onRemoveAction}
                    onRemoveChannel={onRemoveChannel}
                    onTestNotification={onTestNotification}
                    onUpdateChannel={onUpdateChannel}
                  />
                ))}
              </div>

              <div className="mt-6 flex justify-end">
                <AddChannelButton isUpdating={updatePending} onAdd={onAddChannel} />
              </div>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-4 pb-4">
            <p className="mb-4 text-muted-foreground text-sm">
              Connect your Slack workspace to receive notifications for security events and deploy
              approvals.
            </p>
            <div>
              <Button disabled={isConnecting || connectPending} onClick={onConnect}>
                {isConnecting || connectPending ? (
                  <Loader2 className="mr-2 size-4 animate-spin" />
                ) : null}
                Connect to Slack
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
