import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { DeployApprovalRequest } from '@/lib/api'

import { ActionButtons, RequestDetailsCard } from './details'

function renderButtons(requestStatus: string, wasApproved: boolean) {
  render(
    <ActionButtons
      approveMutationPending={false}
      extendMutationPending={false}
      onApprove={vi.fn()}
      onExtend={vi.fn()}
      onReject={vi.fn()}
      rejectMutationPending={false}
      requestStatus={requestStatus}
      wasApproved={wasApproved}
    />
  )
}

describe('ActionButtons', () => {
  it('offers to extend a lapsed approval', () => {
    renderButtons('expired', true)
    expect(screen.getByRole('button', { name: /extend/i })).toBeInTheDocument()
  })

  // A request that expired while pending was never approved, and the API refuses to extend it.
  it('does not offer to extend a request that expired while pending', () => {
    renderButtons('expired', false)
    expect(screen.queryByRole('button', { name: /extend/i })).not.toBeInTheDocument()
  })
})

const PR_URL = 'https://github.com/acme/web/pull/7'

function requestWith(fields: Partial<DeployApprovalRequest>): DeployApprovalRequest {
  return {
    created_at: '2026-01-01T00:00:00Z',
    git_branch: 'feature/deploy',
    git_commit_hash: '1111111111111111111111111111111111111111',
    message: 'Deploy web',
    public_id: '6f1c2a8e-4b7d-4e0a-9c3f-1a2b3c4d5e6f',
    status: 'pending',
    target_api_token_id: 'token-1',
    updated_at: '2026-01-01T00:00:00Z',
    ...fields,
  }
}

describe('RequestDetailsCard', () => {
  it('links the pull request', () => {
    render(<RequestDetailsCard request={requestWith({ pr_url: PR_URL })} />)
    expect(screen.getByRole('link', { name: PR_URL })).toHaveAttribute('href', PR_URL)
  })

  it('shows a placeholder when the request has no pull request', () => {
    render(<RequestDetailsCard request={requestWith({})} />)
    expect(screen.getByText('Pull Request:').parentElement).toHaveTextContent('Pull Request: —')
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it.each(['http://github.com/acme/web/pull/7', 'javascript:alert(1)'])(
    'does not link a URL that is not https: %s',
    (url) => {
      render(<RequestDetailsCard request={requestWith({ pr_url: url })} />)
      expect(screen.getByText(url)).toBeInTheDocument()
      expect(screen.queryByRole('link')).not.toBeInTheDocument()
    }
  )
})
