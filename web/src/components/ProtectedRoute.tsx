import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../contexts/AuthContext'

export function ProtectedRoute() {
  const { isAuthenticated, isLoading, user } = useAuth()

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  // Check if user has UI access (viewers don't get UI access)
  const hasUIAccess = user?.roles?.some((role) => ['admin', 'ops', 'deployer'].includes(role))

  if (!hasUIAccess) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <div className="max-w-md w-full">
          <div className="bg-yellow-50 border border-yellow-200 rounded-md p-4">
            <h3 className="text-sm font-medium text-yellow-800">Access Restricted</h3>
            <p className="mt-1 text-sm text-yellow-700">
              Your viewer role does not have access to the management interface. Please use the CLI
              for read-only access to Convox resources.
            </p>
            <button
              onClick={() => (window.location.href = '/login')}
              className="mt-3 text-sm text-yellow-600 hover:text-yellow-500 font-medium"
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
