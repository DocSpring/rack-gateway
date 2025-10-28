import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'

import { useAuth } from '@/contexts/auth-context'
import { api } from '@/lib/api'
import { DeployApprovalsCard } from '@/pages/settings/deploy-approvals-card'
import { DestructiveActionsCard } from '@/pages/settings/destructive-actions-card'
import { MfaConfigCard } from '@/pages/settings/mfa-config-card'
import type { GlobalSettingsResponse } from '@/pages/settings/types'
import { VcsCiCard } from '@/pages/settings/vcs-ci-card'

export function SettingsPage() {
  const { user } = useAuth()
  const isAdmin = !!user?.roles?.includes('admin')

  const {
    data: globalSettings,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['globalSettings'],
    queryFn: async () => api.get<GlobalSettingsResponse>('/api/v1/settings'),
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const heading = (
    <div className="mb-8">
      <h1 className="font-bold text-3xl">Settings</h1>
      <p className="mt-2 text-muted-foreground">
        Configure gateway-wide behavior and safety controls
      </p>
    </div>
  )

  if (isLoading) {
    return (
      <div className="p-8">
        {heading}
        <div className="flex h-64 items-center justify-center">
          <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </div>
    )
  }

  return (
    <div className="p-8">
      {heading}
      {error ? (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-destructive text-sm">
          Failed to load settings
        </div>
      ) : null}

      <div className="space-y-6">
        <div className="grid gap-6 md:grid-cols-2">
          <MfaConfigCard disabled={!isAdmin} settings={globalSettings} />
          <DestructiveActionsCard disabled={!isAdmin} settings={globalSettings} />
        </div>
        <VcsCiCard disabled={!isAdmin} settings={globalSettings} />
        <DeployApprovalsCard disabled={!isAdmin} settings={globalSettings} />
      </div>
    </div>
  )
}
