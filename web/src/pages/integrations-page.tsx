import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Circle, Loader2, Plus, Trash2, X } from 'lucide-react'
import { useState } from 'react'
import { StringArrayInput } from '../components/settings/string-array-input'
import { Alert, AlertDescription } from '../components/ui/alert'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { NativeSelect } from '../components/ui/native-select'
import { toast } from '../components/ui/use-toast'
import { api } from '../lib/api'

// Helper to extract error message from API errors
function getErrorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object' && 'response' in error) {
    const response = error.response
    if (response && typeof response === 'object' && 'data' in response) {
      const data = response.data
      if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
        return data.error
      }
    }
  }
  return fallback
}

// Type guard for API errors with status codes
function hasStatus(error: unknown, status: number): boolean {
  return !!(
    error &&
    typeof error === 'object' &&
    'response' in error &&
    error.response &&
    typeof error.response === 'object' &&
    'status' in error.response &&
    error.response.status === status
  )
}

type SlackChannel = {
  id: string
  name: string
}

type ChannelConfig = {
  id: string | null
  name: string
  actions: string[]
}

type SlackIntegration = {
  id: number
  workspace_id: string
  workspace_name: string
  channel_actions: Record<string, ChannelConfig>
  created_at: string
  updated_at: string
}

type SlackConfig = {
  configured: boolean
}

type CircleCISettings = {
  api_token: string
  org_slug: string
}

type GitHubSettings = {
  token: string
  repo?: string
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Page component with multiple integration handlers
export function IntegrationsPage() {
  const [isConnecting, setIsConnecting] = useState(false)
  const queryClient = useQueryClient()

  // Check if Slack is configured
  const { data: slackConfig } = useQuery<SlackConfig>({
    queryKey: ['slack-config'],
    queryFn: async () => {
      try {
        await api.post('/.gateway/api/admin/integrations/slack/oauth/authorize')
        return { configured: true }
      } catch (error: unknown) {
        if (hasStatus(error, 503)) {
          return { configured: false }
        }
        return { configured: true }
      }
    },
  })

  // Fetch CircleCI settings
  const { data: circleCISettings } = useQuery<CircleCISettings | null>({
    queryKey: ['circleci-settings'],
    queryFn: async (): Promise<CircleCISettings | null> => {
      try {
        const response = await api.get<CircleCISettings>('/.gateway/api/admin/settings/circleci')
        return response
      } catch (error: unknown) {
        if (hasStatus(error, 404)) {
          return null
        }
        throw error
      }
    },
  })

  // Fetch GitHub settings
  const { data: gitHubSettings } = useQuery<GitHubSettings | null>({
    queryKey: ['github-settings'],
    queryFn: async (): Promise<GitHubSettings | null> => {
      try {
        const response = await api.get<GitHubSettings>('/.gateway/api/admin/settings/github')
        return response
      } catch (error: unknown) {
        if (hasStatus(error, 404)) {
          return null
        }
        throw error
      }
    },
  })

  // Fetch Slack integration status
  const { data: integration, isLoading } = useQuery<SlackIntegration | null>({
    queryKey: ['slack-integration'],
    queryFn: async (): Promise<SlackIntegration | null> => {
      try {
        const response = await api.get<SlackIntegration>('/.gateway/api/admin/integrations/slack')
        return response
      } catch (error: unknown) {
        if (hasStatus(error, 404)) {
          return null
        }
        throw error
      }
    },
  })

  // Fetch available Slack channels
  const { data: channelsData } = useQuery<SlackChannel[]>({
    queryKey: ['slack-channels'],
    queryFn: async () => {
      const response = await api.get<{ channels: SlackChannel[] }>(
        '/.gateway/api/admin/integrations/slack/channels/list'
      )
      return response.channels || []
    },
    enabled: !!integration,
  })

  // Connect to Slack mutation
  const connectMutation = useMutation({
    mutationFn: async () => {
      const response = await api.post<{ authorization_url: string }>(
        '/.gateway/api/admin/integrations/slack/oauth/authorize'
      )
      return response.authorization_url
    },
    onSuccess: (authUrl) => {
      window.location.href = authUrl
    },
    onError: (error: unknown) => {
      const errorMessage = getErrorMessage(error, 'Failed to start Slack authorization')
      toast.error(errorMessage)
      setIsConnecting(false)
    },
  })

  // Disconnect mutation
  const disconnectMutation = useMutation({
    mutationFn: async () => {
      await api.delete('/.gateway/api/admin/integrations/slack')
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['slack-integration'] })
      toast.success('Disconnected from Slack')
    },
    onError: (error: unknown) => {
      const errorMessage = getErrorMessage(error, 'Failed to disconnect')
      toast.error(errorMessage)
    },
  })

  // Update channels mutation
  const updateChannelsMutation = useMutation({
    mutationFn: async (channelActions: Record<string, ChannelConfig>) => {
      await api.put('/.gateway/api/admin/integrations/slack/channels', {
        channel_actions: channelActions,
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['slack-integration'] })
      toast.success('Channel configuration updated')
    },
    onError: (error: unknown) => {
      const errorMessage = getErrorMessage(error, 'Failed to update channels')
      toast.error(errorMessage)
    },
  })

  // Test notification mutation
  const testMutation = useMutation({
    mutationFn: async (channelId: string) => {
      await api.post('/.gateway/api/admin/integrations/slack/test', { channel_id: channelId })
    },
    onSuccess: () => {
      toast.success('Test notification sent')
    },
    onError: (error: unknown) => {
      const errorMessage = getErrorMessage(error, 'Failed to send test notification')
      toast.error(errorMessage)
    },
  })

  const handleConnect = () => {
    setIsConnecting(true)
    connectMutation.mutate()
  }

  const handleDisconnect = () => {
    if (confirm('Are you sure you want to disconnect from Slack?')) {
      disconnectMutation.mutate()
    }
  }

  const handleUpdateChannel = (key: string, channelId: string, channelName: string) => {
    if (!integration) return

    const updatedActions = {
      ...integration.channel_actions,
      [key]: {
        ...integration.channel_actions[key],
        id: channelId,
        name: channelName,
      },
    }

    updateChannelsMutation.mutate(updatedActions)
  }

  const handleAddAction = (key: string, action: string) => {
    if (!(integration && action.trim())) return

    const currentConfig = integration.channel_actions[key]
    const updatedActions = {
      ...integration.channel_actions,
      [key]: {
        ...currentConfig,
        actions: [...currentConfig.actions, action.trim()],
      },
    }

    updateChannelsMutation.mutate(updatedActions)
  }

  const handleRemoveAction = (key: string, actionIndex: number) => {
    if (!integration) return

    const currentConfig = integration.channel_actions[key]
    const updatedActions = {
      ...integration.channel_actions,
      [key]: {
        ...currentConfig,
        actions: currentConfig.actions.filter((_: string, i: number) => i !== actionIndex),
      },
    }

    updateChannelsMutation.mutate(updatedActions)
  }

  const handleAddChannel = (channelName: string) => {
    if (!(integration && channelName.trim())) return

    const key = channelName.toLowerCase().replace(/[^a-z0-9]/g, '-')
    const updatedActions = {
      ...integration.channel_actions,
      [key]: {
        id: null,
        name: channelName,
        actions: [],
      },
    }

    updateChannelsMutation.mutate(updatedActions)
  }

  const handleRemoveChannel = (key: string) => {
    if (!integration) return

    const updatedActions = { ...integration.channel_actions }
    delete updatedActions[key]

    updateChannelsMutation.mutate(updatedActions)
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
  const circleCIEnabled = !!(
    circleCISettings?.api_token?.trim() && circleCISettings?.org_slug?.trim()
  )
  const gitHubEnabled = !!gitHubSettings?.token?.trim()

  return (
    <div className="container mx-auto p-6">
      <div className="mb-6">
        <h1 className="font-bold text-3xl">Integrations</h1>
        <p className="text-muted-foreground">
          Connect external services to receive notifications and automate workflows
        </p>
      </div>

      <div className="space-y-6">
        {/* CircleCI and GitHub Cards - Side by Side */}
        <div className="grid grid-cols-2 gap-6">
          {/* CircleCI Card */}
          <Card>
            <CardHeader>
              <div className="flex items-start justify-between">
                <div>
                  <CardTitle className="flex items-center gap-2">
                    <svg className="size-6" fill="currentColor" viewBox="0 0 103.8 105.2">
                      <title>CircleCI logo</title>
                      <path d="m 38.6,52.6 c 0,-6.9 5.6,-12.5 12.5,-12.5 6.9,0 12.5,5.6 12.5,12.5 0,6.9 -5.6,12.5 -12.5,12.5 C 44.2,65.2 38.6,59.5 38.6,52.6 Z M 51.1,0 C 26.5,0 5.9,16.8 0.1,39.6 0.1,39.8 0,39.9 0,40.1 c 0,1.4 1.1,2.5 2.5,2.5 l 21.2,0 c 1,0 1.9,-0.6 2.3,-1.5 l 0,0 C 30.4,31.6 39.9,25 51.1,25 66.3,25 78.7,37.4 78.7,52.6 78.7,67.8 66.3,80.2 51.1,80.2 40,80.2 30.4,73.6 26,64.1 l 0,0 c -0.4,-0.9 -1.3,-1.5 -2.3,-1.5 l -21.2,0 c -1.4,0 -2.5,1.1 -2.5,2.5 0,0.2 0,0.3 0.1,0.5 5.8,22.8 26.4,39.6 51,39.6 29.1,0 52.7,-23.6 52.7,-52.7 C 103.8,23.5 80.2,0 51.1,0 Z" />
                    </svg>
                    CircleCI
                  </CardTitle>
                  <CardDescription>
                    Auto-approve CircleCI jobs after deploy approval
                  </CardDescription>
                </div>
                <div className="flex items-center gap-2">
                  {circleCIEnabled ? (
                    <CheckCircle2 className="size-5 text-green-600" />
                  ) : (
                    <Circle className="size-5 text-muted-foreground" />
                  )}
                </div>
              </div>
            </CardHeader>
            <CardContent>
              {circleCIEnabled ? (
                <div className="space-y-3">
                  <Alert>
                    <CheckCircle2 className="size-4" />
                    <AlertDescription>
                      CircleCI integration is enabled. The gateway will automatically approve
                      CircleCI jobs when deploy approvals are granted.
                    </AlertDescription>
                  </Alert>
                  <div className="rounded border bg-muted p-4">
                    <div className="space-y-2">
                      <div className="flex justify-between text-sm">
                        <span className="font-medium">Organization:</span>
                        <code className="rounded bg-background px-2 py-1">
                          {circleCISettings?.org_slug}
                        </code>
                      </div>
                      <div className="flex justify-between text-sm">
                        <span className="font-medium">API Token:</span>
                        <span className="text-muted-foreground">Configured</span>
                      </div>
                    </div>
                  </div>
                  <p className="text-muted-foreground text-xs">
                    API token set via{' '}
                    <code className="ml-1 rounded bg-muted px-1 py-0.5">CIRCLECI_TOKEN</code> env
                    var.
                  </p>
                  <p className="text-muted-foreground text-xs">
                    Per-app organization slugs and job names are configured in app settings.
                  </p>
                </div>
              ) : (
                <div className="flex flex-col gap-4">
                  <p className="text-muted-foreground text-sm">
                    Set the CircleCI API token environment variable to enable:
                  </p>
                  <div className="w-full space-y-2 rounded border bg-muted p-4 font-mono text-sm">
                    <div>CIRCLECI_TOKEN=your-api-token</div>
                  </div>
                  <p className="text-muted-foreground text-xs">
                    Per-app organization slugs are configured in app settings.
                  </p>
                </div>
              )}
            </CardContent>
          </Card>

          {/* GitHub Card */}
          <Card>
            <CardHeader>
              <div className="flex items-start justify-between">
                <div>
                  <CardTitle className="flex items-center gap-2">
                    <svg className="size-6" fill="currentColor" viewBox="0 0 24 24">
                      <title>GitHub logo</title>
                      <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
                    </svg>
                    GitHub
                  </CardTitle>
                  <CardDescription>Verify commits and PRs for deployments</CardDescription>
                </div>
                <div className="flex items-center gap-2">
                  {gitHubEnabled ? (
                    <CheckCircle2 className="size-5 text-green-600" />
                  ) : (
                    <Circle className="size-5 text-muted-foreground" />
                  )}
                </div>
              </div>
            </CardHeader>
            <CardContent>
              {gitHubEnabled ? (
                <div className="space-y-3">
                  <Alert>
                    <CheckCircle2 className="size-4" />
                    <AlertDescription>
                      GitHub integration is enabled. The gateway can verify git commits and PRs for
                      deploy approval requests.
                    </AlertDescription>
                  </Alert>
                  <div className="rounded border bg-muted p-4">
                    <div className="space-y-2">
                      {gitHubSettings?.repo && (
                        <div className="flex justify-between text-sm">
                          <span className="font-medium">Repository:</span>
                          <code className="rounded bg-background px-2 py-1">
                            {gitHubSettings.repo}
                          </code>
                        </div>
                      )}
                      <div className="flex justify-between text-sm">
                        <span className="font-medium">API Token:</span>
                        <span className="text-muted-foreground">Configured</span>
                      </div>
                    </div>
                  </div>
                  <p className="text-muted-foreground text-xs">
                    API token set via{' '}
                    <code className="ml-1 rounded bg-muted px-1 py-0.5">GITHUB_TOKEN</code> env var.
                    Per-app repositories are configured in app settings.
                  </p>
                </div>
              ) : (
                <div className="flex flex-col gap-4">
                  <p className="text-muted-foreground text-sm">
                    Set the GitHub API token environment variable to enable:
                  </p>
                  <div className="w-full space-y-2 rounded border bg-muted p-4 font-mono text-sm">
                    <div>GITHUB_TOKEN=your-personal-access-token</div>
                  </div>
                  <p className="text-muted-foreground text-xs">
                    Per-app repositories are configured in app settings.
                  </p>
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        {/* Slack Card */}
        {slackConfigured ? (
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
                    disabled={disconnectMutation.isPending}
                    onClick={handleDisconnect}
                    size="sm"
                    variant="destructive"
                  >
                    {disconnectMutation.isPending ? (
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
                    <CheckCircle2 className="size-4" />
                    <AlertDescription>
                      Slack is connected. Configure which channels receive notifications below.
                    </AlertDescription>
                  </Alert>

                  <div className="space-y-4">
                    <h3 className="font-semibold text-lg">Channel Configuration</h3>

                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                      {Object.entries(integration.channel_actions).map(([key, config]) => (
                        <ChannelConfigCard
                          channels={channelsData || []}
                          config={config as ChannelConfig}
                          configKey={key}
                          isTesting={testMutation.isPending}
                          isUpdating={updateChannelsMutation.isPending}
                          key={key}
                          onAddAction={handleAddAction}
                          onRemoveAction={handleRemoveAction}
                          onRemoveChannel={handleRemoveChannel}
                          onTestNotification={handleTestNotification}
                          onUpdateChannel={handleUpdateChannel}
                        />
                      ))}
                    </div>

                    <div className="mt-6 flex justify-end">
                      <AddChannelButton
                        isUpdating={updateChannelsMutation.isPending}
                        onAdd={handleAddChannel}
                      />
                    </div>
                  </div>
                </div>
              ) : (
                <div className="flex flex-col gap-4 pb-4">
                  <p className="mb-4 text-muted-foreground text-sm">
                    Connect your Slack workspace to receive notifications for security events and
                    deploy approvals.
                  </p>
                  <div>
                    <Button
                      disabled={isConnecting || connectMutation.isPending}
                      onClick={handleConnect}
                    >
                      {isConnecting || connectMutation.isPending ? (
                        <Loader2 className="mr-2 size-4 animate-spin" />
                      ) : null}
                      Connect to Slack
                    </Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        ) : null}
      </div>
    </div>
  )
}

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

function ChannelConfigCard({
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
    // Determine what changed by comparing lengths and content
    if (newActions.length > config.actions.length) {
      // Item was added - find the new one
      const newAction = newActions.find((a) => !config.actions.includes(a))
      if (newAction) {
        onAddAction(configKey, newAction)
      }
    } else if (newActions.length < config.actions.length) {
      // Item was removed - find which index
      const removedIndex = config.actions.findIndex((a) => !newActions.includes(a))
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
              onChange={(e) => {
                const value = e.target.value
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

type AddChannelButtonProps = {
  onAdd: (channelName: string) => void
  isUpdating: boolean
}

function AddChannelButton({ onAdd, isUpdating }: AddChannelButtonProps) {
  const [channelName, setChannelName] = useState('')
  const [isAdding, setIsAdding] = useState(false)

  const handleAdd = () => {
    if (channelName.trim()) {
      onAdd(channelName)
      setChannelName('')
      setIsAdding(false)
    }
  }

  if (!isAdding) {
    return (
      <Button onClick={() => setIsAdding(true)} variant="outline">
        <Plus className="mr-2 size-4" />
        Add Channel
      </Button>
    )
  }

  return (
    <div className="flex gap-2">
      <Input
        autoFocus
        onChange={(e) => setChannelName(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            handleAdd()
          }
          if (e.key === 'Escape') {
            setIsAdding(false)
            setChannelName('')
          }
        }}
        placeholder="Channel name (e.g., #security)"
        value={channelName}
      />
      <Button disabled={isUpdating || !channelName.trim()} onClick={handleAdd}>
        Add
      </Button>
      <Button
        onClick={() => {
          setIsAdding(false)
          setChannelName('')
        }}
        variant="ghost"
      >
        Cancel
      </Button>
    </div>
  )
}

export default IntegrationsPage
