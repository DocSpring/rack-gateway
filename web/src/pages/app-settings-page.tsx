import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { RefreshCw } from 'lucide-react'

import { useAuth } from '@/contexts/auth-context'
import { api } from '@/lib/api'
import { apiPath } from '@/lib/api-path'
import { CommandJustificationCard } from '@/pages/app-settings/command-justification-card'
import { ServiceImagePatternsCard } from '@/pages/app-settings/service-image-patterns-card'
import { StringArrayCard } from '@/pages/app-settings/string-array-card'
import type { AppSettingsResponse } from '@/pages/app-settings/types'
import { VCSCIProvidersCard } from '@/pages/app-settings/vcs-ci-providers-card'

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
          <StringArrayCard
            app={app}
            description="The exact commands a CI/CD token can run during an approved deploy request. An empty list allows none."
            disabled={!isAdmin}
            pathSegment="approved-deploy-commands"
            placeholder="e.g. bundle exec rake db:migrate"
            settingKey="approved_deploy_commands"
            settings={appSettings}
            title="Approved Deploy Commands"
          />
          <ServiceImagePatternsCard app={app} disabled={!isAdmin} settings={appSettings} />
        </div>
        <div className="grid grid-cols-2 gap-12">
          <CommandJustificationCard app={app} disabled={!isAdmin} settings={appSettings} />
        </div>
      </div>
    </div>
  )
}
