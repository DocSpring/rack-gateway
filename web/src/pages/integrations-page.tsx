import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useCallback, useState } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toast } from '@/components/ui/use-toast'
import { useAuth } from '@/contexts/auth-context'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { QUERY_KEYS } from '@/lib/query-keys'
import { CircleCiCard } from '@/pages/integrations/circleci-card'
import { GitHubCard } from '@/pages/integrations/github-card'
import { SlackCard } from '@/pages/integrations/slack-card'
import type {
  ChannelConfig,
  SlackChannel,
  SlackIntegration,
} from '@/pages/integrations/slack-types'

type ChannelActions = Record<string, ChannelConfig>

import { hasStatus } from '@/pages/integrations/utils'

export function IntegrationsPage() {
  const [isConnecting, setIsConnecting] = useState(false)
  const [showDisconnectDialog, setShowDisconnectDialog] = useState(false)
  const queryClient = useQueryClient()
  const { user } = useAuth()

  const { data: integration, isLoading } = useQuery<SlackIntegration | null>({
    queryKey: QUERY_KEYS.SLACK_INTEGRATION,
    queryFn: async () => {
      try {
        return await api.get<SlackIntegration>('/api/v1/integrations/slack')
      } catch (error: unknown) {
        if (hasStatus(error, 404)) {
          return null
        }
        throw error
      }
    },
  })

  const { data: channels } = useQuery<SlackChannel[]>({
    queryKey: ['slack-channels'],
    queryFn: async () => {
      const response = await api.get<{ channels: SlackChannel[] }>(
        '/api/v1/integrations/slack/channels/list'
      )
      return response.channels || []
    },
    enabled: Boolean(integration),
  })

  const connectMutation = useMutation({
    mutationFn: async () => {
      const response = await api.post<{ authorization_url: string }>(
        '/api/v1/integrations/slack/oauth/authorize'
      )
      return response.authorization_url
    },
    onSuccess: (authUrl) => {
      window.location.href = authUrl
    },
    onError: () => {
      setIsConnecting(false)
    },
  })

  const disconnectMutation = useMutation({
    mutationFn: async () => {
      await api.delete('/api/v1/integrations/slack')
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.SLACK_INTEGRATION })
      toast.success('Disconnected from Slack')
    },
  })

  // Each save sends the configuration the change was made to, and the gateway refuses it (409) if that has changed
  // since, so an edit can't undo one saved from another tab. Returning the refetch keeps the controls disabled until
  // the saved configuration is on screen, so the next edit is made to it.
  const updateChannelsMutation = useMutation({
    mutationFn: async ({ previous, next }: { previous: ChannelActions; next: ChannelActions }) => {
      await api.put('/api/v1/integrations/slack/channels', {
        channel_actions: next,
        previous_channel_actions: previous,
      })
    },
    onSuccess: () => {
      toast.success('Channel configuration updated')
      return queryClient.invalidateQueries({ queryKey: QUERY_KEYS.SLACK_INTEGRATION })
    },
    onError: (error: unknown) => {
      if (hasStatus(error, 409)) {
        queryClient.invalidateQueries({ queryKey: QUERY_KEYS.SLACK_INTEGRATION })
      }
    },
  })

  const testMutation = useMutation({
    mutationFn: async (channelId: string) => {
      await api.post('/api/v1/integrations/slack/test', {
        channel_id: channelId,
      })
    },
    onSuccess: () => {
      toast.success('Test notification sent')
    },
  })

  const updateAlertsMutation = useMutation({
    mutationFn: async ({ enabled, channelId }: { enabled: boolean; channelId: string }) => {
      await api.put('/api/v1/integrations/slack/alerts', {
        deploy_approvals_enabled: enabled,
        deploy_approvals_channel_id: channelId,
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.SLACK_INTEGRATION })
      toast.success('Alert settings updated')
    },
  })

  const handleConnect = useCallback(() => {
    setIsConnecting(true)
    connectMutation.mutate()
  }, [connectMutation])

  const handleDisconnect = useCallback(() => {
    setShowDisconnectDialog(true)
  }, [])

  const confirmDisconnect = useCallback(() => {
    disconnectMutation.mutate()
    setShowDisconnectDialog(false)
  }, [disconnectMutation])

  const handleCancelDisconnect = useCallback(() => {
    setShowDisconnectDialog(false)
  }, [])

  // saveChannels saves change applied to the configuration on screen.
  const saveChannels = (change: (current: ChannelActions) => ChannelActions) => {
    if (!integration?.channel_actions) {
      return
    }
    const previous = integration.channel_actions as ChannelActions
    updateChannelsMutation.mutate({ previous, next: change(previous) })
  }

  const handleUpdateChannel = (key: string, channelId: string, channelName: string) => {
    saveChannels((current) => ({
      ...current,
      [key]: { ...current[key], id: channelId, name: channelName },
    }))
  }

  const handleAddAction = (key: string, action: string) => {
    const pattern = action.trim()
    if (!pattern) {
      return
    }
    saveChannels((current) => ({
      ...current,
      [key]: { ...current[key], actions: [...current[key].actions, pattern] },
    }))
  }

  const handleRemoveAction = (key: string, action: string) => {
    saveChannels((current) => ({
      ...current,
      [key]: {
        ...current[key],
        actions: current[key].actions.filter((pattern) => pattern !== action),
      },
    }))
  }

  const handleAddChannel = (channelName: string) => {
    if (!channelName.trim()) {
      return
    }
    const key = channelName.toLowerCase().replace(/[^a-z0-9]/g, '-')
    saveChannels((current) => ({ ...current, [key]: { id: null, name: channelName, actions: [] } }))
  }

  const handleRemoveChannel = (key: string) => {
    saveChannels((current) => {
      const remaining = { ...current }
      delete remaining[key]
      return remaining
    })
  }

  const handleTestNotification = (channelId: string) => {
    testMutation.mutate(channelId)
  }

  const handleUpdateDeployApprovalAlerts = (enabled: boolean, channelId: string) => {
    updateAlertsMutation.mutate({ enabled, channelId })
  }

  if (isLoading) {
    return (
      <div className="container mx-auto p-6">
        <div className="flex items-center justify-center py-12">
          <Loader2 className="size-8 animate-spin text-muted-foreground" />
        </div>
      </div>
    )
  }

  const slackConfigured = user?.integrations?.slack ?? false

  return (
    <div className="container mx-auto p-6">
      <div className="mb-6">
        <h1 className="font-bold text-3xl">Integrations</h1>
        <p className="text-muted-foreground">
          Connect external services to receive notifications and automate workflows
        </p>
      </div>

      <div className="space-y-6">
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <CircleCiCard enabled={user?.integrations?.circleci ?? false} />
          <GitHubCard enabled={user?.integrations?.github ?? false} />
        </div>

        <SlackCard
          channels={channels}
          connectPending={connectMutation.isPending}
          disconnectPending={disconnectMutation.isPending}
          integration={integration}
          isConnecting={isConnecting}
          onAddAction={handleAddAction}
          onAddChannel={handleAddChannel}
          onConnect={handleConnect}
          onDisconnect={handleDisconnect}
          onRemoveAction={handleRemoveAction}
          onRemoveChannel={handleRemoveChannel}
          onTestNotification={handleTestNotification}
          onUpdateChannel={handleUpdateChannel}
          onUpdateDeployApprovalAlerts={handleUpdateDeployApprovalAlerts}
          slackConfigured={slackConfigured}
          testPending={testMutation.isPending}
          updateAlertsPending={updateAlertsMutation.isPending}
          updatePending={updateChannelsMutation.isPending}
        />
      </div>

      <DisconnectDialog
        onCancel={handleCancelDisconnect}
        onConfirm={confirmDisconnect}
        open={showDisconnectDialog}
      />
    </div>
  )
}

interface DisconnectDialogProps {
  open: boolean
  onCancel: () => void
  onConfirm: () => void
}

function DisconnectDialog({ open, onCancel, onConfirm }: DisconnectDialogProps) {
  const handleOpenChange = useCallback(
    (isOpen: boolean) => {
      if (!isOpen) {
        onCancel()
      }
    },
    [onCancel]
  )

  return (
    <Dialog onOpenChange={handleOpenChange} open={open}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Disconnect from Slack</DialogTitle>
          <DialogDescription>
            Are you sure you want to disconnect from Slack? This will remove all channel
            configurations and notifications.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button onClick={onCancel} variant="outline">
            Cancel
          </Button>
          <Button onClick={onConfirm} variant="destructive">
            Disconnect
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
