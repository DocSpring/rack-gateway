import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo, useState } from 'react'

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
  | 'default_branch'
  | 'verify_git_commit_mode'
  | 'circleci_approval_job_name'

type BooleanOverrideKey =
  | 'github_verification'
  | 'allow_deploy_from_default_branch'
  | 'require_pr_for_branch'
  | 'circleci_auto_approve_on_approval'

type OverrideState = Record<StringOverrideKey, string | null> &
  Record<BooleanOverrideKey, boolean | null>

const STRING_OVERRIDE_KEYS: readonly StringOverrideKey[] = [
  'vcs_provider',
  'vcs_repo',
  'ci_provider',
  'default_branch',
  'verify_git_commit_mode',
  'circleci_approval_job_name',
]

const BOOLEAN_OVERRIDE_KEYS: readonly BooleanOverrideKey[] = [
  'github_verification',
  'allow_deploy_from_default_branch',
  'require_pr_for_branch',
  'circleci_auto_approve_on_approval',
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
  githubVerification: boolean
  allowDeployFromDefaultBranch: boolean
  requirePRForBranch: boolean
  defaultBranch: string
  verifyGitCommitMode: string
  circleCIAutoApproveOnApproval: boolean
  circleCIApprovalJobName: string
}

type VcsCiFormState = {
  overrides: OverrideState
  display: VcsCiDisplayValues
  hasChanges: boolean
  reset: () => void
  setVcsProvider: (value: string | null) => void
  setVcsRepo: (value: string | null) => void
  setCiProvider: (value: string | null) => void
  setGithubVerification: (value: boolean) => void
  setAllowDeployFromDefaultBranch: (value: boolean) => void
  setRequirePRForBranch: (value: boolean) => void
  setDefaultBranch: (value: string | null) => void
  setVerifyGitCommitMode: (value: string | null) => void
  setCircleCIAutoApproveOnApproval: (value: boolean) => void
  setCircleCIApprovalJobName: (value: string | null) => void
}

function useVcsCiForm(settings: AppSettingsResponse | undefined): VcsCiFormState {
  const [vcsProvider, setVcsProvider] = useState<string | null>(null)
  const [vcsRepo, setVcsRepo] = useState<string | null>(null)
  const [ciProvider, setCiProvider] = useState<string | null>(null)
  const [githubVerification, setGithubVerification] = useState<boolean | null>(null)
  const [allowDeployFromDefaultBranch, setAllowDeployFromDefaultBranch] = useState<boolean | null>(
    null
  )
  const [requirePRForBranch, setRequirePRForBranch] = useState<boolean | null>(null)
  const [defaultBranch, setDefaultBranch] = useState<string | null>(null)
  const [verifyGitCommitMode, setVerifyGitCommitMode] = useState<string | null>(null)
  const [circleCIAutoApproveOnApproval, setCircleCIAutoApproveOnApproval] = useState<
    boolean | null
  >(null)
  const [circleCIApprovalJobName, setCircleCIApprovalJobName] = useState<string | null>(null)

  const overrides = useMemo<OverrideState>(
    () => ({
      vcs_provider: vcsProvider,
      vcs_repo: vcsRepo,
      ci_provider: ciProvider,
      default_branch: defaultBranch,
      verify_git_commit_mode: verifyGitCommitMode,
      circleci_approval_job_name: circleCIApprovalJobName,
      github_verification: githubVerification,
      allow_deploy_from_default_branch: allowDeployFromDefaultBranch,
      require_pr_for_branch: requirePRForBranch,
      circleci_auto_approve_on_approval: circleCIAutoApproveOnApproval,
    }),
    [
      allowDeployFromDefaultBranch,
      ciProvider,
      circleCIApprovalJobName,
      circleCIAutoApproveOnApproval,
      defaultBranch,
      githubVerification,
      requirePRForBranch,
      vcsProvider,
      vcsRepo,
      verifyGitCommitMode,
    ]
  )

  const display = useMemo<VcsCiDisplayValues>(() => {
    const rawVcsProvider = getSettingValue(settings?.vcs_provider, '')
    const currentVcsProvider =
      rawVcsProvider || (settings?.vcs_provider?.source === 'global_default' ? 'github' : '')
    const currentVcsRepo = getSettingValue(settings?.vcs_repo, '')
    const rawCiProvider = getSettingValue(settings?.ci_provider, '')
    const currentCiProvider =
      rawCiProvider || (settings?.ci_provider?.source === 'global_default' ? 'circleci' : '')
    const currentGithubVerification = getSettingValue(settings?.github_verification, true)
    const currentAllowDeployFromDefaultBranch = getSettingValue(
      settings?.allow_deploy_from_default_branch,
      false
    )
    const currentRequirePRForBranch = getSettingValue(settings?.require_pr_for_branch, true)
    const currentDefaultBranch = getSettingValue(settings?.default_branch, 'main')
    const currentVerifyGitCommitMode = getSettingValue(settings?.verify_git_commit_mode, 'latest')
    const currentCircleCIAutoApproveOnApproval = getSettingValue(
      settings?.circleci_auto_approve_on_approval,
      false
    )
    const currentCircleCIApprovalJobName = getSettingValue(settings?.circleci_approval_job_name, '')

    return {
      vcsProvider: mergeOverride(overrides.vcs_provider, currentVcsProvider),
      vcsRepo: mergeOverride(overrides.vcs_repo, currentVcsRepo),
      ciProvider: mergeOverride(overrides.ci_provider, currentCiProvider),
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
      circleCIAutoApproveOnApproval: mergeOverride(
        overrides.circleci_auto_approve_on_approval,
        currentCircleCIAutoApproveOnApproval
      ),
      circleCIApprovalJobName: mergeOverride(
        overrides.circleci_approval_job_name,
        currentCircleCIApprovalJobName
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
    setGithubVerification(null)
    setAllowDeployFromDefaultBranch(null)
    setRequirePRForBranch(null)
    setDefaultBranch(null)
    setVerifyGitCommitMode(null)
    setCircleCIAutoApproveOnApproval(null)
    setCircleCIApprovalJobName(null)
  }, [])

  return {
    overrides,
    display,
    hasChanges,
    reset,
    setVcsProvider,
    setVcsRepo,
    setCiProvider,
    setGithubVerification: (value: boolean) => setGithubVerification(value),
    setAllowDeployFromDefaultBranch: (value: boolean) => setAllowDeployFromDefaultBranch(value),
    setRequirePRForBranch: (value: boolean) => setRequirePRForBranch(value),
    setDefaultBranch,
    setVerifyGitCommitMode,
    setCircleCIAutoApproveOnApproval: (value: boolean) => setCircleCIAutoApproveOnApproval(value),
    setCircleCIApprovalJobName,
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
    setGithubVerification,
    setAllowDeployFromDefaultBranch,
    setRequirePRForBranch,
    setDefaultBranch,
    setVerifyGitCommitMode,
    setCircleCIAutoApproveOnApproval,
    setCircleCIApprovalJobName,
  } = useVcsCiForm(settings)

  const {
    vcsProvider: displayVcsProvider,
    vcsRepo: displayVcsRepo,
    ciProvider: displayCiProvider,
    githubVerification: displayGithubVerification,
    allowDeployFromDefaultBranch: displayAllowDeployFromDefaultBranch,
    requirePRForBranch: displayRequirePRForBranch,
    defaultBranch: displayDefaultBranch,
    verifyGitCommitMode: displayVerifyGitCommitMode,
    circleCIAutoApproveOnApproval: displayCircleCIAutoApproveOnApproval,
    circleCIApprovalJobName: displayCircleCIApprovalJobName,
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
    settings?.github_verification?.source === 'db' ||
    settings?.allow_deploy_from_default_branch?.source === 'db' ||
    settings?.require_pr_for_branch?.source === 'db' ||
    settings?.default_branch?.source === 'db' ||
    settings?.verify_git_commit_mode?.source === 'db' ||
    settings?.circleci_auto_approve_on_approval?.source === 'db' ||
    settings?.circleci_approval_job_name?.source === 'db'

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
            displayCiProvider={displayCiProvider}
            displayVcsProvider={displayVcsProvider}
            displayVcsRepo={displayVcsRepo}
            githubAvailable={githubAvailable}
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

        <div className="space-y-4">
          <h3 className="font-medium text-sm">CircleCI Deploy Approval Settings</h3>
          <div className={disabled || !circleciAvailable ? 'opacity-50' : ''}>
            <Label htmlFor="circleci-auto-approve">Auto-approve CircleCI job on approval</Label>
            <div className="flex items-center gap-2">
              <input
                checked={displayCircleCIAutoApproveOnApproval}
                disabled={disabled || !circleciAvailable}
                id="circleci-auto-approve"
                onChange={(event) => setCircleCIAutoApproveOnApproval(event.target.checked)}
                type="checkbox"
              />
              <SourceIndicator setting={settings?.circleci_auto_approve_on_approval} />
            </div>
            <p className="mt-1 text-muted-foreground text-xs">
              Automatically trigger the CircleCI approval job when this deploy approval is approved
            </p>
          </div>

          <div className={disabled || !circleciAvailable ? 'opacity-50' : ''}>
            <Label htmlFor="circleci-approval-job-name">CircleCI Approval Job Name</Label>
            <div className="flex items-center gap-2">
              <Input
                disabled={disabled || !circleciAvailable}
                id="circleci-approval-job-name"
                onChange={(event) => setCircleCIApprovalJobName(event.target.value)}
                placeholder="approve_deploy_staging"
                value={displayCircleCIApprovalJobName}
              />
              <SourceIndicator setting={settings?.circleci_approval_job_name} />
            </div>
            <p className="mt-1 text-muted-foreground text-xs">
              The name of the CircleCI approval job to auto-approve (e.g., "approve_deploy_staging")
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
