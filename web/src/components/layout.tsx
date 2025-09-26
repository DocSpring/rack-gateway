import { Link, Navigate, Outlet, useLocation } from '@tanstack/react-router'
import {
  Boxes,
  FileText,
  Key,
  LogOut,
  type LucideIcon,
  Server,
  Settings,
  Shield,
  TerminalSquare,
  Users,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useAuth } from '../contexts/auth-context'
import { cn } from '../lib/utils'
import { ThemeToggle } from './theme-toggle'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from './ui/dialog'
import { Separator } from './ui/separator'

type NavigationItem = {
  name: string
  icon: LucideIcon
  href?: string
  onSelect?: () => void
  disabled?: boolean
}

const baseNavigation: NavigationItem[] = [
  { name: 'Rack', href: '/rack', icon: Server },
  { name: 'Apps', href: '/apps', icon: Boxes },
  { name: 'Processes', href: '/processes', icon: TerminalSquare },
  { name: 'Instances', href: '/instances', icon: TerminalSquare },
  { name: 'Builds', href: '/builds', icon: FileText },
  { name: 'Releases', href: '/releases', icon: FileText },
  { name: 'Users', href: '/users', icon: Users },
  { name: 'API Tokens', href: '/api_tokens', icon: Key },
  { name: 'Audit Logs', href: '/audit_logs', icon: FileText },
]

const USER_AUDIT_RE = /\/users\/[^/]+\/audit_logs/

function isNavigationItemActive(item: NavigationItem, pathname: string): boolean {
  if (!item.href) {
    return false
  }
  if (item.href === '/rack') {
    return pathname === '/rack'
  }
  if (item.href === '/audit_logs') {
    return pathname.startsWith('/audit_logs') || USER_AUDIT_RE.test(pathname)
  }
  if (item.href === '/users') {
    return (
      pathname === '/users' || (pathname.startsWith('/users/') && !USER_AUDIT_RE.test(pathname))
    )
  }
  if (item.href === '/') {
    return pathname === '/'
  }
  return pathname === item.href || pathname.startsWith(`${item.href}/`)
}

export function Layout() {
  const { user, logout } = useAuth()
  const location = useLocation()
  const pathname = useMemo(() => {
    const p = location.pathname || ''
    const base = '/.gateway/web'
    if (p === base) {
      return '/'
    }
    if (p.startsWith(`${base}/`)) {
      const trimmed = p.slice(base.length)
      return trimmed === '' ? '/' : trimmed
    }
    return p || '/'
  }, [location.pathname])
  const [showCliDialog, setShowCliDialog] = useState(false)

  const needsMfaEnrollment = Boolean(user?.mfaRequired && !user?.mfaEnrolled)

  const rackAlias = user?.rack?.alias ?? user?.rack?.name ?? 'default'
  const gatewayOrigin = useMemo(() => {
    try {
      return window.location.origin
    } catch {
      return 'https://gateway.example.com'
    }
  }, [])

  const navigation = useMemo<NavigationItem[]>(() => {
    const nav = baseNavigation.slice()

    nav.push({ name: 'Account Security', href: '/account/security', icon: Shield })

    nav.push({
      name: 'Configure CLI',
      icon: TerminalSquare,
      onSelect: () => setShowCliDialog(true),
    })

    if (user?.roles?.includes('admin')) {
      nav.push({ name: 'Settings', href: '/settings', icon: Settings })
    }

    if (needsMfaEnrollment) {
      return nav
        .map((item) => {
          if (item.href === '/account/security') {
            return item
          }
          return { ...item, disabled: true }
        })
        .sort((a, b) => {
          const aIsAccount = a.href === '/account/security'
          const bIsAccount = b.href === '/account/security'
          if (aIsAccount && !bIsAccount) {
            return -1
          }
          if (!aIsAccount && bIsAccount) {
            return 1
          }
          return 0
        })
    }

    return nav
  }, [needsMfaEnrollment, user?.roles])

  const currentUserHref = useMemo(() => {
    if (!user?.email) {
      return null
    }
    return `/users/${encodeURIComponent(user.email)}`
  }, [user?.email])

  // Declarative redirect: when at layout root, go to Rack
  if (needsMfaEnrollment && pathname !== '/account/security') {
    return <Navigate replace to="/account/security" />
  }

  if (pathname === '/') {
    return <Navigate replace to={needsMfaEnrollment ? '/account/security' : '/rack'} />
  }

  return (
    <div className="flex h-screen bg-background">
      {/* Sidebar */}
      <div className="flex w-64 flex-col border-r bg-card">
        {/* Logo */}
        <div className="flex h-16 items-center px-6">
          {/* biome-ignore lint/performance/noImgElement: not using Next.js Image in this Vite app */}
          <img
            alt=""
            aria-hidden
            className="mr-3 size-7"
            height={32}
            src="/.gateway/web/logo.svg"
            width={32}
          />
          <h1 className="font-semibold text-xl">Convox Gateway</h1>
        </div>

        <Separator />

        {/* Navigation */}
        <nav className="flex-1 space-y-1 px-3 py-4">
          {navigation.map((item) => {
            const Icon = item.icon
            const isActive = isNavigationItemActive(item, pathname)
            const itemClassName = cn(
              'flex items-center rounded-md px-3 py-2 font-medium text-sm transition-colors',
              isActive
                ? 'bg-accent text-foreground'
                : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
              item.disabled && 'pointer-events-none cursor-not-allowed opacity-50'
            )

            if (item.href) {
              if (item.disabled) {
                return (
                  <span aria-disabled="true" className={itemClassName} key={item.name}>
                    <Icon className="mr-4 h-6 w-6" />
                    {item.name}
                  </span>
                )
              }
              return (
                <Link
                  aria-disabled={item.disabled ? true : undefined}
                  className={itemClassName}
                  key={item.name}
                  tabIndex={item.disabled ? -1 : undefined}
                  to={item.href}
                >
                  <Icon className="mr-4 h-6 w-6" />
                  {item.name}
                </Link>
              )
            }

            return (
              <button
                className={cn(itemClassName, 'w-full', !item.disabled && 'cursor-pointer')}
                disabled={item.disabled}
                key={item.name}
                onClick={item.onSelect}
                type="button"
              >
                <Icon className="mr-4 h-6 w-6" />
                {item.name}
              </button>
            )
          })}
        </nav>

        <Separator />

        {/* User section */}
        <div className="p-4">
          <div className="mb-3 flex items-center justify-between gap-2">
            {currentUserHref ? (
              <Link
                className="group block min-w-0 flex-1 rounded-md px-1 py-0.5 transition-colors hover:bg-accent/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
                to={currentUserHref}
              >
                <p className="truncate font-medium text-sm group-hover:underline">
                  {user?.name || 'User'}
                </p>
                <p className="truncate text-muted-foreground text-xs group-hover:underline">
                  {user?.email}
                </p>
              </Link>
            ) : (
              <div className="min-w-0 flex-1">
                <p className="truncate font-medium text-sm">{user?.name || 'User'}</p>
                <p className="truncate text-muted-foreground text-xs">{user?.email}</p>
              </div>
            )}
            <ThemeToggle />
          </div>
          {user?.rack && (
            <div className="mb-3 text-muted-foreground text-xs">
              <div className="group relative inline-flex items-center">
                <span>Rack: {user.rack.alias || user.rack.name || 'Unknown'}</span>
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
        {needsMfaEnrollment ? (
          <div className="bg-destructive text-destructive-foreground">
            <div className="px-6 py-3 font-semibold text-sm">
              Multi-factor authentication is required before you can access the rest of the app.
              Visit <span className="underline">Account Security</span> to finish setup.
            </div>
          </div>
        ) : null}
        <Outlet />
      </div>

      {/* Configure CLI Dialog */}
      <Dialog onOpenChange={setShowCliDialog} open={showCliDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Configure CLI</DialogTitle>
            <DialogDescription>
              Follow these steps to install and authenticate the Convox gateway CLI.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 text-sm">
            <p>Clone the repository and install the CLI:</p>
            <div className="rounded-md border bg-muted p-3 font-mono text-xs">
              <div>git clone git@github.com:DocSpring/convox-gateway.git</div>
              <div className="mt-1">cd convox-gateway</div>
              <div className="mt-1">./scripts/install.sh</div>
            </div>

            <p className="pt-1">Authenticate the CLI against this gateway:</p>
            <div className="rounded-md border bg-muted p-3 font-mono text-xs">
              <div>
                convox-gateway login {rackAlias} {gatewayOrigin}
              </div>
            </div>
            <p className="text-muted-foreground">
              After logging in, you can run Convox commands via the gateway using{' '}
              <span className="font-mono">convox-gateway convox …</span>
            </p>
            <p className="text-muted-foreground">
              See the{' '}
              <a
                className="underline hover:no-underline"
                href="https://github.com/DocSpring/convox-gateway/blob/main/README.md"
                rel="noreferrer noopener"
                target="_blank"
              >
                README
              </a>{' '}
              for more information.
            </p>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
