import { Link } from '@tanstack/react-router'
import { Edit2, Eye, Lock, MoreVertical, Trash2, Unlock } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { User } from '@/pages/users/user-utils'
import { canModifyUser, isUserLocked } from '@/pages/users/user-utils'

type UserActionsProps = {
  user: User
  currentUserEmail?: string
  isUnlocking: boolean
  onEdit: (user: User) => void
  onLock: (user: User) => void
  onUnlock: (user: User) => void
  onDelete: (user: User) => void
}

export function UserActions({
  user,
  currentUserEmail,
  isUnlocking,
  onEdit,
  onLock,
  onUnlock,
  onDelete,
}: UserActionsProps) {
  const canModify = canModifyUser(user, currentUserEmail)
  const locked = isUserLocked(user)

  return (
    <div className="flex justify-end">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button aria-label={`Actions for ${user.email}`} size="sm" variant="ghost">
            <MoreVertical className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem asChild>
            <Link params={{ email: user.email }} to="/users/$email">
              <Eye className="h-4 w-4" />
              View Details
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => onEdit(user)}>
            <Edit2 className="h-4 w-4" />
            Edit User
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          {locked ? (
            <DropdownMenuItem disabled={isUnlocking} onClick={() => onUnlock(user)}>
              <Unlock className="h-4 w-4" />
              Unlock Account
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem disabled={!canModify} onClick={() => onLock(user)}>
              <Lock className="h-4 w-4" />
              Lock Account
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            disabled={!canModify}
            onClick={() => onDelete(user)}
            variant="destructive"
          >
            <Trash2 className="h-4 w-4" />
            Delete User
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
