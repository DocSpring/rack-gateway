import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { isAxiosError } from 'axios'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'
import type { SettingsSetting } from '@/api/schemas'
import { getSettingValue, SourceIndicator } from '@/components/settings/source-indicator'
import { StringArrayInput } from '@/components/settings/string-array-input'
import { toast } from '@/components/ui/use-toast'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { useAuth } from '../contexts/auth-context'
import { api } from '../lib/api'

type SettingsErrorPayload = {
  error?: string
}

type AppSettingsResponse = {
  [key: string]: SettingsSetting
}

function extractErrorMessage(error: unknown): string | undefined {
  if (isAxiosError<SettingsErrorPayload>(error)) {
    const payload = error.response?.data
    if (typeof payload === 'string') {
      return payload
    }
    if (payload && typeof payload.error === 'string') {
      return payload.error
    }
  }
  if (error instanceof Error) {
    return error.message
  }
  return
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Multiple settings require individual checks
function VCSCIProvidersCard({
  app,
  settings,
  disabled,
  integrations,
}: {
  app: string
  settings: AppSettingsResponse | undefined
  disabled: boolean
  integrations?: { github: boolean; circleci: boolean }
}) {
  const qc = useQueryClient()

  const githubAvailable = integrations?.github ?? false
  const circleciAvailable = integrations?.circleci ?? false
  const [vcsProvider, setVcsProvider] = useState<string | null>(null)
  const [vcsRepo, setVcsRepo] = useState<string | null>(null)
  const [ciProvider, setCiProvider] = useState<string | null>(null)
  const [ciOrgSlug, setCiOrgSlug] = useState<string | null>(null)
  const [githubVerification, setGithubVerification] = useState<boolean | null>(null)
  const [allowDeployFromDefaultBranch, setAllowDeployFromDefaultBranch] = useState<boolean | null>(
    null
  )
  const [requirePRForBranch, setRequirePRForBranch] = useState<boolean | null>(null)
  const [defaultBranch, setDefaultBranch] = useState<string | null>(null)
  const [verifyGitCommitMode, setVerifyGitCommitMode] = useState<string | null>(null)

  const currentVcsProvider = getSettingValue(settings?.vcs_provider, '')
  const currentVcsRepo = getSettingValue(settings?.vcs_repo, '')
  const currentCiProvider = getSettingValue(settings?.ci_provider, '')
  const currentCiOrgSlug = getSettingValue(settings?.ci_org_slug, '')
  const currentGithubVerification = getSettingValue(settings?.github_verification, true)
  const currentAllowDeployFromDefaultBranch = getSettingValue(
    settings?.allow_deploy_from_default_branch,
    false
  )
  const currentRequirePRForBranch = getSettingValue(settings?.require_pr_for_branch, true)
  const currentDefaultBranch = getSettingValue(settings?.default_branch, 'main')
  const currentVerifyGitCommitMode = getSettingValue(settings?.verify_git_commit_mode, 'latest')

  const displayVcsProvider = vcsProvider !== null ? vcsProvider : currentVcsProvider
  const displayVcsRepo = vcsRepo !== null ? vcsRepo : currentVcsRepo
  const displayCiProvider = ciProvider !== null ? ciProvider : currentCiProvider
  const displayCiOrgSlug = ciOrgSlug !== null ? ciOrgSlug : currentCiOrgSlug
  const displayGithubVerification =
    githubVerification !== null ? githubVerification : currentGithubVerification
  const displayAllowDeployFromDefaultBranch =
    allowDeployFromDefaultBranch !== null
      ? allowDeployFromDefaultBranch
      : currentAllowDeployFromDefaultBranch
  const displayRequirePRForBranch =
    requirePRForBranch !== null ? requirePRForBranch : currentRequirePRForBranch
  const displayDefaultBranch = defaultBranch !== null ? defaultBranch : currentDefaultBranch
  const displayVerifyGitCommitMode =
    verifyGitCommitMode !== null ? verifyGitCommitMode : currentVerifyGitCommitMode

  const hasChanges =
    vcsProvider !== null ||
    vcsRepo !== null ||
    ciProvider !== null ||
    ciOrgSlug !== null ||
    githubVerification !== null ||
    allowDeployFromDefaultBranch !== null ||
    requirePRForBranch !== null ||
    defaultBranch !== null ||
    verifyGitCommitMode !== null

  const updateMutation = useMutation({
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Multiple settings require individual checks
    mutationFn: async () => {
      const updates: Promise<unknown>[] = []
      if (vcsProvider !== null) {
        updates.push(
          api.put(`/.gateway/api/apps/${app}/settings/vcs_provider`, vcsProvider || null)
        )
      }
      if (vcsRepo !== null) {
        updates.push(api.put(`/.gateway/api/apps/${app}/settings/vcs_repo`, vcsRepo || null))
      }
      if (ciProvider !== null) {
        updates.push(api.put(`/.gateway/api/apps/${app}/settings/ci_provider`, ciProvider || null))
      }
      if (ciOrgSlug !== null) {
        updates.push(api.put(`/.gateway/api/apps/${app}/settings/ci_org_slug`, ciOrgSlug || null))
      }
      if (githubVerification !== null) {
        updates.push(
          api.put(`/.gateway/api/apps/${app}/settings/github_verification`, githubVerification)
        )
      }
      if (allowDeployFromDefaultBranch !== null) {
        updates.push(
          api.put(
            `/.gateway/api/apps/${app}/settings/allow_deploy_from_default_branch`,
            allowDeployFromDefaultBranch
          )
        )
      }
      if (requirePRForBranch !== null) {
        updates.push(
          api.put(`/.gateway/api/apps/${app}/settings/require_pr_for_branch`, requirePRForBranch)
        )
      }
      if (defaultBranch !== null) {
        updates.push(api.put(`/.gateway/api/apps/${app}/settings/default_branch`, defaultBranch))
      }
      if (verifyGitCommitMode !== null) {
        updates.push(
          api.put(`/.gateway/api/apps/${app}/settings/verify_git_commit_mode`, verifyGitCommitMode)
        )
      }
      await Promise.all(updates)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appSettings', app] })
      setVcsProvider(null)
      setVcsRepo(null)
      setCiProvider(null)
      setCiOrgSlug(null)
      setGithubVerification(null)
      setAllowDeployFromDefaultBranch(null)
      setRequirePRForBranch(null)
      setDefaultBranch(null)
      setVerifyGitCommitMode(null)
      toast.success('Deploy settings updated')
    },
    onError: (err: unknown) => {
      const message = extractErrorMessage(err)
      toast.error(message ?? 'Failed to update settings')
    },
  })

  const clearMutation = useMutation({
    // biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Multiple settings require individual checks
    mutationFn: async () => {
      const updates: Promise<unknown>[] = []
      if (settings?.vcs_provider?.source === 'db') {
        updates.push(api.delete(`/.gateway/api/apps/${app}/settings/vcs_provider`))
      }
      if (settings?.vcs_repo?.source === 'db') {
        updates.push(api.delete(`/.gateway/api/apps/${app}/settings/vcs_repo`))
      }
      if (settings?.ci_provider?.source === 'db') {
        updates.push(api.delete(`/.gateway/api/apps/${app}/settings/ci_provider`))
      }
      if (settings?.ci_org_slug?.source === 'db') {
        updates.push(api.delete(`/.gateway/api/apps/${app}/settings/ci_org_slug`))
      }
      if (settings?.github_verification?.source === 'db') {
        updates.push(api.delete(`/.gateway/api/apps/${app}/settings/github_verification`))
      }
      if (settings?.allow_deploy_from_default_branch?.source === 'db') {
        updates.push(
          api.delete(`/.gateway/api/apps/${app}/settings/allow_deploy_from_default_branch`)
        )
      }
      if (settings?.require_pr_for_branch?.source === 'db') {
        updates.push(api.delete(`/.gateway/api/apps/${app}/settings/require_pr_for_branch`))
      }
      if (settings?.default_branch?.source === 'db') {
        updates.push(api.delete(`/.gateway/api/apps/${app}/settings/default_branch`))
      }
      if (settings?.verify_git_commit_mode?.source === 'db') {
        updates.push(api.delete(`/.gateway/api/apps/${app}/settings/verify_git_commit_mode`))
      }
      await Promise.all(updates)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appSettings', app] })
      setVcsProvider(null)
      setVcsRepo(null)
      setCiProvider(null)
      setCiOrgSlug(null)
      setGithubVerification(null)
      setAllowDeployFromDefaultBranch(null)
      setRequirePRForBranch(null)
      setDefaultBranch(null)
      setVerifyGitCommitMode(null)
      toast.success('Deploy settings cleared')
    },
    onError: (err: unknown) => {
      const message = extractErrorMessage(err)
      toast.error(message ?? 'Failed to clear settings')
    },
  })

  const handleCancel = () => {
    setVcsProvider(null)
    setVcsRepo(null)
    setCiProvider(null)
    setCiOrgSlug(null)
    setGithubVerification(null)
    setAllowDeployFromDefaultBranch(null)
    setRequirePRForBranch(null)
    setDefaultBranch(null)
    setVerifyGitCommitMode(null)
  }

  const handleSave = () => {
    updateMutation.mutate()
  }

  const handleClear = () => {
    clearMutation.mutate()
  }

  const hasDbSettings =
    settings?.vcs_provider?.source === 'db' ||
    settings?.vcs_repo?.source === 'db' ||
    settings?.ci_provider?.source === 'db' ||
    settings?.ci_org_slug?.source === 'db' ||
    settings?.github_verification?.source === 'db' ||
    settings?.allow_deploy_from_default_branch?.source === 'db' ||
    settings?.require_pr_for_branch?.source === 'db' ||
    settings?.default_branch?.source === 'db' ||
    settings?.verify_git_commit_mode?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>VCS, CI & Deploy Settings</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        {/* VCS & CI Configuration */}
        <div className="grid grid-cols-2 gap-12">
          <div>
            <Label htmlFor="vcs-provider">VCS Provider</Label>
            <div className="flex items-center gap-2">
              <select
                className="h-9 flex-1 rounded-md border border-input bg-background px-3 text-sm"
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
              <SourceIndicator setting={settings?.vcs_provider} />
            </div>
          </div>

          <div>
            <Label htmlFor="vcs-repo">VCS Repository</Label>
            <div className="flex items-center gap-2">
              <Input
                className="flex-1"
                disabled={disabled}
                id="vcs-repo"
                onChange={(e) => setVcsRepo(e.target.value)}
                placeholder="docspring"
                type="text"
                value={displayVcsRepo ?? ''}
              />
              <SourceIndicator setting={settings?.vcs_repo} />
            </div>
            <p className="mt-1 text-muted-foreground text-xs">
              Just the repo name (not the full URL)
            </p>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-12">
          <div className={disabled || !circleciAvailable ? 'opacity-50' : ''}>
            <Label htmlFor="ci-provider">CI Provider</Label>
            <div className="flex items-center gap-2">
              <select
                className="h-9 flex-1 rounded-md border border-input bg-background px-3 text-sm"
                disabled={disabled || !circleciAvailable}
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
              <SourceIndicator setting={settings?.ci_provider} />
            </div>
          </div>

          <div className={disabled || !circleciAvailable ? 'opacity-50' : ''}>
            <Label htmlFor="ci-org-slug">CI Organization Slug</Label>
            <div className="flex items-center gap-2">
              <Input
                className="flex-1"
                disabled={disabled || !circleciAvailable}
                id="ci-org-slug"
                onChange={(e) => setCiOrgSlug(e.target.value)}
                placeholder="gh/DocSpring"
                type="text"
                value={displayCiOrgSlug}
              />
              <SourceIndicator setting={settings?.ci_org_slug} />
            </div>
            <p className="mt-1 text-muted-foreground text-xs">Leave empty to use global default</p>
          </div>
        </div>

        {/* GitHub Verification Settings */}
        <div>
          <h3 className="mb-4 font-semibold text-sm">GitHub Verification</h3>
          <div className="grid grid-cols-2 gap-12">
            <div className="space-y-4">
              <label
                className={`flex items-center gap-3 ${disabled || !githubAvailable ? 'cursor-not-allowed opacity-50' : ''}`}
              >
                <input
                  checked={displayGithubVerification}
                  disabled={disabled || !githubAvailable}
                  onChange={(e) => setGithubVerification(e.target.checked)}
                  type="checkbox"
                />
                <span className="font-medium text-sm">Enable GitHub verification</span>
                <SourceIndicator setting={settings?.github_verification} />
              </label>
              <p className="text-muted-foreground text-xs">
                Verify git commits against GitHub when creating deploy approval requests.
              </p>

              <label
                className={`flex items-center gap-3 ${disabled || !githubAvailable ? 'cursor-not-allowed opacity-50' : ''}`}
              >
                <input
                  checked={displayAllowDeployFromDefaultBranch}
                  disabled={disabled || !githubAvailable}
                  onChange={(e) => setAllowDeployFromDefaultBranch(e.target.checked)}
                  type="checkbox"
                />
                <span className="font-medium text-sm">Allow deploy from default branch</span>
                <SourceIndicator setting={settings?.allow_deploy_from_default_branch} />
              </label>
              <p className="text-muted-foreground text-xs">
                When disabled, deployments must be from a non-default branch.
              </p>

              <label
                className={`flex items-center gap-3 ${disabled || !githubAvailable ? 'cursor-not-allowed opacity-50' : ''}`}
              >
                <input
                  checked={displayRequirePRForBranch}
                  disabled={disabled || !githubAvailable}
                  onChange={(e) => setRequirePRForBranch(e.target.checked)}
                  type="checkbox"
                />
                <span className="font-medium text-sm">Require PR for branch</span>
                <SourceIndicator setting={settings?.require_pr_for_branch} />
              </label>
              <p className="text-muted-foreground text-xs">
                Require a GitHub pull request to exist for the branch being deployed.
              </p>
            </div>

            <div className="space-y-4">
              <div className={disabled || !githubAvailable ? 'opacity-50' : ''}>
                <Label htmlFor="default-branch">Default Branch</Label>
                <div className="flex items-center gap-2">
                  <Input
                    disabled={disabled || !githubAvailable}
                    id="default-branch"
                    onChange={(e) => setDefaultBranch(e.target.value)}
                    placeholder="main"
                    type="text"
                    value={displayDefaultBranch}
                  />
                  <SourceIndicator setting={settings?.default_branch} />
                </div>
                <p className="mt-1 text-muted-foreground text-xs">
                  The default branch name for the app's repository
                </p>
              </div>

              <div className={disabled || !githubAvailable ? 'opacity-50' : ''}>
                <Label htmlFor="verify-git-commit-mode">Git Commit Verification Mode</Label>
                <div className="flex items-center gap-2">
                  <select
                    className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                    disabled={disabled || !githubAvailable}
                    id="verify-git-commit-mode"
                    onChange={(e) => setVerifyGitCommitMode(e.target.value)}
                    value={displayVerifyGitCommitMode}
                  >
                    <option value="branch">branch (commit must exist on branch)</option>
                    <option value="latest">latest (commit must be latest on branch)</option>
                  </select>
                  <SourceIndicator setting={settings?.verify_git_commit_mode} />
                </div>
                <p className="mt-1 text-muted-foreground text-xs">
                  How strictly to verify git commits when deploying
                </p>
              </div>
            </div>
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

function StringArrayCard({
  app,
  settings,
  disabled,
  settingKey,
  title,
  description,
  placeholder,
}: {
  app: string
  settings: AppSettingsResponse | undefined
  disabled: boolean
  settingKey: string
  title: string
  description: string
  placeholder?: string
}) {
  const qc = useQueryClient()
  const setting = settings?.[settingKey]
  const currentValue = getSettingValue<string[] | null>(setting, null) ?? []

  const [items, setItems] = useState<string[]>(currentValue)

  // Check if items differ from current value
  const hasChanges =
    items.length !== currentValue.length ||
    items.some((item, i) => item.trim() !== currentValue[i]?.trim())

  const updateMutation = useMutation({
    mutationFn: async () => {
      // Filter out empty strings
      const filtered = items.map((s) => s.trim()).filter((s) => s.length > 0)
      await api.put(`/.gateway/api/apps/${app}/settings/${settingKey}`, filtered)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appSettings', app] })
      toast.success('Setting updated')
    },
    onError: (err: unknown) => {
      const message = extractErrorMessage(err)
      toast.error(message ?? 'Failed to update setting')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      await api.delete(`/.gateway/api/apps/${app}/settings/${settingKey}`)
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['appSettings', app] })
      // Reset to empty array after clearing
      setItems([])
      toast.success('Setting cleared')
    },
    onError: (err: unknown) => {
      const message = extractErrorMessage(err)
      toast.error(message ?? 'Failed to clear setting')
    },
  })

  const handleCancel = () => {
    setItems(currentValue)
  }

  const handleSave = () => {
    updateMutation.mutate()
  }

  const handleClear = () => {
    clearMutation.mutate()
  }

  const hasDbSetting = setting?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 pb-6">
        <p className="text-muted-foreground text-sm">{description}</p>

        <div className="space-y-2">
          <StringArrayInput
            disabled={disabled}
            onChange={setItems}
            placeholder={placeholder ?? 'Enter value'}
            value={items}
          />
          <SourceIndicator setting={setting} />
        </div>

        <div className="flex justify-end gap-2">
          {hasDbSetting && !hasChanges && (
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

function ServiceImagePatternsCard({
  app,
  settings,
  disabled,
}: {
  app: string
  settings: AppSettingsResponse | undefined
  disabled: boolean
}) {
  const qc = useQueryClient()
  const [value, setValue] = useState<string | null>(null)

  const setting = settings?.service_image_patterns
  const currentValue = getSettingValue<Record<string, string> | null>(setting, null)

  let displayValue = ''
  if (value !== null) {
    displayValue = value
  } else if (currentValue) {
    displayValue = JSON.stringify(currentValue, null, 2)
  }

  const hasChanges = value !== null

  const updateMutation = useMutation({
    mutationFn: async () => {
      if (value === null) return
      try {
        const parsed = JSON.parse(value)
        await api.put(`/.gateway/api/apps/${app}/settings/service_image_patterns`, parsed)
      } catch (_err) {
        throw new Error('Invalid JSON format')
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appSettings', app] })
      setValue(null)
      toast.success('Setting updated')
    },
    onError: (err: unknown) => {
      const message = extractErrorMessage(err)
      toast.error(message ?? 'Failed to update setting')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      await api.delete(`/.gateway/api/apps/${app}/settings/service_image_patterns`)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appSettings', app] })
      setValue(null)
      toast.success('Setting cleared')
    },
    onError: (err: unknown) => {
      const message = extractErrorMessage(err)
      toast.error(message ?? 'Failed to clear setting')
    },
  })

  const handleCancel = () => {
    setValue(null)
  }

  const handleSave = () => {
    updateMutation.mutate()
  }

  const handleClear = () => {
    clearMutation.mutate()
  }

  const hasDbSetting = setting?.source === 'db'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Service Image Patterns</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 pb-6">
        <p className="text-muted-foreground text-sm">
          Per-service regex patterns for validating Docker images in convox.yml. Validates build
          commands to ensure only images matching the pattern are allowed.
        </p>
        <div>
          <Label htmlFor="service-image-patterns">JSON object (service name → regex)</Label>
          <div className="flex flex-col gap-2">
            <textarea
              className="min-h-32 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
              disabled={disabled}
              id="service-image-patterns"
              onChange={(e) => setValue(e.target.value)}
              placeholder='{"web": "^ghcr.io/myorg/myapp:{{GIT_COMMIT}}$", "worker": "^.*$"}'
              value={displayValue}
            />
            <SourceIndicator setting={setting} />
          </div>
        </div>

        <div className="flex justify-end gap-2">
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

export function AppSettingsPage() {
  const { app } = useParams({ from: '/apps/$app/settings' }) as { app: string }
  const { user } = useAuth()
  const isAdmin = !!user?.roles?.includes('admin')

  const {
    data: appSettings,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['appSettings', app],
    queryFn: async () => api.get<AppSettingsResponse>(`/.gateway/api/apps/${app}/settings`),
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

      <div className="grid gap-6">
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
            placeholder="e.g. DATABASE_URL"
            settingKey="protected_env_vars"
            settings={appSettings}
            title="Protected Environment Variables"
          />
          <StringArrayCard
            app={app}
            description="Environment variables that are treated as secrets (values masked) but can still be changed."
            disabled={!isAdmin}
            placeholder="e.g. API_KEY"
            settingKey="secret_env_vars"
            settings={appSettings}
            title="Secret Environment Variables"
          />
        </div>
        <div className="grid grid-cols-2 gap-12">
          <StringArrayCard
            app={app}
            description="Commands that a CI/CD token can run during an approved deploy request."
            disabled={!isAdmin}
            placeholder="e.g. bundle exec rake db:migrate"
            settingKey="approved_deploy_commands"
            settings={appSettings}
            title="Approved Deploy Commands"
          />
          <ServiceImagePatternsCard app={app} disabled={!isAdmin} settings={appSettings} />
        </div>
      </div>
    </div>
  )
}
