import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { RefreshCw } from 'lucide-react'

import { useAuth } from '@/contexts/auth-context'
import { api } from '@/lib/api'
import { apiPath } from '@/lib/api-path'
import { CommandJustificationCard } from '@/pages/app-settings/command-justification-card'
import { JsonSettingCard } from '@/pages/app-settings/json-setting-card'
import { StringArrayCard } from '@/pages/app-settings/string-array-card'
import type { AppSettingsResponse } from '@/pages/app-settings/types'
import { VCSCIProvidersCard } from '@/pages/app-settings/vcs-ci-providers-card'
import type { GlobalSettingsResponse } from '@/pages/settings/types'

export function AppSettingsPage() {
  const { app } = useParams({ from: '/_app/apps/$app/settings' }) as { app: string }
  const { user } = useAuth()
  const isAdmin = Boolean(user?.roles?.includes('admin'))

  const {
    data: appSettings,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['appSettings', app],
    queryFn: async () => api.get<AppSettingsResponse>(apiPath`/api/v1/apps/${app}/settings`),
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  // The gateway-wide require_command_justification applies to every app, whatever the app's own setting says.
  const { data: globalSettings } = useQuery({
    queryKey: ['globalSettings'],
    queryFn: () => api.get<GlobalSettingsResponse>('/api/v1/settings'),
    staleTime: 0,
  })

  if (isLoading) {
    return (
      <div>
        <div className="flex h-64 items-center justify-center">
          <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </div>
    )
  }

  return (
    <div>
      {error ? (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-destructive text-sm">
          Failed to load settings
        </div>
      ) : null}

      {/* Keyed by app: the cards keep unsaved edits in local state, which must not carry over to another app */}
      <div className="grid gap-6" key={app}>
        <VCSCIProvidersCard
          app={app}
          disabled={!isAdmin}
          integrations={user?.integrations}
          settings={appSettings}
        />
        <div className="grid grid-cols-2 gap-12">
          <StringArrayCard
            app={app}
            description="Environment variables that are protected (values masked) and cannot be changed."
            disabled={!isAdmin}
            pathSegment="protected-env-vars"
            placeholder="e.g. DATABASE_URL"
            settingKey="protected_env_vars"
            settings={appSettings}
            title="Protected Environment Variables"
          />
          <StringArrayCard
            app={app}
            description="Environment variables that are treated as secrets (values masked) but can still be changed."
            disabled={!isAdmin}
            pathSegment="secret-env-vars"
            placeholder="e.g. API_KEY"
            settingKey="secret_env_vars"
            settings={appSettings}
            title="Secret Environment Variables"
          />
        </div>
        <div className="grid grid-cols-2 gap-12">
          <StringArrayCard
            app={app}
            description="Audit logs and Slack notifications show the values of these environment variables and redact all others. Secret and protected values are always redacted."
            disabled={!isAdmin}
            pathSegment="audit-visible-env-vars"
            placeholder="e.g. RAILS_ENV"
            settingKey="audit_visible_env_vars"
            settings={appSettings}
            title="Environment Variables Shown in Audit Logs"
          />
        </div>
        <div className="grid grid-cols-2 gap-12">
          <JsonSettingCard
            app={app}
            description={
              <>
                The exact commands a CI/CD token can run during an approved deploy request, each
                with the env vars it gets: &quot;env&quot; lists the ones it needs besides the
                app&apos;s command env vars, &quot;all_env_vars&quot; gives it every env var, and
                &quot;write&quot; gives it the real values of the read-only env vars. An empty list
                allows none.
              </>
            }
            disabled={!isAdmin}
            label="JSON list of commands"
            pathSegment="approved-deploy-commands"
            placeholder='[{"command": "bundle exec rake db:migrate", "write": true}]'
            settingKey="approved_deploy_commands"
            settings={appSettings}
            title="Approved Deploy Commands"
          />
          <JsonSettingCard
            app={app}
            description={
              <>
                Per-service regex patterns for the Docker images in convox.yml, matched against the
                whole image reference. Required for builds made under a deploy approval: every
                service needs a pattern (its own or &quot;*&quot;) that names the image repository.{' '}
                {'{{GIT_COMMIT}}'} is replaced with the approved commit.
              </>
            }
            disabled={!isAdmin}
            label="JSON object (service name → regex)"
            pathSegment="service-image-patterns"
            placeholder='{"*": "docker\\.io/myorg/app:{{GIT_COMMIT}}-amd64"}'
            settingKey="service_image_patterns"
            settings={appSettings}
            title="Service Image Patterns"
          />
        </div>
        <div className="grid grid-cols-2 gap-12">
          <StringArrayCard
            app={app}
            description="The env vars every command run with exec or run gets. A command can ask for more with --env, or for every env var with --all-env-vars. An empty list gives every command every env var except RACK_URL."
            disabled={!isAdmin}
            pathSegment="command-env-vars"
            placeholder="e.g. PATH"
            settingKey="command_env_vars"
            settings={appSettings}
            title="Command Env Vars"
          />
          <JsonSettingCard
            app={app}
            description="Env vars a command gets with another env var's value, unless it's run with --write. For example, a command's DATABASE_URL can take the value of READ_ONLY_DATABASE_URL, a database user that can't change data."
            disabled={!isAdmin}
            label="JSON object (env var → env var it takes its value from)"
            pathSegment="command-env-read-only"
            placeholder='{"DATABASE_URL": "READ_ONLY_DATABASE_URL"}'
            settingKey="command_env_read_only"
            settings={appSettings}
            title="Read-Only Env Vars for Commands"
          />
        </div>
        <div className="grid grid-cols-2 gap-12">
          <CommandJustificationCard
            app={app}
            disabled={!isAdmin}
            gatewayWide={globalSettings?.require_command_justification}
            settings={appSettings}
          />
        </div>
      </div>
    </div>
  )
}
