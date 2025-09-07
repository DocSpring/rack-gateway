import { FileText, Key, LogOut, Users } from 'lucide-react'
import { Link, Outlet, useLocation } from 'react-router-dom'
import { useAuth } from '../contexts/auth-context'
import { cn } from '../lib/utils'
import { ThemeToggle } from './theme-toggle'
import { Button } from './ui/button'
import { Separator } from './ui/separator'

const navigation = [
  { name: 'Users', href: '/users', icon: Users },
  { name: 'API Tokens', href: '/api_tokens', icon: Key },
  { name: 'Audit Logs', href: '/audit_logs', icon: FileText },
]

export function Layout() {
  const { user, logout } = useAuth()
  const location = useLocation()

  return (
    <div className="flex h-screen bg-background">
      {/* Sidebar */}
      <div className="flex w-64 flex-col border-r bg-card">
        {/* Logo */}
        <div className="flex h-16 items-center px-6">
          {/* biome-ignore lint/performance/noImgElement: not using Next.js Image in this Vite app */}
          <img alt="" aria-hidden className="mr-2 h-8 w-8" height={32} src="/logo.svg" width={32} />
          <h1 className="font-semibold text-xl">Convox Gateway</h1>
        </div>

        <Separator />

        {/* Navigation */}
        <nav className="flex-1 space-y-1 px-3 py-4">
          {navigation.map((item) => {
            const Icon = item.icon
            return (
              <Link
                className={cn(
                  'flex items-center rounded-md px-3 py-2 font-medium text-sm transition-colors',
                  location.pathname === item.href
                    ? 'bg-primary text-white'
                    : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                )}
                key={item.name}
                to={item.href}
              >
                <Icon className="mr-3 h-4 w-4" />
                {item.name}
              </Link>
            )
          })}
        </nav>

        <Separator />

        {/* User section */}
        <div className="p-4">
          <div className="mb-3 flex items-center justify-between">
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium text-sm">{user?.name || 'User'}</p>
              <p className="truncate text-muted-foreground text-xs">{user?.email}</p>
            </div>
            <ThemeToggle />
          </div>
          {user?.rack && (
            <div className="mb-3 text-muted-foreground text-xs">
              <div className="group relative inline-flex items-center">
                <span>Rack: {user.rack.name || 'Unknown'}</span>
                <div
                  className="pointer-events-none absolute top-full left-0 z-50 mt-1 hidden w-max max-w-[260px] rounded-md border border-border bg-popover px-2 py-1 text-[11px] text-popover-foreground shadow-md group-hover:block"
                  role="tooltip"
                >
                  {user.rack.host || 'Unknown host'}
                </div>
              </div>
            </div>
          )}
          <Button className="w-full" onClick={logout} size="sm" variant="outline">
            <LogOut className="mr-2 h-4 w-4" />
            Logout
          </Button>
        </div>
      </div>

      {/* Main content */}
      <div className="flex-1 overflow-auto">
        <Outlet />
      </div>
    </div>
  )
}
