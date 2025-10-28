import { Link } from '@tanstack/react-router'
import { Lock } from 'lucide-react'
import { TimeAgo } from '@/components/time-ago'
import { Badge } from '@/components/ui/badge'
import { TableCell, TableRow } from '@/components/ui/table'
import { UserMetaCell } from '@/components/user-meta-cell'
import { UserActions } from '@/pages/users/user-actions'
import { AVAILABLE_ROLES, isUserLocked, type User } from '@/pages/users/user-utils'

type UsersTableRowProps = {
  user: User
  isAdmin: boolean
  currentUserEmail?: string
  onEdit: (user: User) => void
  onDelete: (user: User) => void
  onLock: (user: User) => void
  onUnlock: (user: User) => void
  isUnlocking: boolean
}

export function UsersTableRow({
  user,
  isAdmin,
  currentUserEmail,
  onEdit,
  onDelete,
  onLock,
  onUnlock,
  isUnlocking,
}: UsersTableRowProps) {
  const locked = isUserLocked(user)

  return (
    <TableRow key={user.email}>
      <TableCell className={locked ? 'opacity-60' : ''}>
        <div>
          <div className="font-medium">
            <Link
              className="underline hover:no-underline"
              params={{ email: user.email }}
              to="/users/$email"
            >
              {user.name}
            </Link>
            {locked && <Lock className="ml-2 inline h-4 w-4" />}
            {user.email === currentUserEmail && (
              <Badge className="ml-2" variant="outline">
                You
              </Badge>
            )}
          </div>
          <div className="text-muted-foreground text-sm">
            <Link
              className="underline hover:no-underline"
              params={{ email: user.email }}
              to="/users/$email"
            >
              {user.email}
            </Link>
          </div>
        </div>
      </TableCell>
      <TableCell className={locked ? 'opacity-60' : ''}>
        <div className="flex flex-wrap gap-1">
          {user.roles.map((role) => {
            const cfg = AVAILABLE_ROLES[role as keyof typeof AVAILABLE_ROLES]
            return (
              <Badge className={cfg?.className} key={role} variant="default">
                {cfg?.label || role}
              </Badge>
            )
          })}
        </div>
      </TableCell>
      <TableCell className={locked ? 'opacity-60' : ''}>
        {locked ? (
          <Badge variant="destructive">Locked</Badge>
        ) : (
          <Badge variant="default">Active</Badge>
        )}
      </TableCell>
      <TableCell className={locked ? 'opacity-60' : ''}>
        <UserMetaCell
          email={user.created_by_email ?? undefined}
          name={user.created_by_name ?? undefined}
        />
      </TableCell>
      <TableCell className={locked ? 'text-sm opacity-60' : 'text-sm'}>
        <TimeAgo date={user.created_at} />
      </TableCell>
      {isAdmin && (
        <TableCell className="text-right">
          <UserActions
            currentUserEmail={currentUserEmail}
            isUnlocking={isUnlocking}
            onDelete={() => onDelete(user)}
            onEdit={() => onEdit(user)}
            onLock={() => onLock(user)}
            onUnlock={() => onUnlock(user)}
            user={user}
          />
        </TableCell>
      )}
    </TableRow>
  )
}
