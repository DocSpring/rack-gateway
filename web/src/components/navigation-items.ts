import {
  Blocks,
  Boxes,
  Cpu,
  Hammer,
  HardDrive,
  Key,
  ListChecks,
  Lock,
  Logs,
  type LucideIcon,
  Puzzle,
  Server,
  ServerCog,
  Settings,
  TerminalSquare,
  Users,
} from 'lucide-react'
import { hasPermission, PERMISSIONS } from '../lib/permissions'

export type NavigationItem = {
  name: string
  icon: LucideIcon
  href?: string
  onSelect?: () => void
  disabled?: boolean
}

type NavigationUser = {
  email?: string
  roles?: string[]
  permissions?: string[]
}

// Activity: the full audit log for people who may read it, otherwise the user's own activity.
function activityNavigationItem(user: NavigationUser | null | undefined): NavigationItem | null {
  if (hasPermission(user?.permissions, PERMISSIONS.auditLogRead)) {
    return { name: 'Audit Logs', href: '/audit-logs', icon: Logs }
  }
  if (!user?.email) {
    return null
  }
  return {
    name: 'My Activity',
    href: `/users/${encodeURIComponent(user.email)}/audit-logs`,
    icon: Logs,
  }
}

export function buildNavigationItems(
  user: NavigationUser | null | undefined,
  setShowCliDialog: (show: boolean) => void
): NavigationItem[] {
  const can = (permission: string) => hasPermission(user?.permissions, permission)
  const userRoles = user?.roles
  const nav: NavigationItem[] = [
    { name: 'Rack', href: '/rack', icon: Server },
    { name: 'Apps', href: '/apps', icon: Boxes },
    { name: 'Processes', href: '/processes', icon: Cpu },
    { name: 'Instances', href: '/instances', icon: HardDrive },
    { name: 'Builds', href: '/builds', icon: Hammer },
    { name: 'Releases', href: '/releases', icon: Blocks },
  ]

  if (can(PERMISSIONS.userList)) {
    nav.push({ name: 'Users', href: '/users', icon: Users })
  }
  if (can(PERMISSIONS.apiTokenRead)) {
    nav.push({ name: 'API Tokens', href: '/api-tokens', icon: Key })
  }
  if (can(PERMISSIONS.deployApprovalList)) {
    nav.push({
      name: 'Deploy Approvals',
      href: '/deploy-approval-requests',
      icon: ListChecks,
    })
  }

  const activity = activityNavigationItem(user)
  if (activity) {
    nav.push(activity)
  }
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
