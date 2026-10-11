import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ActionButtons } from './details'

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
