export type SlackChannel = {
	id: string
	name: string
}

export type ChannelConfig = {
	id: string | null
	name: string
	actions: string[]
}

export type SlackIntegration = {
	id: number
	workspace_id: string
	workspace_name: string
	channel_actions: Record<string, ChannelConfig>
	created_at: string
	updated_at: string
}

export type SlackConfig = {
	configured: boolean
}

export type CircleCISettings = {
	api_token: string
	org_slug: string
}

export type GitHubSettings = {
	token: string
	repo?: string
}
