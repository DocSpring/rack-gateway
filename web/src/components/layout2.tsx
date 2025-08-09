import { Outlet } from 'react-router-dom'
import { useAuth } from '../contexts/auth-context'
import { ThemeToggle } from './theme-toggle'

export function Layout() {
  const { user, logout } = useAuth()

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="border-border border-b bg-card shadow-sm">
        <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
          <div className="flex h-16 items-center justify-between">
            <div className="flex items-center space-x-8">
              <h1 className="font-semibold text-foreground text-xl">Convox Gateway</h1>
              <span className="text-muted-foreground text-sm">User Management</span>
            </div>
            <div className="flex items-center space-x-4">
              <ThemeToggle />
              <span className="text-muted-foreground text-sm">{user?.email}</span>
              <button
                className="font-medium text-destructive text-sm hover:text-destructive/80"
                onClick={logout}
                type="button"
              >
                Logout
              </button>
            </div>
          </div>
        </div>
      </header>

      {/* Main content */}
      <main className="mx-auto max-w-7xl py-6 sm:px-6 lg:px-8">
        <Outlet />
      </main>
    </div>
  )
}
