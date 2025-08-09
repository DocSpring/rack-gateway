import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../contexts/auth-context'

export function ProtectedRoute() {
  const { isAuthenticated, isLoading, user } = useAuth()

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="h-12 w-12 animate-spin rounded-full border-blue-600 border-b-2" />
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate replace to="/login" />
  }

  // Check if user has UI access (viewers don't get UI access)
  const hasUIAccess = user?.roles?.some((role) => ['admin', 'ops', 'deployer'].includes(role))

  if (!hasUIAccess) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gray-50">
        <div className="w-full max-w-md">
          <div className="rounded-md border border-yellow-200 bg-yellow-50 p-4">
            <h3 className="font-medium text-sm text-yellow-800">Access Restricted</h3>
            <p className="mt-1 text-sm text-yellow-700">
              Your viewer role does not have access to the management interface. Please use the CLI
              for read-only access to Convox resources.
            </p>
            <button
              className="mt-3 font-medium text-sm text-yellow-600 hover:text-yellow-500"
              onClick={() => {
                window.location.href = '/login'
              }}
              type="button"
            >
              Sign in with a different account
            </button>
          </div>
        </div>
      </div>
    )
  }

  return <Outlet />
}
