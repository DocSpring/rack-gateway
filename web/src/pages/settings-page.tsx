import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'
import type { SettingsSetting } from '@/api/schemas'
import { getSettingValue, SourceIndicator } from '@/components/settings/source-indicator'
import { toast } from '@/components/ui/use-toast'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { useAuth } from '../contexts/auth-context'
import { useStepUp } from '../contexts/step-up-context'
import { api } from '../lib/api'
import { toastAPIError } from '../lib/error-utils'

type GlobalSettingsResponse = {
  [key: string]: SettingsSetting
}

function MfaConfigCard({
  settings,
  disabled,
}: {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [requireMfa, setRequireMfa] = useState<boolean | null>(null)
  const [trustedDeviceTtl, setTrustedDeviceTtl] = useState<number | null>(null)
  const [stepUpWindow, setStepUpWindow] = useState<number | null>(null)

  const currentRequireMfa = getSettingValue(settings?.mfa_require_all_users, true)
  const currentTrustedDeviceTtl = getSettingValue(settings?.mfa_trusted_device_ttl_days, 30)
  const currentStepUpWindow = getSettingValue(settings?.mfa_step_up_window_minutes, 10)

  const displayRequireMfa = requireMfa !== null ? requireMfa : currentRequireMfa
  const displayTrustedDeviceTtl =
    trustedDeviceTtl !== null ? trustedDeviceTtl : currentTrustedDeviceTtl
  const displayStepUpWindow = stepUpWindow !== null ? stepUpWindow : currentStepUpWindow

  const hasChanges = requireMfa !== null || trustedDeviceTtl !== null || stepUpWindow !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      const updates: Record<string, unknown> = {}
      if (requireMfa !== null) {
        updates.mfa_require_all_users = requireMfa
      }
      if (trustedDeviceTtl !== null) {
        updates.mfa_trusted_device_ttl_days = trustedDeviceTtl
      }
      if (stepUpWindow !== null) {
        updates.mfa_step_up_window_minutes = stepUpWindow
      }
      return await api.put<Record<string, SettingsSetting>>(
        '/api/v1/settings/mfa-configuration',
        updates
      )
    },
    onSuccess: (updatedSettings) => {
      // Merge updated settings into cache instead of refetching
      qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
        ...old,
        ...updatedSettings,
      }))
      setRequireMfa(null)
      setTrustedDeviceTtl(null)
      setStepUpWindow(null)
      toast.success('MFA settings updated')
    },
    onError: (err: unknown) => {
      toastAPIError(err, 'Failed to update MFA settings')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      const keys: string[] = []
      if (settings?.mfa_require_all_users?.source === 'db') {
        keys.push('mfa_require_all_users')
      }
      if (settings?.mfa_trusted_device_ttl_days?.source === 'db') {
        keys.push('mfa_trusted_device_ttl_days')
      }
      if (settings?.mfa_step_up_window_minutes?.source === 'db') {
        keys.push('mfa_step_up_window_minutes')
      }
      if (keys.length > 0) {
        const params = keys.map((k) => `key=${k}`).join('&')
        return await api.delete<Record<string, SettingsSetting>>(
          `/api/v1/settings/mfa-configuration?${params}`
        )
      }
    },
    onSuccess: (updatedSettings) => {
      // Merge updated settings into cache instead of refetching
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setRequireMfa(null)
      setTrustedDeviceTtl(null)
      setStepUpWindow(null)
      toast.success('MFA settings cleared')
    },
    onError: (err: unknown) => {
      toastAPIError(err, 'Failed to clear MFA settings')
    },
  })

  const handleCancel = () => {
    setRequireMfa(null)
    setTrustedDeviceTtl(null)
    setStepUpWindow(null)
  }

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync()
    } catch (err) {
      if (handleStepUpError(err, () => updateMutation.mutateAsync())) {
        return // MFA modal shown, will retry after verification
      }
      // Error already handled by mutation's onError
    }
  }

  const handleClear = async () => {
    try {
      await clearMutation.mutateAsync()
    } catch (err) {
      if (handleStepUpError(err, () => clearMutation.mutateAsync())) {
        return // MFA modal shown, will retry after verification
      }
      // Error already handled by mutation's onError
    }
  }

  const hasDbSettings =
    settings?.mfa_require_all_users?.source === 'db' ||
    settings?.mfa_trusted_device_ttl_days?.source === 'db' ||
    settings?.mfa_step_up_window_minutes?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>MFA Configuration</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <label className="flex items-center gap-3">
          <input
            checked={displayRequireMfa}
            disabled={disabled}
            onChange={(e) => setRequireMfa(e.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Require MFA for all users</span>
          <SourceIndicator setting={settings?.mfa_require_all_users} />
        </label>

        <div>
          <Label htmlFor="trusted-device-ttl">Trusted Device TTL (days)</Label>
          <div className="flex items-center gap-2">
            <Input
              className="w-24"
              disabled={disabled}
              id="trusted-device-ttl"
              min={1}
              onChange={(e) => {
                const val = Number.parseInt(e.target.value, 10)
                if (!Number.isNaN(val) && val >= 1) {
                  setTrustedDeviceTtl(val)
                }
              }}
              type="number"
              value={displayTrustedDeviceTtl}
            />
            <SourceIndicator setting={settings?.mfa_trusted_device_ttl_days} />
          </div>
          <p className="mt-1 text-muted-foreground text-xs">
            Number of days a trusted device remains valid
          </p>
        </div>

        <div>
          <Label htmlFor="step-up-window">Step-up Window (minutes)</Label>
          <div className="flex items-center gap-2">
            <Input
              className="w-24"
              disabled={disabled}
              id="step-up-window"
              min={1}
              onChange={(e) => {
                const val = Number.parseInt(e.target.value, 10)
                if (!Number.isNaN(val) && val >= 1) {
                  setStepUpWindow(val)
                }
              }}
              type="number"
              value={displayStepUpWindow}
            />
            <SourceIndicator setting={settings?.mfa_step_up_window_minutes} />
          </div>
          <p className="mt-1 text-muted-foreground text-xs">
            Duration of MFA step-up authentication window for sensitive operations
          </p>
        </div>

        <div className="flex justify-end gap-2">
          {hasDbSettings && (
            <Button
              disabled={disabled || clearMutation.isPending}
              onClick={handleClear}
              size="sm"
              variant="outline"
            >
              Clear
            </Button>
          )}
          {hasChanges && (
            <>
              <Button disabled={disabled} onClick={handleCancel} size="sm" variant="outline">
                Cancel
              </Button>
              <Button
                disabled={disabled || updateMutation.isPending}
                onClick={handleSave}
                size="sm"
              >
                Save
              </Button>
            </>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function DestructiveActionsCard({
  settings,
  disabled,
}: {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [allowDestructive, setAllowDestructive] = useState<boolean | null>(null)

  const currentValue = getSettingValue(settings?.allow_destructive_actions, false)
  const displayValue = allowDestructive !== null ? allowDestructive : currentValue
  const hasChanges = allowDestructive !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      if (allowDestructive !== null) {
        return await api.put<Record<string, SettingsSetting>>(
          '/api/v1/settings/allow-destructive-actions',
          {
            allow_destructive_actions: allowDestructive,
          }
        )
      }
    },
    onSuccess: (updatedSettings) => {
      // Merge updated settings into cache instead of refetching
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setAllowDestructive(null)
      toast.success('Setting updated')
    },
    onError: (err: unknown) => {
      toastAPIError(err, 'Failed to update setting')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () =>
      await api.delete<Record<string, SettingsSetting>>(
        '/api/v1/settings/allow-destructive-actions?key=allow_destructive_actions'
      ),
    onSuccess: (updatedSettings) => {
      // Merge updated settings into cache instead of refetching
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setAllowDestructive(null)
      toast.success('Setting cleared')
    },
    onError: (err: unknown) => {
      toastAPIError(err, 'Failed to clear setting')
    },
  })

  const handleCancel = () => {
    setAllowDestructive(null)
  }

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync()
    } catch (err) {
      if (handleStepUpError(err, () => updateMutation.mutateAsync())) {
        return // MFA modal shown, will retry after verification
      }
      // Error already handled by mutation's onError
    }
  }

  const handleClear = async () => {
    try {
      await clearMutation.mutateAsync()
    } catch (err) {
      if (handleStepUpError(err, () => clearMutation.mutateAsync())) {
        return // MFA modal shown, will retry after verification
      }
      // Error already handled by mutation's onError
    }
  }

  const hasDbSetting = settings?.allow_destructive_actions?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Allow Destructive Actions</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="mb-3 text-muted-foreground text-sm">
          When disabled, dangerous delete operations are banned globally (e.g. deleting apps).
          Enable to allow destructive actions (you will still need the required permissions).
        </p>
        <label className="flex items-center gap-3">
          <input
            checked={displayValue}
            disabled={disabled}
            onChange={(e) => setAllowDestructive(e.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Allow destructive actions</span>
          <SourceIndicator setting={settings?.allow_destructive_actions} />
        </label>

        <div className="mt-4 flex justify-end gap-2">
          {hasDbSetting && (
            <Button
              disabled={disabled || clearMutation.isPending}
              onClick={handleClear}
              size="sm"
              variant="outline"
            >
              Clear
            </Button>
          )}
          {hasChanges && (
            <>
              <Button disabled={disabled} onClick={handleCancel} size="sm" variant="outline">
                Cancel
              </Button>
              <Button
                disabled={disabled || updateMutation.isPending}
                onClick={handleSave}
                size="sm"
              >
                Save
              </Button>
            </>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function VCSCIProvidersCard({
  settings,
  disabled,
}: {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [vcsProvider, setVcsProvider] = useState<string | null>(null)
  const [vcsOrgName, setVcsOrgName] = useState<string | null>(null)
  const [ciProvider, setCiProvider] = useState<string | null>(null)
  const [ciOrgSlug, setCiOrgSlug] = useState<string | null>(null)

  const currentVcsProvider = getSettingValue(settings?.default_vcs_provider, 'github')
  const currentVcsOrgName = getSettingValue(settings?.default_vcs_org_name, '')
  const currentCiProvider = getSettingValue(settings?.default_ci_provider, 'circleci')
  const currentCiOrgSlug = getSettingValue(settings?.default_ci_org_slug, '')

  const displayVcsProvider = vcsProvider !== null ? vcsProvider : currentVcsProvider
  const displayVcsOrgName = vcsOrgName !== null ? vcsOrgName : currentVcsOrgName
  const displayCiProvider = ciProvider !== null ? ciProvider : currentCiProvider
  const displayCiOrgSlug = ciOrgSlug !== null ? ciOrgSlug : currentCiOrgSlug

  const hasChanges =
    vcsProvider !== null || vcsOrgName !== null || ciProvider !== null || ciOrgSlug !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      const updates: Record<string, unknown> = {}
      if (vcsProvider !== null) {
        updates.default_vcs_provider = vcsProvider
      }
      if (vcsOrgName !== null) {
        updates.default_vcs_org_name = vcsOrgName
      }
      if (ciProvider !== null) {
        updates.default_ci_provider = ciProvider
      }
      if (ciOrgSlug !== null) {
        updates.default_ci_org_slug = ciOrgSlug
      }
      return await api.put<Record<string, SettingsSetting>>(
        '/api/v1/settings/vcs-and-ci-defaults',
        updates
      )
    },
    onSuccess: (updatedSettings) => {
      // Merge updated settings into cache instead of refetching
      qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
        ...old,
        ...updatedSettings,
      }))
      setVcsProvider(null)
      setVcsOrgName(null)
      setCiProvider(null)
      setCiOrgSlug(null)
      toast.success('Provider settings updated')
    },
    onError: (err: unknown) => {
      toastAPIError(err, 'Failed to update settings')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      const keys: string[] = []
      if (settings?.default_vcs_provider?.source === 'db') {
        keys.push('default_vcs_provider')
      }
      if (settings?.default_vcs_org_name?.source === 'db') {
        keys.push('default_vcs_org_name')
      }
      if (settings?.default_ci_provider?.source === 'db') {
        keys.push('default_ci_provider')
      }
      if (settings?.default_ci_org_slug?.source === 'db') {
        keys.push('default_ci_org_slug')
      }
      if (keys.length > 0) {
        const params = keys.map((k) => `key=${k}`).join('&')
        return await api.delete<Record<string, SettingsSetting>>(
          `/api/v1/settings/vcs-and-ci-defaults?${params}`
        )
      }
    },
    onSuccess: (updatedSettings) => {
      // Merge updated settings into cache instead of refetching
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setVcsProvider(null)
      setVcsOrgName(null)
      setCiProvider(null)
      setCiOrgSlug(null)
      toast.success('Provider settings cleared')
    },
    onError: (err: unknown) => {
      toastAPIError(err, 'Failed to clear settings')
    },
  })

  const handleCancel = () => {
    setVcsProvider(null)
    setVcsOrgName(null)
    setCiProvider(null)
    setCiOrgSlug(null)
  }

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync()
    } catch (err) {
      if (handleStepUpError(err, () => updateMutation.mutateAsync())) {
        return // MFA modal shown, will retry after verification
      }
      // Error already handled by mutation's onError
    }
  }

  const handleClear = async () => {
    try {
      await clearMutation.mutateAsync()
    } catch (err) {
      if (handleStepUpError(err, () => clearMutation.mutateAsync())) {
        return // MFA modal shown, will retry after verification
      }
      // Error already handled by mutation's onError
    }
  }

  const hasDbSettings =
    settings?.default_vcs_provider?.source === 'db' ||
    settings?.default_vcs_org_name?.source === 'db' ||
    settings?.default_ci_provider?.source === 'db' ||
    settings?.default_ci_org_slug?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Default VCS & CI Providers</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 pb-6">
        <p className="text-muted-foreground text-sm">
          Configure default version control and CI/CD providers for all apps. Apps can override
          these in their individual settings.
        </p>

        <div className="grid grid-cols-2 gap-12">
          <div>
            <Label htmlFor="vcs-provider">VCS Provider</Label>
            <div className="flex items-center gap-2">
              <select
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                disabled={disabled}
                id="vcs-provider"
                onChange={(e) => setVcsProvider(e.target.value)}
                value={displayVcsProvider}
              >
                <option value="github">GitHub</option>
                <option disabled value="gitlab">
                  GitLab (coming soon)
                </option>
                <option disabled value="bitbucket">
                  Bitbucket (coming soon)
                </option>
              </select>
              <SourceIndicator setting={settings?.default_vcs_provider} />
            </div>
          </div>

          <div>
            <Label htmlFor="vcs-org-name">VCS Organization Name</Label>
            <div className="flex items-center gap-2">
              <Input
                disabled={disabled}
                id="vcs-org-name"
                onChange={(e) => setVcsOrgName(e.target.value)}
                placeholder="DocSpring"
                type="text"
                value={displayVcsOrgName}
              />
              <SourceIndicator setting={settings?.default_vcs_org_name} />
            </div>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-12">
          <div>
            <Label htmlFor="ci-provider">CI Provider</Label>
            <div className="flex items-center gap-2">
              <select
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                disabled={disabled}
                id="ci-provider"
                onChange={(e) => setCiProvider(e.target.value)}
                value={displayCiProvider}
              >
                <option value="circleci">CircleCI</option>
                <option disabled value="github_actions">
                  GitHub Actions (coming soon)
                </option>
                <option disabled value="gitlab_ci">
                  GitLab CI (coming soon)
                </option>
              </select>
              <SourceIndicator setting={settings?.default_ci_provider} />
            </div>
          </div>

          <div>
            <Label htmlFor="ci-org-slug">CI Organization Slug</Label>
            <div className="flex items-center gap-2">
              <Input
                disabled={disabled}
                id="ci-org-slug"
                onChange={(e) => setCiOrgSlug(e.target.value)}
                placeholder="gh/DocSpring"
                type="text"
                value={displayCiOrgSlug}
              />
              <SourceIndicator setting={settings?.default_ci_org_slug} />
            </div>
            <p className="mt-1 text-muted-foreground text-xs">
              For CircleCI: gh/YourOrg (GitHub), bb/YourOrg (Bitbucket)
            </p>
          </div>
        </div>

        <div className="flex justify-end gap-2">
          {hasDbSettings && (
            <Button
              disabled={disabled || clearMutation.isPending}
              onClick={handleClear}
              size="sm"
              variant="outline"
            >
              Clear
            </Button>
          )}
          {hasChanges && (
            <>
              <Button disabled={disabled} onClick={handleCancel} size="sm" variant="outline">
                Cancel
              </Button>
              <Button
                disabled={disabled || updateMutation.isPending}
                onClick={handleSave}
                size="sm"
              >
                Save
              </Button>
            </>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function DeployApprovalsCard({
  settings,
  disabled,
}: {
  settings: GlobalSettingsResponse | undefined
  disabled: boolean
}) {
  const qc = useQueryClient()
  const { handleStepUpError } = useStepUp()
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [windowMinutes, setWindowMinutes] = useState<number | null>(null)

  const currentEnabled = getSettingValue(settings?.deploy_approvals_enabled, true)
  const currentWindowMinutes = getSettingValue(settings?.deploy_approval_window_minutes, 15)

  const displayEnabled = enabled !== null ? enabled : currentEnabled
  const displayWindowMinutes = windowMinutes !== null ? windowMinutes : currentWindowMinutes

  const hasChanges = enabled !== null || windowMinutes !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      const updates: Record<string, unknown> = {}
      if (enabled !== null) {
        updates.deploy_approvals_enabled = enabled
      }
      if (windowMinutes !== null) {
        updates.deploy_approval_window_minutes = windowMinutes
      }
      return await api.put<Record<string, SettingsSetting>>(
        '/api/v1/settings/deploy-approvals',
        updates
      )
    },
    onSuccess: (updatedSettings) => {
      // Merge updated settings into cache instead of refetching
      qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
        ...old,
        ...updatedSettings,
      }))
      setEnabled(null)
      setWindowMinutes(null)
      toast.success('Deploy approval settings updated')
    },
    onError: (err: unknown) => {
      toastAPIError(err, 'Failed to update deploy approval settings')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      const keys: string[] = []
      if (settings?.deploy_approvals_enabled?.source === 'db') {
        keys.push('deploy_approvals_enabled')
      }
      if (settings?.deploy_approval_window_minutes?.source === 'db') {
        keys.push('deploy_approval_window_minutes')
      }
      if (keys.length > 0) {
        const params = keys.map((k) => `key=${k}`).join('&')
        return await api.delete<Record<string, SettingsSetting>>(
          `/api/v1/settings/deploy-approvals?${params}`
        )
      }
    },
    onSuccess: (updatedSettings) => {
      // Merge updated settings into cache instead of refetching
      if (updatedSettings) {
        qc.setQueryData(['globalSettings'], (old: GlobalSettingsResponse | undefined) => ({
          ...old,
          ...updatedSettings,
        }))
      }
      setEnabled(null)
      setWindowMinutes(null)
      toast.success('Deploy approval settings cleared')
    },
    onError: (err: unknown) => {
      toastAPIError(err, 'Failed to clear deploy approval settings')
    },
  })

  const handleCancel = () => {
    setEnabled(null)
    setWindowMinutes(null)
  }

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync()
    } catch (err) {
      if (handleStepUpError(err, () => updateMutation.mutateAsync())) {
        return // MFA modal shown, will retry after verification
      }
      // Error already handled by mutation's onError
    }
  }

  const handleClear = async () => {
    try {
      await clearMutation.mutateAsync()
    } catch (err) {
      if (handleStepUpError(err, () => clearMutation.mutateAsync())) {
        return // MFA modal shown, will retry after verification
      }
      // Error already handled by mutation's onError
    }
  }

  const hasDbSettings =
    settings?.deploy_approvals_enabled?.source === 'db' ||
    settings?.deploy_approval_window_minutes?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Deploy Approvals</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <p className="text-muted-foreground text-sm">
          Configure manual deploy approval workflow for CI/CD pipelines. When enabled, API tokens
          with deploy_with_approval permission require admin approval before deploying.
        </p>

        <label className="flex items-center gap-3">
          <input
            checked={displayEnabled}
            disabled={disabled}
            onChange={(e) => setEnabled(e.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Enable deploy approvals</span>
          <SourceIndicator setting={settings?.deploy_approvals_enabled} />
        </label>

        <div>
          <Label htmlFor="approval-window">Approval Window (minutes)</Label>
          <div className="flex items-center gap-2">
            <Input
              className="w-24"
              disabled={disabled}
              id="approval-window"
              min={1}
              onChange={(e) => {
                const val = Number.parseInt(e.target.value, 10)
                if (!Number.isNaN(val) && val >= 1) {
                  setWindowMinutes(val)
                }
              }}
              type="number"
              value={displayWindowMinutes}
            />
            <SourceIndicator setting={settings?.deploy_approval_window_minutes} />
          </div>
          <p className="mt-1 text-muted-foreground text-xs">
            How long approvals remain valid after admin approval
          </p>
        </div>

        <div className="flex justify-end gap-2">
          {hasDbSettings && (
            <Button
              disabled={disabled || clearMutation.isPending}
              onClick={handleClear}
              size="sm"
              variant="outline"
            >
              Clear
            </Button>
          )}
          {hasChanges && (
            <>
              <Button disabled={disabled} onClick={handleCancel} size="sm" variant="outline">
                Cancel
              </Button>
              <Button
                disabled={disabled || updateMutation.isPending}
                onClick={handleSave}
                size="sm"
              >
                Save
              </Button>
            </>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

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

  if (isLoading) {
    return (
      <div className="p-8">
        <div className="mb-8">
          <h1 className="font-bold text-3xl">Settings</h1>
          <p className="mt-2 text-muted-foreground">
            Configure gateway-wide behavior and safety controls
          </p>
        </div>
        <div className="flex h-64 items-center justify-center">
          <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </div>
    )
  }

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">Settings</h1>
        <p className="mt-2 text-muted-foreground">
          Configure gateway-wide behavior and safety controls
        </p>
      </div>

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
        <VCSCIProvidersCard disabled={!isAdmin} settings={globalSettings} />
        <DeployApprovalsCard disabled={!isAdmin} settings={globalSettings} />
      </div>
    </div>
  )
}
