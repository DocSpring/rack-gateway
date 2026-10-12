import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { DbAuditVerificationStatus } from '@/api/schemas'

import { AuditVerificationSummary } from './verification-status'

const PASSED: DbAuditVerificationStatus = {
  checked_at: '2026-05-01T00:30:00Z',
  result: 'passed',
  failure_count: 0,
  warning_count: 0,
  summary: 'Checking every event\nResult: OK\n',
  full_checked_at: '2026-05-01T00:30:00Z',
  last_passed_at: '2026-05-01T00:30:00Z',
}

describe('AuditVerificationSummary', () => {
  it('says when the audit chain has not been verified yet', () => {
    render(<AuditVerificationSummary status={null} />)
    expect(screen.getByText(/Audit chain not verified yet/)).toBeInTheDocument()
  })

  it('shows a passing verification', () => {
    render(<AuditVerificationSummary status={PASSED} />)
    expect(screen.getByText('Audit chain verified')).toBeInTheDocument()
    expect(screen.getByText(/last full check/)).toBeInTheDocument()
    expect(screen.queryByText(/failing since/)).not.toBeInTheDocument()
  })

  it('shows how long a verification has been failing, and its report', () => {
    render(
      <AuditVerificationSummary
        status={{
          ...PASSED,
          result: 'failed',
          failure_count: 2,
          failing_since: '2026-05-01T00:30:00Z',
          summary: 'FAILURES (2):\n  - event 4 (id 5): event_hash does not match\n',
        }}
      />
    )
    expect(
      screen.getByText('Audit chain verification failed: 2 integrity failures')
    ).toBeInTheDocument()
    expect(screen.getByText(/failing since/)).toBeInTheDocument()
    expect(screen.getByText(/event 4 \(id 5\)/)).toBeInTheDocument()
  })

  it('says when verification could not run', () => {
    render(
      <AuditVerificationSummary
        status={{
          ...PASSED,
          result: 'error',
          failing_since: '2026-05-01T00:30:00Z',
          summary: "Audit log verification couldn't run: AccessDenied\n",
        }}
      />
    )
    expect(screen.getByText("Audit chain verification couldn't run")).toBeInTheDocument()
    expect(screen.getByText(/AccessDenied/)).toBeInTheDocument()
  })
})
