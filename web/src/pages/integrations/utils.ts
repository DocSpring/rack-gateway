export function hasStatus(error: unknown, status: number): boolean {
  if (
    !error ||
    typeof error !== 'object' ||
    !('response' in error) ||
    !(error as { response?: unknown }).response
  ) {
    return false
  }

  const response = (error as { response?: unknown }).response
  return Boolean(
    response &&
      typeof response === 'object' &&
      'status' in response &&
      (response as { status?: unknown }).status === status
  )
}
