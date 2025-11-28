import { Link, Navigate, Outlet, useLocation } from '@tanstack/react-router'
import {
  Blocks,
  Boxes,
  Cpu,
  Hammer,
  HardDrive,
  Key,
  ListChecks,
  Lock,
  LogOut,
  Logs,
  type LucideIcon,
  Puzzle,
  Server,
  ServerCog,
  Settings,
  TerminalSquare,
  Users,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useAuth } from '../contexts/auth-context'
import { cn } from '../lib/utils'
import { CliSetupDialog } from './cli-setup-dialog'
import { ThemeToggle } from './theme-toggle'
import { Button } from './ui/button'
import { Separator } from './ui/separator'

type NavigationItem = {
  name: string
  icon: LucideIcon
  href?: string
  onSelect?: () => void
  disabled?: boolean
}

const USER_AUDIT_RE = /\/users\/[^/]+\/audit-logs/

function isNavigationItemActive(item: NavigationItem, pathname: string): boolean {
  if (!item.href) {
    return false
  }
  if (item.href === '/rack') {
    return pathname === '/rack'
  }
  if (item.href === '/audit-logs') {
    return pathname.startsWith('/audit-logs') || USER_AUDIT_RE.test(pathname)
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

function buildNavigationItems(
  userRoles: string[] | undefined,
  setShowCliDialog: (show: boolean) => void
): NavigationItem[] {
  const nav: NavigationItem[] = [
    { name: 'Rack', href: '/rack', icon: Server },
    { name: 'Apps', href: '/apps', icon: Boxes },
    { name: 'Processes', href: '/processes', icon: Cpu },
    { name: 'Instances', href: '/instances', icon: HardDrive },
    { name: 'Builds', href: '/builds', icon: Hammer },
    { name: 'Releases', href: '/releases', icon: Blocks },
    { name: 'Users', href: '/users', icon: Users },
    { name: 'API Tokens', href: '/api-tokens', icon: Key },
  ]

  if (userRoles?.includes('admin')) {
    nav.push({
      name: 'Deploy Approvals',
      href: '/deploy-approval-requests',
      icon: ListChecks,
    })
  }

  nav.push({ name: 'Audit Logs', href: '/audit-logs', icon: Logs })
  nav.push({
    name: 'Account Security',
    href: '/account/security',
    icon: Lock,
  })

  if (userRoles?.includes('admin')) {
    nav.push({ name: 'Integrations', href: '/integrations', icon: Puzzle })
    nav.push({ name: 'Settings', href: '/settings', icon: Settings })
    nav.push({ name: 'Background Jobs', href: '/jobs', icon: ServerCog })
  }

  nav.push({
    name: 'Configure CLI',
    icon: TerminalSquare,
    onSelect: () => setShowCliDialog(true),
  })

  return nav
}

function applyMfaEnrollmentRestrictions(nav: NavigationItem[]): NavigationItem[] {
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

function normalizePathname(locationPathname: string): string {
  const p = locationPathname || ''
  const base = '/web'
  if (p === base) {
    return '/'
  }
  if (p.startsWith(`${base}/`)) {
    const trimmed = p.slice(base.length)
    return trimmed === '' ? '/' : trimmed
  }
  return p || '/'
}

function handleAuthRedirect(pathname: string): void {
  const redirectTarget = pathname !== '/' ? `/app${pathname}` : undefined
  const loginUrl = redirectTarget
    ? `/app/login?returnTo=${encodeURIComponent(redirectTarget)}`
    : '/app/login'
  if (typeof window !== 'undefined') {
    window.location.href = loginUrl
  }
}

function buildMfaEnrollmentUrl(pathname: string, search?: string): string {
  let redirectTarget = pathname !== '/' ? pathname : undefined

  // If we are on the login page, prefer the returnTo param as the redirect target
  if (pathname.endsWith('/login') && search) {
    const params = new URLSearchParams(search)
    const returnTo = params.get('returnTo')
    // Validate that returnTo is a safe relative path and not an external URL or protocol-relative URL
    if (returnTo?.startsWith('/') && !returnTo.startsWith('//') && !returnTo.includes('://')) {
      redirectTarget = returnTo
    }
  }

  return redirectTarget
    ? `/account/security?redirect=${encodeURIComponent(redirectTarget)}`
    : '/account/security'
}

function getRedirectTarget({
  isLoading,
  user,
  pathname,
  search,
  needsMfaEnrollment,
}: {
  isLoading: boolean
  user: ReturnType<typeof useAuth>['user']
  pathname: string
  search: string
  needsMfaEnrollment: boolean
}): string | null | undefined {
  // Redirect to login with returnTo if user is not authenticated
  if (!(isLoading || user) && pathname !== '/login' && !pathname.startsWith('/auth/')) {
    handleAuthRedirect(pathname)
    return null
  }

  // Redirect to MFA enrollment if needed
  if (needsMfaEnrollment && pathname !== '/account/security') {
    return buildMfaEnrollmentUrl(pathname, search)
  }

  // Redirect root to appropriate landing page
  if (pathname === '/') {
    return needsMfaEnrollment ? '/account/security' : '/rack'
  }
}

function UserSection({
  currentUserHref,
  user,
}: {
  currentUserHref: string | null
  user: ReturnType<typeof useAuth>['user']
}) {
  return (
    <div className="mb-3 flex items-center justify-between gap-2">
      {currentUserHref ? (
        <Link
          className="group block min-w-0 flex-1 rounded-md py-0.5 transition-colors hover:bg-accent/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
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
  )
}

function NavigationItemComponent({ item, pathname }: { item: NavigationItem; pathname: string }) {
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
}

export function Layout() {
  const { user, logout, isLoading } = useAuth()
  const location = useLocation()
  const pathname = useMemo(() => normalizePathname(location.pathname), [location.pathname])
  const [showCliDialog, setShowCliDialog] = useState(false)

  const needsMfaEnrollment = Boolean(user?.mfa_required && !user?.mfa_enrolled)

  const rackAlias = user?.rack?.alias ?? user?.rack?.name ?? 'default'
  const gatewayOrigin = useMemo(() => {
    try {
      return window.location.origin
    } catch {
      return 'https://gateway.example.com'
    }
  }, [])

  const navigation = useMemo<NavigationItem[]>(() => {
    const nav = buildNavigationItems(user?.roles, setShowCliDialog)
    return needsMfaEnrollment ? applyMfaEnrollmentRestrictions(nav) : nav
  }, [needsMfaEnrollment, user?.roles])

  const currentUserHref = useMemo(() => {
    if (!user?.email) {
      return null
    }
    return `/users/${encodeURIComponent(user.email)}`
  }, [user?.email])

  const redirectTarget = getRedirectTarget({
    isLoading,
    user,
    pathname,
    search: location.search,
    needsMfaEnrollment,
  })
  if (redirectTarget === null) {
    return null
  }
  if (redirectTarget) {
    return <Navigate replace to={redirectTarget} />
  }

  return (
    <div className="flex h-screen w-full overflow-hidden bg-background">
      {/* Sidebar */}
      <div className="flex w-64 flex-col border-r bg-card">
        {/* Logo */}
        <div className="flex h-16 shrink-0 items-center px-6">
          <img
            alt=""
            aria-hidden
            className="mr-3 size-7"
            height={32}
            src="/app/logo.svg"
            width={32}
          />
          <h1 className="font-semibold text-xl">Rack Gateway</h1>
        </div>

        <Separator className="shrink-0" />

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto px-3 py-4">
          {navigation.map((item) => (
            <NavigationItemComponent item={item} key={item.name} pathname={pathname} />
          ))}
        </nav>

        <Separator className="shrink-0" />

        {/* User section */}
        <div className="relative shrink-0 bg-card px-5 pt-3 pb-4">
          <div className="-top-[43px] pointer-events-none absolute left-0 h-[42px] w-full bg-gradient-to-t from-card via-card/5 to-transparent" />
          <UserSection currentUserHref={currentUserHref} user={user} />
          {user?.rack && (
            <div className="mb-1 text-muted-foreground text-xs">
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
          <div className="mb-5 text-muted-foreground text-xs">
            <div className="inline-flex items-center">
              <span>Version: v{__APP_VERSION__} ({__COMMIT_HASH__})</span>
            </div>
          </div>
          <Button className="w-full" onClick={logout} size="sm" variant="outline">
            <LogOut className="mr-2 h-4 w-4" />
            Logout
          </Button>
        </div>
      </div>

      {/* Main content */}
      <div className="flex-1 overflow-y-auto">
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
      <CliSetupDialog
        gatewayOrigin={gatewayOrigin}
        onOpenChange={setShowCliDialog}
        open={showCliDialog}
        rackAlias={rackAlias}
      />
    </div>
  )
}
