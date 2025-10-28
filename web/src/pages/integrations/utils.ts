export function getErrorMessage(error: unknown, fallback: string): string {
	if (error && typeof error === 'object' && 'response' in error) {
		const response = (error as { response?: unknown }).response
		if (response && typeof response === 'object' && 'data' in response) {
			const data = (response as { data?: unknown }).data
			if (
				data &&
				typeof data === 'object' &&
				'error' in data &&
				typeof (data as { error?: unknown }).error === 'string'
			) {
				return (data as { error: string }).error
			}
		}
	}
	return fallback
}

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
