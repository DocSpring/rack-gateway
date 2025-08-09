import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { authService } from '../lib/auth'

export function CallbackPage() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const handleCallback = async () => {
      const code = searchParams.get('code')
      const state = searchParams.get('state')
      const urlError = searchParams.get('error')

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
        // Redirect to main app
        navigate('/', { replace: true })
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Authentication failed')
      }
    }

    handleCallback()
  }, [searchParams, navigate])

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-50">
        <div className="w-full max-w-md">
          <div className="rounded-md border border-red-200 bg-red-50 p-4">
            <h3 className="font-medium text-red-800 text-sm">Authentication Error</h3>
            <p className="mt-1 text-red-700 text-sm">{error}</p>
            <button
              className="mt-3 font-medium text-red-600 text-sm hover:text-red-500"
              onClick={() => navigate('/login')}
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
