import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo, useState } from 'react'

import { getSettingValue } from '@/components/settings/source-indicator'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'

import type { AppSettingsResponse } from '@/pages/app-settings/types'
import { extractErrorMessage } from '@/pages/app-settings/utils'
import { VcsProviderFields } from '@/pages/app-settings/vcs-provider-fields'
import { VerificationSettingsFields } from '@/pages/app-settings/verification-settings-fields'

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

type StringOverrideKey =
  | 'vcs_provider'
  | 'vcs_repo'
  | 'ci_provider'
  | 'ci_org_slug'
  | 'default_branch'
  | 'verify_git_commit_mode'

type BooleanOverrideKey =
  | 'github_verification'
  | 'allow_deploy_from_default_branch'
  | 'require_pr_for_branch'

type OverrideState = Record<StringOverrideKey, string | null> &
  Record<BooleanOverrideKey, boolean | null>

const STRING_OVERRIDE_KEYS: readonly StringOverrideKey[] = [
  'vcs_provider',
  'vcs_repo',
  'ci_provider',
  'ci_org_slug',
  'default_branch',
  'verify_git_commit_mode',
]

const BOOLEAN_OVERRIDE_KEYS: readonly BooleanOverrideKey[] = [
  'github_verification',
  'allow_deploy_from_default_branch',
  'require_pr_for_branch',
]

function buildUpdatePayload(overrides: OverrideState) {
  const payload: Record<string, unknown> = {}

  for (const key of STRING_OVERRIDE_KEYS) {
    const value = overrides[key]
    if (value !== null) {
      payload[key] = value || null
    }
  }

  for (const key of BOOLEAN_OVERRIDE_KEYS) {
    const value = overrides[key]
    if (value !== null) {
      payload[key] = value
    }
  }

  return payload
}

function mergeOverride<T>(overrideValue: T | null, currentValue: T): T {
  return overrideValue !== null ? overrideValue : currentValue
}

type VcsCiDisplayValues = {
  vcsProvider: string
  vcsRepo: string
  ciProvider: string
  ciOrgSlug: string
  githubVerification: boolean
  allowDeployFromDefaultBranch: boolean
  requirePRForBranch: boolean
  defaultBranch: string
  verifyGitCommitMode: string
}

type VcsCiFormState = {
  overrides: OverrideState
  display: VcsCiDisplayValues
  hasChanges: boolean
  reset: () => void
  setVcsProvider: (value: string | null) => void
  setVcsRepo: (value: string | null) => void
  setCiProvider: (value: string | null) => void
  setCiOrgSlug: (value: string | null) => void
  setGithubVerification: (value: boolean) => void
  setAllowDeployFromDefaultBranch: (value: boolean) => void
  setRequirePRForBranch: (value: boolean) => void
  setDefaultBranch: (value: string | null) => void
  setVerifyGitCommitMode: (value: string | null) => void
}

function useVcsCiForm(settings: AppSettingsResponse | undefined): VcsCiFormState {
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

  const overrides = useMemo<OverrideState>(
    () => ({
      vcs_provider: vcsProvider,
      vcs_repo: vcsRepo,
      ci_provider: ciProvider,
      ci_org_slug: ciOrgSlug,
      default_branch: defaultBranch,
      verify_git_commit_mode: verifyGitCommitMode,
      github_verification: githubVerification,
      allow_deploy_from_default_branch: allowDeployFromDefaultBranch,
      require_pr_for_branch: requirePRForBranch,
    }),
    [
      allowDeployFromDefaultBranch,
      ciOrgSlug,
      ciProvider,
      defaultBranch,
      githubVerification,
      requirePRForBranch,
      vcsProvider,
      vcsRepo,
      verifyGitCommitMode,
    ]
  )

  const display = useMemo<VcsCiDisplayValues>(() => {
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

    return {
      vcsProvider: mergeOverride(overrides.vcs_provider, currentVcsProvider),
      vcsRepo: mergeOverride(overrides.vcs_repo, currentVcsRepo),
      ciProvider: mergeOverride(overrides.ci_provider, currentCiProvider),
      ciOrgSlug: mergeOverride(overrides.ci_org_slug, currentCiOrgSlug),
      githubVerification: mergeOverride(overrides.github_verification, currentGithubVerification),
      allowDeployFromDefaultBranch: mergeOverride(
        overrides.allow_deploy_from_default_branch,
        currentAllowDeployFromDefaultBranch
      ),
      requirePRForBranch: mergeOverride(overrides.require_pr_for_branch, currentRequirePRForBranch),
      defaultBranch: mergeOverride(overrides.default_branch, currentDefaultBranch),
      verifyGitCommitMode: mergeOverride(
        overrides.verify_git_commit_mode,
        currentVerifyGitCommitMode
      ),
    }
  }, [overrides, settings])

  const hasChanges = useMemo(
    () => Object.values(overrides).some((value) => value !== null),
    [overrides]
  )

  const reset = useCallback(() => {
    setVcsProvider(null)
    setVcsRepo(null)
    setCiProvider(null)
    setCiOrgSlug(null)
    setGithubVerification(null)
    setAllowDeployFromDefaultBranch(null)
    setRequirePRForBranch(null)
    setDefaultBranch(null)
    setVerifyGitCommitMode(null)
  }, [])

  return {
    overrides,
    display,
    hasChanges,
    reset,
    setVcsProvider,
    setVcsRepo,
    setCiProvider,
    setCiOrgSlug,
    setGithubVerification: (value: boolean) => setGithubVerification(value),
    setAllowDeployFromDefaultBranch: (value: boolean) => setAllowDeployFromDefaultBranch(value),
    setRequirePRForBranch: (value: boolean) => setRequirePRForBranch(value),
    setDefaultBranch,
    setVerifyGitCommitMode,
  }
}

export function VCSCIProvidersCard({
  app,
  settings,
  disabled,
  integrations,
}: VCSCIProvidersCardProps) {
  const qc = useQueryClient()

  const githubAvailable = integrations?.github ?? false
  const circleciAvailable = integrations?.circleci ?? false
  const {
    overrides,
    display,
    hasChanges,
    reset,
    setVcsProvider,
    setVcsRepo,
    setCiProvider,
    setCiOrgSlug,
    setGithubVerification,
    setAllowDeployFromDefaultBranch,
    setRequirePRForBranch,
    setDefaultBranch,
    setVerifyGitCommitMode,
  } = useVcsCiForm(settings)

  const {
    vcsProvider: displayVcsProvider,
    vcsRepo: displayVcsRepo,
    ciProvider: displayCiProvider,
    ciOrgSlug: displayCiOrgSlug,
    githubVerification: displayGithubVerification,
    allowDeployFromDefaultBranch: displayAllowDeployFromDefaultBranch,
    requirePRForBranch: displayRequirePRForBranch,
    defaultBranch: displayDefaultBranch,
    verifyGitCommitMode: displayVerifyGitCommitMode,
  } = display

  const updateMutation = useMutation({
    mutationFn: async () => {
      const payload = buildUpdatePayload(overrides)
      if (Object.keys(payload).length === 0) {
        return
      }
      await api.put(`/api/v1/apps/${app}/settings/vcs-ci-deploy`, payload)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['appSettings', app] })
      reset()
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
      reset()
      toast.success('Deploy settings cleared')
    },
    onError: (error: unknown) => {
      const message = extractErrorMessage(error)
      toast.error(message ?? 'Failed to clear settings')
    },
  })

  const handleCancel = () => {
    reset()
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
          <VcsProviderFields
            circleciAvailable={circleciAvailable}
            disabled={disabled}
            displayCiOrgSlug={displayCiOrgSlug}
            displayCiProvider={displayCiProvider}
            displayVcsProvider={displayVcsProvider}
            displayVcsRepo={displayVcsRepo}
            githubAvailable={githubAvailable}
            onChangeCiOrgSlug={setCiOrgSlug}
            onChangeCiProvider={(value) => setCiProvider(value || null)}
            onChangeVcsProvider={setVcsProvider}
            onChangeVcsRepo={setVcsRepo}
            settings={settings}
          />
          <VerificationSettingsFields
            disabled={disabled}
            displayAllowDeployFromDefaultBranch={displayAllowDeployFromDefaultBranch}
            displayDefaultBranch={displayDefaultBranch}
            displayGithubVerification={displayGithubVerification}
            displayRequirePRForBranch={displayRequirePRForBranch}
            displayVerifyGitCommitMode={displayVerifyGitCommitMode}
            githubAvailable={githubAvailable}
            onChangeDefaultBranch={setDefaultBranch}
            onChangeVerifyGitCommitMode={setVerifyGitCommitMode}
            onToggleAllowDeployFromDefaultBranch={setAllowDeployFromDefaultBranch}
            onToggleGithubVerification={setGithubVerification}
            onToggleRequirePRForBranch={setRequirePRForBranch}
            settings={settings}
          />
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
