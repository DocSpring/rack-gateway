const STEP_UP_DEBUG_HANDLER_KEY = '__RGW_STEP_UP_DEBUG__'

export type StepUpDebugHandler = (...args: unknown[]) => void

export function setStepUpDebugHandler(handler: StepUpDebugHandler | null): void {
  const target = globalThis as Record<string, unknown>
  if (handler) {
    target[STEP_UP_DEBUG_HANDLER_KEY] = handler
    return
  }
  delete target[STEP_UP_DEBUG_HANDLER_KEY]
}

export function getStepUpDebugHandler(): StepUpDebugHandler | null {
  const handler = (globalThis as Record<string, unknown>)[STEP_UP_DEBUG_HANDLER_KEY]
  return typeof handler === 'function' ? (handler as StepUpDebugHandler) : null
}

export function debugLog(...args: unknown[]): void {
  const handler = getStepUpDebugHandler()
  handler?.('[STEP_UP]', ...args)
}
