import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { getSettingValue, SourceIndicator } from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'

import type { AppSettingsResponse } from '@/pages/app-settings/types'
import { extractErrorMessage } from '@/pages/app-settings/utils'

type IntegrationAvailability = {
  github: boolean
  circleci: boolean
}

type VCSCIProvidersCardProps = {
  app: string
  settings: AppSettingsResponse | undefined
  disabled: boolean
  integrations?: IntegrationAvailability
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Multiple settings require individual checks
export function VCSCIProvidersCard({
  app,
  settings,
  disabled,
  integrations,
}: VCSCIProvidersCardProps) {
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
      const payload: Record<string, unknown> = {}
      if (vcsProvider !== null) {
        payload.vcs_provider = vcsProvider || null
      }
      if (vcsRepo !== null) {
        payload.vcs_repo = vcsRepo || null
      }
      if (ciProvider !== null) {
        payload.ci_provider = ciProvider || null
      }
      if (ciOrgSlug !== null) {
        payload.ci_org_slug = ciOrgSlug || null
      }
      if (githubVerification !== null) {
        payload.github_verification = githubVerification
      }
      if (allowDeployFromDefaultBranch !== null) {
        payload.allow_deploy_from_default_branch = allowDeployFromDefaultBranch
      }
      if (requirePRForBranch !== null) {
        payload.require_pr_for_branch = requirePRForBranch
      }
      if (defaultBranch !== null) {
        payload.default_branch = defaultBranch
      }
      if (verifyGitCommitMode !== null) {
        payload.verify_git_commit_mode = verifyGitCommitMode
      }
      if (Object.keys(payload).length === 0) {
        return
      }
      await api.put(`/api/v1/apps/${app}/settings/vcs-ci-deploy`, payload)
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
    onError: (error: unknown) => {
      const message = extractErrorMessage(error)
      toast.error(message ?? 'Failed to update settings')
    },
  })

  const clearMutation = useMutation({
    mutationFn: async () => {
      await api.delete(`/api/v1/apps/${app}/settings/vcs-ci-deploy`)
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
    onError: (error: unknown) => {
      const message = extractErrorMessage(error)
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
        <CardTitle>Version Control &amp; CI/CD Settings</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <div>
          <p className="text-muted-foreground text-sm">
            Configure repository settings and CI providers used for deploy approvals.
          </p>
        </div>

        <div className="grid gap-6 lg:grid-cols-2">
          <div className="space-y-4">
            <div className={disabled ? 'opacity-50' : ''}>
              <Label htmlFor="vcs-provider">VCS Provider</Label>
              <div className="flex items-center gap-2">
                <select
                  className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                  disabled={disabled}
                  id="vcs-provider"
                  onChange={(event) => setVcsProvider(event.target.value || null)}
                  value={displayVcsProvider}
                >
                  <option value="">Not set</option>
                  <option value="github">GitHub</option>
                </select>
                <SourceIndicator setting={settings?.vcs_provider} />
              </div>
            </div>

            <div className={disabled || !githubAvailable ? 'opacity-50' : ''}>
              <Label htmlFor="vcs-repo">GitHub Repo</Label>
              <div className="flex items-center gap-2">
                <Input
                  disabled={disabled || !githubAvailable}
                  id="vcs-repo"
                  onChange={(event) => setVcsRepo(event.target.value)}
                  placeholder="org/repo"
                  value={displayVcsRepo}
                />
                <SourceIndicator setting={settings?.vcs_repo} />
              </div>
              <p className="mt-1 text-muted-foreground text-xs">
                GitHub repository in <code>org/repo</code> format
              </p>
            </div>

            <div>
              <Label htmlFor="ci-provider">CI Provider</Label>
              <div className="flex items-center gap-2">
                <select
                  className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                  disabled={disabled}
                  id="ci-provider"
                  onChange={(event) => setCiProvider(event.target.value || null)}
                  value={displayCiProvider}
                >
                  <option value="">Not set</option>
                  <option disabled={!circleciAvailable} value="circleci">
                    CircleCI
                  </option>
                  <option disabled={!githubAvailable} value="github">
                    GitHub Actions
                  </option>
                </select>
                <SourceIndicator setting={settings?.ci_provider} />
              </div>
            </div>

            <div className={disabled ? 'opacity-50' : ''}>
              <Label htmlFor="ci-org-slug">CircleCI Org Slug</Label>
              <div className="flex items-center gap-2">
                <Input
                  disabled={disabled || !circleciAvailable}
                  id="ci-org-slug"
                  onChange={(event) => setCiOrgSlug(event.target.value)}
                  placeholder="gh/org-name"
                  value={displayCiOrgSlug}
                />
                <SourceIndicator setting={settings?.ci_org_slug} />
              </div>
              <p className="mt-1 text-muted-foreground text-xs">
                Required for CircleCI integration. Format: <code>gh/org-name</code>
              </p>
            </div>
          </div>

          <div className="space-y-4">
            <div className="space-y-4">
              <label
                className={`flex items-center gap-3 ${disabled || !githubAvailable ? 'cursor-not-allowed opacity-50' : ''}`}
              >
                <input
                  checked={displayGithubVerification}
                  disabled={disabled || !githubAvailable}
                  onChange={(event) => setGithubVerification(event.target.checked)}
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
                  onChange={(event) => setAllowDeployFromDefaultBranch(event.target.checked)}
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
                  onChange={(event) => setRequirePRForBranch(event.target.checked)}
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
                    onChange={(event) => setDefaultBranch(event.target.value)}
                    placeholder="main"
                    type="text"
                    value={displayDefaultBranch}
                  />
                  <SourceIndicator setting={settings?.default_branch} />
                </div>
                <p className="mt-1 text-muted-foreground text-xs">
                  The default branch name for the app&apos;s repository
                </p>
              </div>

              <div className={disabled || !githubAvailable ? 'opacity-50' : ''}>
                <Label htmlFor="verify-git-commit-mode">Git Commit Verification Mode</Label>
                <div className="flex items-center gap-2">
                  <select
                    className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
                    disabled={disabled || !githubAvailable}
                    id="verify-git-commit-mode"
                    onChange={(event) => setVerifyGitCommitMode(event.target.value)}
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
