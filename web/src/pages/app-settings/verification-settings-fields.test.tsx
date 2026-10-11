import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { VerificationSettingsFields } from './verification-settings-fields'

function renderFields(props: {
  githubAvailable: boolean
  githubVerification: boolean
  vcsRepo: string
}) {
  return render(
    <VerificationSettingsFields
      disabled={false}
      displayAllowDeployFromDefaultBranch={false}
      displayDefaultBranch="main"
      displayGithubVerification={props.githubVerification}
      displayRequirePRForBranch={true}
      displayVcsRepo={props.vcsRepo}
      displayVerifyGitCommitMode="branch"
      githubAvailable={props.githubAvailable}
      onChangeDefaultBranch={vi.fn()}
      onChangeVerifyGitCommitMode={vi.fn()}
      onToggleAllowDeployFromDefaultBranch={vi.fn()}
      onToggleGithubVerification={vi.fn()}
      onToggleRequirePRForBranch={vi.fn()}
      settings={undefined}
    />
  )
}

describe('VerificationSettingsFields', () => {
  it('warns that requests are refused when verification is on without a repository', () => {
    renderFields({ githubAvailable: true, githubVerification: true, vcsRepo: '' })
    expect(screen.getByRole('alert')).toHaveTextContent('no GitHub repository is set')
  })

  it('warns when the gateway has no GitHub token, and still lets verification be turned off', () => {
    renderFields({ githubAvailable: false, githubVerification: true, vcsRepo: 'acme/web' })
    expect(screen.getByRole('alert')).toHaveTextContent('the gateway has no GITHUB_TOKEN')
    expect(screen.getByLabelText(/Enable GitHub verification/)).toBeEnabled()
  })

  it('shows no warning when verification can run, or is off', () => {
    renderFields({ githubAvailable: true, githubVerification: true, vcsRepo: 'acme/web' })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('does not let verification be turned on without a GitHub token', () => {
    renderFields({ githubAvailable: false, githubVerification: false, vcsRepo: '' })
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.getByLabelText(/Enable GitHub verification/)).toBeDisabled()
  })
})
