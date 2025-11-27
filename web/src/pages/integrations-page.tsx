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
import { getErrorMessage, hasStatus } from '@/pages/integrations/utils'

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
    onError: (error: unknown) => {
      const message = getErrorMessage(error, 'Failed to start Slack authorization')
      toast.error(message)
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
    onError: (error: unknown) => {
      const message = getErrorMessage(error, 'Failed to disconnect')
      toast.error(message)
    },
  })

  const updateChannelsMutation = useMutation({
    mutationFn: async (channelActions: Record<string, ChannelConfig>) => {
      await api.put('/api/v1/integrations/slack/channels', {
        channel_actions: channelActions,
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.SLACK_INTEGRATION })
      toast.success('Channel configuration updated')
    },
    onError: (error: unknown) => {
      const message = getErrorMessage(error, 'Failed to update channels')
      toast.error(message)
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
    onError: (error: unknown) => {
      const message = getErrorMessage(error, 'Failed to send test notification')
      toast.error(message)
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
    onError: (error: unknown) => {
      const message = getErrorMessage(error, 'Failed to update alert settings')
      toast.error(message)
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

  const handleUpdateChannel = (key: string, channelId: string, channelName: string) => {
    if (!integration?.channel_actions) {
      return
    }

    const channelActions = integration.channel_actions as Record<string, ChannelConfig>
    updateChannelsMutation.mutate({
      ...channelActions,
      [key]: {
        ...channelActions[key],
        id: channelId,
        name: channelName,
      },
    })
  }

  const handleAddAction = (key: string, action: string) => {
    if (!(integration?.channel_actions && action.trim())) {
      return
    }

    const channelActions = integration.channel_actions as Record<string, ChannelConfig>
    const currentConfig = channelActions[key]
    updateChannelsMutation.mutate({
      ...channelActions,
      [key]: {
        ...currentConfig,
        actions: [...currentConfig.actions, action.trim()],
      },
    })
  }

  const handleRemoveAction = (key: string, actionIndex: number) => {
    if (!integration?.channel_actions) {
      return
    }

    const channelActions = integration.channel_actions as Record<string, ChannelConfig>
    const currentConfig = channelActions[key]
    updateChannelsMutation.mutate({
      ...channelActions,
      [key]: {
        ...currentConfig,
        actions: currentConfig.actions.filter((_, index) => index !== actionIndex),
      },
    })
  }

  const handleAddChannel = (channelName: string) => {
    if (!(integration?.channel_actions && channelName.trim())) {
      return
    }

    const key = channelName.toLowerCase().replace(/[^a-z0-9]/g, '-')
    const channelActions = integration.channel_actions as Record<string, ChannelConfig>
    updateChannelsMutation.mutate({
      ...channelActions,
      [key]: {
        id: null,
        name: channelName,
        actions: [],
      },
    })
  }

  const handleRemoveChannel = (key: string) => {
    if (!integration?.channel_actions) {
      return
    }

    const channelActions = integration.channel_actions as Record<string, ChannelConfig>
    const remaining = { ...channelActions }
    delete remaining[key]
    updateChannelsMutation.mutate(remaining)
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

type DisconnectDialogProps = {
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
