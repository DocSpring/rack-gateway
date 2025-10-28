import { SourceIndicator } from '@/components/settings/source-indicator'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { AppSettingsResponse } from '@/pages/app-settings/types'

type VcsProviderFieldsProps = {
  disabled: boolean
  githubAvailable: boolean
  circleciAvailable: boolean
  settings: AppSettingsResponse | undefined
  displayVcsProvider: string
  displayVcsRepo: string
  displayCiProvider: string
  displayCiOrgSlug: string
  onChangeVcsProvider: (value: string | null) => void
  onChangeVcsRepo: (value: string) => void
  onChangeCiProvider: (value: string | null) => void
  onChangeCiOrgSlug: (value: string | null) => void
}

export function VcsProviderFields({
  disabled,
  githubAvailable,
  circleciAvailable,
  settings,
  displayVcsProvider,
  displayVcsRepo,
  displayCiProvider,
  displayCiOrgSlug,
  onChangeVcsProvider,
  onChangeVcsRepo,
  onChangeCiProvider,
  onChangeCiOrgSlug,
}: VcsProviderFieldsProps) {
  return (
    <div className="space-y-4">
      <div className={disabled ? 'opacity-50' : ''}>
        <Label htmlFor="vcs-provider">VCS Provider</Label>
        <div className="flex items-center gap-2">
          <select
            className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
            disabled={disabled}
            id="vcs-provider"
            onChange={(event) => onChangeVcsProvider(event.target.value || null)}
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
            onChange={(event) => onChangeVcsRepo(event.target.value)}
            placeholder="org/repo"
            value={displayVcsRepo}
          />
          <SourceIndicator setting={settings?.vcs_repo} />
        </div>
        <p className="mt-1 text-muted-foreground text-xs">
          GitHub repository in <code>org/repo</code> format
        </p>
      </div>

      <div className={disabled ? 'opacity-50' : ''}>
        <Label htmlFor="ci-provider">CI Provider</Label>
        <div className="flex items-center gap-2">
          <select
            className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
            disabled={disabled}
            id="ci-provider"
            onChange={(event) => onChangeCiProvider(event.target.value || null)}
            value={displayCiProvider}
          >
            <option value="">Not set</option>
            <option value="circleci">CircleCI</option>
          </select>
          <SourceIndicator setting={settings?.ci_provider} />
        </div>
      </div>

      <div className={disabled || !circleciAvailable ? 'opacity-50' : ''}>
        <Label htmlFor="ci-org-slug">CircleCI Org Slug</Label>
        <div className="flex items-center gap-2">
          <Input
            disabled={disabled || !circleciAvailable}
            id="ci-org-slug"
            onChange={(event) => onChangeCiOrgSlug(event.target.value)}
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
  )
}
