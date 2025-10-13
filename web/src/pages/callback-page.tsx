import { useNavigate, useRouter } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { authService } from '../lib/auth'

export function CallbackPage() {
  const navigate = useNavigate()
  const router = useRouter()
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const handleCallback = async () => {
      const search = router.history.location.search || ''
      const sp = new URLSearchParams(search)
      const code = sp.get('code')
      const state = sp.get('state')
      const urlError = sp.get('error')

      if (urlError) {
        setError(`Authentication failed: ${urlError}`)
        return
      }

      if (!(code && state)) {
        setError('Invalid callback parameters')
        return
      }

      try {
        await authService.handleCallback(code, state)
        // Redirect to Rack page after successful login
        navigate({ to: '/rack', replace: true })
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Authentication failed')
      }
    }

    handleCallback()
  }, [router, navigate])

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-50">
        <div className="w-full max-w-md">
          <div className="rounded-md border border-red-200 bg-red-50 p-4">
            <h3 className="font-medium text-red-800 text-sm">Authentication Error</h3>
            <p className="mt-1 text-red-700 text-sm">{error}</p>
            <button
              className="mt-3 font-medium text-red-600 text-sm hover:text-red-500"
              onClick={() => navigate({ to: '/app/login' })}
              type="button"
            >
              Back to login
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50">
      <div className="text-center">
        <div className="mx-auto h-12 w-12 animate-spin rounded-full border-blue-600 border-b-2" />
        <p className="mt-4 text-gray-600 text-sm">Completing authentication...</p>
      </div>
    </div>
  )
}
