import { Edit2, Lock, RefreshCw, Trash2, Unlock } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { GatewayUser } from '@/lib/api'
import { RoleBadges } from '@/pages/user/role-badges'

type UserHeaderProps = {
  user: GatewayUser | undefined
  userLoading: boolean
  decodedEmail: string
  onEdit: () => void
  isEditBusy: boolean
  onUnlock: () => Promise<unknown>
  unlockPending: boolean
  locked: boolean
  onLock: () => void
  onSignOutEverywhere: () => void
  signOutPending: boolean
  hasSessions: boolean
  onDelete: () => void
  deletePending: boolean
}

export function UserHeaderSection({
  user,
  userLoading,
  decodedEmail,
  onEdit,
  isEditBusy,
  onUnlock,
  unlockPending,
  locked,
  onLock,
  onSignOutEverywhere,
  signOutPending,
  hasSessions,
  onDelete,
  deletePending,
}: UserHeaderProps) {
  const handleUnlockClick = () => {
    onUnlock().catch(() => {
      /* handled within hook */
    })
  }

  return (
    <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
      <div>
        <h1 className="font-semibold text-3xl">
          {userLoading ? 'Loading…' : user?.name || decodedEmail}
        </h1>
        <p className="text-muted-foreground" data-testid="user-email">
          {decodedEmail}
        </p>
        {user?.roles && user.roles.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-2">
            <RoleBadges roles={user.roles ?? []} />
          </div>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2 md:justify-end">
        <Button disabled={userLoading || !user || isEditBusy} onClick={onEdit} variant="secondary">
          <Edit2 className="mr-2 h-4 w-4" /> Edit
        </Button>
        {locked ? (
          <Button
            disabled={unlockPending || userLoading}
            onClick={handleUnlockClick}
            variant="secondary"
          >
            {unlockPending ? (
              <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
            ) : (
              <Unlock className="mr-2 h-4 w-4" />
            )}
            Unlock Account
          </Button>
        ) : (
          <Button disabled={userLoading || !user} onClick={onLock} variant="secondary">
            <Lock className="mr-2 h-4 w-4" />
            Lock Account
          </Button>
        )}
        <Button
          disabled={signOutPending || userLoading || !hasSessions}
          onClick={onSignOutEverywhere}
          variant="destructive"
        >
          {signOutPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
          Sign Out Everywhere
        </Button>
        <Button
          disabled={userLoading || !user || deletePending}
          onClick={onDelete}
          variant="destructive"
        >
          {deletePending ? (
            <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
          ) : (
            <Trash2 className="mr-2 h-4 w-4" />
          )}
          Delete User
        </Button>
      </div>
    </div>
  )
}
