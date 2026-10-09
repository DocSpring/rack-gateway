import { describe, expect, it, vi } from 'vitest'
import { retryQueuedRequests, StepUpQueue, type StepUpRequest } from './step-up-queue'

const MFA_ERROR = new Error('mfa required')
const isMFAError = (error: unknown) => error === MFA_ERROR
const run = async (action: StepUpRequest['action']) => await action?.()

describe('StepUpQueue', () => {
  it('uses the first request with an action as the primary request', () => {
    const queue = new StepUpQueue()
    const primary = { action: vi.fn() }
    queue.enqueue({})
    queue.enqueue(primary)
    queue.enqueue({ action: vi.fn() })

    expect(queue.primary()).toBe(primary)
    expect(queue.size).toBe(3)
  })

  it('rejects every waiting request on cancel', () => {
    const queue = new StepUpQueue()
    const first = { onReject: vi.fn() }
    const second = { onReject: vi.fn() }
    queue.enqueue(first)
    queue.enqueue(second)

    const error = new Error('cancelled')
    queue.rejectAll(error)

    expect(first.onReject).toHaveBeenCalledWith(error)
    expect(second.onReject).toHaveBeenCalledWith(error)
    expect(queue.size).toBe(0)
  })
})

describe('retryQueuedRequests', () => {
  it('settles every waiting request', async () => {
    const succeeded = { action: () => 'ok', onResolve: vi.fn(), onReject: vi.fn() }
    const failed = {
      action: () => {
        throw new Error('boom')
      },
      onResolve: vi.fn(),
      onReject: vi.fn(),
    }
    const waiting = { onResolve: vi.fn() }
    const requeue = vi.fn()

    await retryQueuedRequests([succeeded, failed, waiting], { run, isMFAError, requeue })

    expect(succeeded.onResolve).toHaveBeenCalledWith('ok')
    expect(failed.onReject).toHaveBeenCalledWith(new Error('boom'))
    expect(waiting.onResolve).toHaveBeenCalledWith()
    expect(requeue).not.toHaveBeenCalled()
  })

  it('queues a request that still needs MFA for a fresh prompt', async () => {
    const needsMFA = {
      action: () => {
        throw MFA_ERROR
      },
      onResolve: vi.fn(),
      onReject: vi.fn(),
    }
    const requeue = vi.fn()

    await retryQueuedRequests([needsMFA], { run, isMFAError, requeue })

    expect(requeue).toHaveBeenCalledWith(needsMFA)
    expect(needsMFA.onResolve).not.toHaveBeenCalled()
    expect(needsMFA.onReject).not.toHaveBeenCalled()
  })
})
