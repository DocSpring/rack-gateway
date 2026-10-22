export type StepUpAction = (() => Promise<unknown>) | (() => unknown) | null

export interface StepUpRequest {
  action?: StepUpAction
  onResolve?: (value?: unknown) => void
  onReject?: (error: unknown) => void
  // verify marks a prompt that isn't retrying a protected request: the gateway checks the answer (which opens a
  // step-up window) before action runs. Otherwise action is retried with the answer as its MFA proof.
  verify?: boolean
}

/**
 * Requests waiting on one MFA prompt. Several requests can fail with an MFA error at the same
 * time (for example every query on a page), and each caller's promise must settle once the user
 * verifies or cancels.
 */
export class StepUpQueue {
  private requests: StepUpRequest[] = []
  private cancellations = 0

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

  /** Rejects every queued request because the user cancelled the prompt. */
  cancel(error: unknown): void {
    this.cancellations += 1
    this.rejectAll(error)
  }

  /** How many times the prompt was cancelled: a retry batch compares it to tell whether the user cancelled. */
  get cancelCount(): number {
    return this.cancellations
  }
}

interface RetryOptions {
  run: (action: StepUpAction) => Promise<unknown>
  isMFAError: (error: unknown) => boolean
  requeue: (request: StepUpRequest) => void
  // cancelled reports whether the user cancelled a prompt since the batch started
  cancelled: () => boolean
}

/**
 * Retries requests that were waiting behind a successful MFA verification. They run without the
 * MFA credential (a TOTP code works once), relying on the step-up the verification just recorded.
 * A request that still needs MFA (an "always" route) is queued again for a fresh prompt, unless the
 * user cancelled a prompt meanwhile: then it's rejected as cancelled, rather than opening another.
 */
export async function retryQueuedRequests(
  requests: StepUpRequest[],
  { run, isMFAError, requeue, cancelled }: RetryOptions
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
        if (!isMFAError(error)) {
          request.onReject?.(error)
        } else if (cancelled()) {
          request.onReject?.(createCancelledError())
        } else {
          requeue(request)
        }
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
