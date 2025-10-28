import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'

import { useMutation } from '@/hooks/use-mutation'
import { QUERY_KEYS } from '@/lib/query-keys'
import { toast } from '@/components/ui/use-toast'
import { api } from '@/lib/api'
import { CircleCiCard } from '@/pages/integrations/circleci-card'
import { GitHubCard } from '@/pages/integrations/github-card'
import { SlackCard } from '@/pages/integrations/slack-card'
import type {
	ChannelConfig,
	CircleCISettings,
	GitHubSettings,
	SlackChannel,
	SlackConfig,
	SlackIntegration,
} from '@/pages/integrations/types'
import { getErrorMessage, hasStatus } from '@/pages/integrations/utils'

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Page composes multiple integration handlers
export function IntegrationsPage() {
	const [isConnecting, setIsConnecting] = useState(false)
	const queryClient = useQueryClient()

	const { data: slackConfig } = useQuery<SlackConfig>({
		queryKey: ['slack-config'],
		queryFn: async () => {
			try {
				await api.post('/api/v1/integrations/slack/oauth/authorize')
				return { configured: true }
			} catch (error: unknown) {
				if (hasStatus(error, 503)) {
					return { configured: false }
				}
				return { configured: true }
			}
		},
	})

	const { data: circleCISettings } = useQuery<CircleCISettings | null>({
		queryKey: ['circleci-settings'],
		queryFn: async () => {
			try {
				return await api.get<CircleCISettings>('/api/v1/settings/circleci')
			} catch (error: unknown) {
				if (hasStatus(error, 404)) {
					return null
				}
				throw error
			}
		},
	})

	const { data: gitHubSettings } = useQuery<GitHubSettings | null>({
		queryKey: ['github-settings'],
		queryFn: async () => {
			try {
				return await api.get<GitHubSettings>('/api/v1/settings/github')
			} catch (error: unknown) {
				if (hasStatus(error, 404)) {
					return null
				}
				throw error
			}
		},
	})

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
		enabled: !!integration,
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

	const handleConnect = () => {
		setIsConnecting(true)
		connectMutation.mutate()
	}

	const handleDisconnect = () => {
		if (window.confirm('Are you sure you want to disconnect from Slack?')) {
			disconnectMutation.mutate()
		}
	}

	const handleUpdateChannel = (key: string, channelId: string, channelName: string) => {
		if (!integration) {
			return
		}

		updateChannelsMutation.mutate({
			...integration.channel_actions,
			[key]: {
				...integration.channel_actions[key],
				id: channelId,
				name: channelName,
			},
		})
	}

	const handleAddAction = (key: string, action: string) => {
		if (!(integration && action.trim())) {
			return
		}

		const currentConfig = integration.channel_actions[key]
		updateChannelsMutation.mutate({
			...integration.channel_actions,
			[key]: {
				...currentConfig,
				actions: [...currentConfig.actions, action.trim()],
			},
		})
	}

	const handleRemoveAction = (key: string, actionIndex: number) => {
		if (!integration) {
			return
		}

		const currentConfig = integration.channel_actions[key]
		updateChannelsMutation.mutate({
			...integration.channel_actions,
			[key]: {
				...currentConfig,
				actions: currentConfig.actions.filter((_, index) => index !== actionIndex),
			},
		})
	}

	const handleAddChannel = (channelName: string) => {
		if (!(integration && channelName.trim())) {
			return
		}

		const key = channelName.toLowerCase().replace(/[^a-z0-9]/g, '-')
		updateChannelsMutation.mutate({
			...integration.channel_actions,
			[key]: {
				id: null,
				name: channelName,
				actions: [],
			},
		})
	}

	const handleRemoveChannel = (key: string) => {
		if (!integration) {
			return
		}

		const remaining = { ...integration.channel_actions }
		delete remaining[key]
		updateChannelsMutation.mutate(remaining)
	}

	const handleTestNotification = (channelId: string) => {
		testMutation.mutate(channelId)
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

	const slackConfigured = slackConfig?.configured !== false

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
					<CircleCiCard settings={circleCISettings ?? undefined} />
					<GitHubCard settings={gitHubSettings ?? undefined} />
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
					slackConfigured={slackConfigured}
					testPending={testMutation.isPending}
					updatePending={updateChannelsMutation.isPending}
				/>
			</div>
		</div>
	)
}
