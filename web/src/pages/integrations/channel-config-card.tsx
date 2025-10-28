import { Loader2, X } from 'lucide-react'

import { StringArrayInput } from '@/components/settings/string-array-input'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'

import type { ChannelConfig, SlackChannel } from '@/pages/integrations/types'

type ChannelConfigCardProps = {
	configKey: string
	config: ChannelConfig
	channels: SlackChannel[]
	onUpdateChannel: (key: string, channelId: string, channelName: string) => void
	onAddAction: (key: string, action: string) => void
	onRemoveAction: (key: string, actionIndex: number) => void
	onRemoveChannel: (key: string) => void
	onTestNotification: (channelId: string) => void
	isUpdating: boolean
	isTesting: boolean
}

export function ChannelConfigCard({
	configKey,
	config,
	channels,
	onUpdateChannel,
	onAddAction,
	onRemoveAction,
	onRemoveChannel,
	onTestNotification,
	isUpdating,
	isTesting,
}: ChannelConfigCardProps) {
	const handleActionsChange = (newActions: string[]) => {
		if (newActions.length > config.actions.length) {
			const newAction = newActions.find((action) => !config.actions.includes(action))
			if (newAction) {
				onAddAction(configKey, newAction)
			}
			return
		}

		if (newActions.length < config.actions.length) {
			const removedIndex = config.actions.findIndex((action) => !newActions.includes(action))
			if (removedIndex !== -1) {
				onRemoveAction(configKey, removedIndex)
			}
		}
	}

	return (
		<Card>
			<CardHeader>
				<div className="flex items-center justify-between">
					<CardTitle className="text-base">{config.name}</CardTitle>
					<Button
						disabled={isUpdating}
						onClick={() => onRemoveChannel(configKey)}
						size="sm"
						variant="ghost"
					>
						<X className="size-4" />
					</Button>
				</div>
			</CardHeader>
			<CardContent className="space-y-4">
				<div className="space-y-2">
					<Label htmlFor={`channel-${configKey}`}>Slack Channel</Label>
					<div className="flex gap-2">
						<NativeSelect
							className="flex-1"
							disabled={isUpdating}
							id={`channel-${configKey}`}
							onChange={(event) => {
								const value = event.target.value
								const channel = channels.find((c) => c.id === value)
								if (channel) {
									onUpdateChannel(configKey, channel.id, channel.name)
								}
							}}
							value={config.id || ''}
						>
							<option value="">Select a channel...</option>
							{channels.map((channel) => (
								<option key={channel.id} value={channel.id}>
									{channel.name}
								</option>
							))}
						</NativeSelect>
						{config.id && (
							<Button
								disabled={isTesting}
								onClick={() => onTestNotification(config.id!)}
								size="sm"
								variant="outline"
							>
								{isTesting ? <Loader2 className="size-4 animate-spin" /> : 'Test'}
							</Button>
						)}
					</div>
				</div>

				<div className="space-y-2">
					<Label>Action Patterns</Label>
					<StringArrayInput
						disabled={isUpdating}
						onChange={handleActionsChange}
						placeholder="e.g., mfa.*, deploy-approval-request.*"
						value={config.actions}
					/>
					<p className="text-muted-foreground text-xs">
						Use glob patterns like <code>mfa.*</code> to match actions. View audit logs to see
						available actions.
					</p>
				</div>
			</CardContent>
		</Card>
	)
}
