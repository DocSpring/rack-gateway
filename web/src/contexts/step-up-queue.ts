export type StepUpAction = (() => Promise<unknown>) | (() => unknown) | null

export type StepUpRequest = {
  action?: StepUpAction
  onResolve?: (value?: unknown) => void
  onReject?: (error: unknown) => void
}

/**
 * Requests waiting on one MFA prompt. Several requests can fail with an MFA error at the same
 * time (for example every query on a page), and each caller's promise must settle once the user
 * verifies or cancels.
 */
export class StepUpQueue {
  private requests: StepUpRequest[] = []

  enqueue(request: StepUpRequest): void {
    this.requests.push(request)
  }

  get size(): number {
    return this.requests.length
  }

  /** The request that is retried with the MFA credential: the first one that has an action. */
  primary(): StepUpRequest | undefined {
    return this.requests.find((request) => request.action)
  }

  /** Removes and returns every queued request. */
  drain(): StepUpRequest[] {
    const drained = this.requests
    this.requests = []
    return drained
  }

  rejectAll(error: unknown): void {
    for (const request of this.drain()) {
      request.onReject?.(error)
    }
  }
}

type RetryOptions = {
  run: (action: StepUpAction) => Promise<unknown>
  isMFAError: (error: unknown) => boolean
  requeue: (request: StepUpRequest) => void
}

/**
 * Retries requests that were waiting behind a successful MFA verification. They run without the
 * MFA credential (a TOTP code works once), relying on the step-up the verification just recorded.
 * A request that still needs MFA (an "always" route) is queued again for a fresh prompt.
 */
export async function retryQueuedRequests(
  requests: StepUpRequest[],
  { run, isMFAError, requeue }: RetryOptions
): Promise<void> {
  await Promise.all(
    requests.map(async (request) => {
      if (!request.action) {
        request.onResolve?.()
        return
      }
      try {
        request.onResolve?.(await run(request.action))
      } catch (error) {
        if (isMFAError(error)) {
          requeue(request)
          return
        }
        request.onReject?.(error)
      }
    })
  )
}

export function createCancelledError(): Error {
  // suppressToast prevents a duplicate toast; the user cancelled on purpose.
  const error = new Error('MFA verification cancelled') as Error & { suppressToast?: boolean }
  error.suppressToast = true
  return error
}
