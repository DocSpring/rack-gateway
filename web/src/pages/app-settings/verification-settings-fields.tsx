import { SourceIndicator } from '@/components/settings/source-indicator'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { AppSettingsResponse } from '@/pages/app-settings/types'

type VerificationSettingsFieldsProps = {
  disabled: boolean
  githubAvailable: boolean
  settings: AppSettingsResponse | undefined
  displayGithubVerification: boolean
  displayAllowDeployFromDefaultBranch: boolean
  displayRequirePRForBranch: boolean
  displayDefaultBranch: string
  displayVerifyGitCommitMode: string
  onToggleGithubVerification: (value: boolean) => void
  onToggleAllowDeployFromDefaultBranch: (value: boolean) => void
  onToggleRequirePRForBranch: (value: boolean) => void
  onChangeDefaultBranch: (value: string) => void
  onChangeVerifyGitCommitMode: (value: string) => void
}

export function VerificationSettingsFields({
  disabled,
  githubAvailable,
  settings,
  displayGithubVerification,
  displayAllowDeployFromDefaultBranch,
  displayRequirePRForBranch,
  displayDefaultBranch,
  displayVerifyGitCommitMode,
  onToggleGithubVerification,
  onToggleAllowDeployFromDefaultBranch,
  onToggleRequirePRForBranch,
  onChangeDefaultBranch,
  onChangeVerifyGitCommitMode,
}: VerificationSettingsFieldsProps) {
  const sectionDisabled = disabled || !githubAvailable

  return (
    <div className="space-y-4">
      <div className="space-y-4">
        <label
          className={`flex items-center gap-3 ${sectionDisabled ? 'cursor-not-allowed opacity-50' : ''}`}
        >
          <input
            checked={displayGithubVerification}
            disabled={sectionDisabled}
            onChange={(event) => onToggleGithubVerification(event.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Enable GitHub verification</span>
          <SourceIndicator setting={settings?.github_verification} />
        </label>
        <p className="text-muted-foreground text-xs">
          Verify git commits against GitHub when creating deploy approval requests.
        </p>

        <label
          className={`flex items-center gap-3 ${sectionDisabled ? 'cursor-not-allowed opacity-50' : ''}`}
        >
          <input
            checked={displayAllowDeployFromDefaultBranch}
            disabled={sectionDisabled}
            onChange={(event) => onToggleAllowDeployFromDefaultBranch(event.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Allow deploy from default branch</span>
          <SourceIndicator setting={settings?.allow_deploy_from_default_branch} />
        </label>
        <p className="text-muted-foreground text-xs">
          When disabled, deployments must be from a non-default branch.
        </p>

        <label
          className={`flex items-center gap-3 ${sectionDisabled ? 'cursor-not-allowed opacity-50' : ''}`}
        >
          <input
            checked={displayRequirePRForBranch}
            disabled={sectionDisabled}
            onChange={(event) => onToggleRequirePRForBranch(event.target.checked)}
            type="checkbox"
          />
          <span className="font-medium text-sm">Require PR for branch</span>
          <SourceIndicator setting={settings?.require_pr_for_branch} />
        </label>
        <p className="text-muted-foreground text-xs">
          Require a GitHub pull request to exist for the branch being deployed.
        </p>
      </div>

      <div className={sectionDisabled ? 'opacity-50' : ''}>
        <Label htmlFor="default-branch">Default Branch</Label>
        <div className="flex items-center gap-2">
          <Input
            disabled={sectionDisabled}
            id="default-branch"
            onChange={(event) => onChangeDefaultBranch(event.target.value)}
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

      <div className={sectionDisabled ? 'opacity-50' : ''}>
        <Label htmlFor="verify-git-commit-mode">Git Commit Verification Mode</Label>
        <div className="flex items-center gap-2">
          <select
            className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
            disabled={sectionDisabled}
            id="verify-git-commit-mode"
            onChange={(event) => onChangeVerifyGitCommitMode(event.target.value)}
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
  )
}
