import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Edit2, Eye, Lock, MoreVertical, Plus, Trash2, Unlock } from 'lucide-react'
import { useState } from 'react'
import { toast } from '@/components/ui/use-toast'
import { ConfirmDeleteDialog } from '../components/confirm-delete-dialog'
import { TablePane } from '../components/table-pane'
import { TimeAgo } from '../components/time-ago'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../components/ui/dropdown-menu'
import { Label } from '../components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import { Textarea } from '../components/ui/textarea'
import type { UserEditDialogMode, UserEditDialogValues } from '../components/user-edit-dialog'
import { UserEditDialog } from '../components/user-edit-dialog'
import { UserMetaCell } from '../components/user-meta-cell'
import { useAuth } from '../contexts/auth-context'
import { api, type RoleName } from '../lib/api'
import { DEFAULT_PER_PAGE } from '../lib/constants'
import { pickPrimaryRole } from '../lib/user-roles'

type User = {
  id?: number
  email: string
  name: string
  roles: string[]
  created_at: string
  updated_at: string
  suspended: boolean
  created_by_email?: string
  created_by_name?: string
  locked_at?: string
  locked_reason?: string
  locked_by_user_id?: number
}

// CI/CD role is intentionally omitted here; it is reserved for automation tokens only.
const AVAILABLE_ROLES = {
  viewer: {
    label: 'Viewer',
    description: 'Read-only access to apps and logs',
    className: 'bg-zinc-600 text-white',
  },
  ops: {
    label: 'Operations',
    description: 'Can manage processes and access systems',
    className: 'bg-green-600 text-white',
  },
  deployer: {
    label: 'Deployer',
    description: 'Can deploy apps and manage configurations',
    className: 'bg-blue-600 text-white',
  },
  admin: {
    label: 'Administrator',
    description: 'Full access to all resources',
    className: 'bg-purple-600 text-white',
  },
} as const

function isUserLocked(user: User): boolean {
  return !!user.locked_at
}

function canModifyUser(user: User, currentUserEmail?: string): boolean {
  return user.email !== currentUserEmail
}

type UserActionsProps = {
  user: User
  currentUserEmail?: string
  isUnlocking: boolean
  onEdit: (user: User) => void
  onLock: (user: User) => void
  onUnlock: (user: User) => void
  onDelete: (user: User) => void
}

function UserActions({
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

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Component manages user CRUD operations with lock/unlock functionality
export function UsersPage() {
  const queryClient = useQueryClient()
  const { user: currentUser } = useAuth()
  const [isDialogOpen, setIsDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<UserEditDialogMode>('create')
  const [editingUser, setEditingUser] = useState<User | null>(null)
  const [isDeleteOpen, setIsDeleteOpen] = useState(false)
  const [userToDelete, setUserToDelete] = useState<User | null>(null)
  const [isLockDialogOpen, setIsLockDialogOpen] = useState(false)
  const [userToLock, setUserToLock] = useState<User | null>(null)
  const [lockReason, setLockReason] = useState('')

  // Check if current user is admin
  const isAdmin = !!currentUser?.roles?.includes('admin')

  // Fetch users
  const {
    data: users = [],
    isLoading,
    error: queryError,
  } = useQuery({
    queryKey: ['users'],
    queryFn: async () => {
      const response = await api.get<User[]>('/.gateway/api/admin/users')
      return response
    },
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  // Pagination for users list
  const perPage = DEFAULT_PER_PAGE
  const [page, setPage] = useState(1)
  const total = users.length
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const start = (page - 1) * perPage
  const end = Math.min(start + perPage, total)
  const rows = users.slice(start, end)

  // Create user mutation
  const createUserMutation = useMutation({
    mutationFn: async (data: { email: string; name: string; roles: string[] }) => {
      await api.post('/.gateway/api/admin/users', data)
    },
    onSuccess: () => {
      // handled in handleSaveUser to sequence refetch/close deterministically
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to create user')
    },
  })

  // Update user profile (name/email) mutation
  const updateProfileMutation = useMutation({
    mutationFn: async ({
      originalEmail,
      email,
      name,
    }: {
      originalEmail: string
      email: string
      name: string
    }) => {
      await api.put(`/.gateway/api/admin/users/${encodeURIComponent(originalEmail)}`, {
        email,
        name,
      })
    },
  })

  // Update user roles mutation
  const updateRolesMutation = useMutation({
    mutationFn: async ({ email, roles }: { email: string; roles: string[] }) => {
      await api.put(`/.gateway/api/admin/users/${email}/roles`, { roles })
    },
    onSuccess: () => {
      // No-op: combined success handling happens in handleSaveUser
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to update roles')
    },
  })

  // Delete user mutation
  const deleteUserMutation = useMutation({
    mutationFn: async (email: string) => {
      await api.delete(`/.gateway/api/admin/users/${email}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'], refetchType: 'active' })
      toast.success('User deleted successfully')
      handleDeleteDialogOpenChange(false)
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to delete user')
    },
  })

  // Lock user mutation
  const lockUserMutation = useMutation({
    mutationFn: async ({ email, reason }: { email: string; reason: string }) => {
      await api.lockUser(email, reason)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'], refetchType: 'active' })
      toast.success('User account locked successfully')
      setIsLockDialogOpen(false)
      setUserToLock(null)
      setLockReason('')
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to lock user account')
    },
  })

  // Unlock user mutation
  const unlockUserMutation = useMutation({
    mutationFn: async (email: string) => {
      await api.unlockUser(email)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'], refetchType: 'active' })
      toast.success('User account unlocked successfully')
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to unlock user account')
    },
  })

  const handleAddUser = () => {
    setEditingUser(null)
    setDialogMode('create')
    setIsDialogOpen(true)
  }

  const handleEditUser = (user: User) => {
    setEditingUser(user)
    setDialogMode('edit')
    setIsDialogOpen(true)
  }

  const handleDialogOpenChange = (open: boolean) => {
    setIsDialogOpen(open)
    if (!open) {
      setEditingUser(null)
      setDialogMode('create')
    }
  }

  const createUserFlow = async ({ email, name, role }: UserEditDialogValues) => {
    try {
      await createUserMutation.mutateAsync({
        email,
        name,
        roles: [role],
      })
      queryClient.setQueryData<User[] | undefined>(['users'], (prev) => {
        const arr = Array.isArray(prev) ? prev.slice() : []
        arr.push({
          email,
          name,
          roles: [role],
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          suspended: false,
          created_by_email: currentUser?.email,
          created_by_name: currentUser?.name,
        } as User)
        return arr
      })
      await queryClient.invalidateQueries({ queryKey: ['users'], refetchType: 'active' })
      await queryClient.refetchQueries({ queryKey: ['users'] })
      toast.success('User created successfully')
    } catch (err) {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to create user')
      throw err
    }
  }

  const updateExistingUser = async (original: User, values: UserEditDialogValues) => {
    const { email, name, role } = values
    const originalEmail = original.email
    const changedEmail = email !== originalEmail
    const changedName = name !== original.name
    const desiredRoles: RoleName[] = [role]
    const rolesChanged =
      original.roles.length !== desiredRoles.length ||
      desiredRoles.some((r) => !original.roles.includes(r))

    try {
      if (changedEmail || changedName) {
        await updateProfileMutation.mutateAsync({ originalEmail, email, name })
      }
      if (rolesChanged || changedEmail) {
        await updateRolesMutation.mutateAsync({ email, roles: desiredRoles })
      }
      await queryClient.invalidateQueries({ queryKey: ['users'], refetchType: 'active' })
      toast.success('User updated successfully')
    } catch (err) {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to update user')
      throw err
    }
  }

  const handleDialogSubmit = async (values: UserEditDialogValues) => {
    if (dialogMode === 'create') {
      await createUserFlow(values)
      return
    }
    if (!editingUser) {
      return
    }
    await updateExistingUser(editingUser, values)
  }

  const handleRequestDeleteUser = (user: User) => {
    if (!canModifyUser(user, currentUser?.email)) {
      toast.error("You can't delete your own account")
      return
    }
    setUserToDelete(user)
    setIsDeleteOpen(true)
  }

  const handleDeleteDialogOpenChange = (open: boolean) => {
    setIsDeleteOpen(open)
    if (!open) {
      setUserToDelete(null)
    }
  }

  const confirmDeleteUser = async () => {
    if (!userToDelete) {
      return
    }
    try {
      await deleteUserMutation.mutateAsync(userToDelete.email)
    } catch (_err) {
      // Errors are surfaced via mutation onError toast; keep dialog open for retry.
    }
  }

  const handleRequestLockUser = (user: User) => {
    if (!canModifyUser(user, currentUser?.email)) {
      toast.error("You can't lock your own account")
      return
    }
    setUserToLock(user)
    setIsLockDialogOpen(true)
  }

  const handleLockDialogOpenChange = (open: boolean) => {
    setIsLockDialogOpen(open)
    if (!open) {
      setUserToLock(null)
      setLockReason('')
    }
  }

  const confirmLockUser = async () => {
    if (!(userToLock && lockReason.trim())) {
      toast.error('Lock reason is required')
      return
    }
    try {
      await lockUserMutation.mutateAsync({ email: userToLock.email, reason: lockReason })
    } catch (_err) {
      // Errors are surfaced via mutation onError toast; keep dialog open for retry.
    }
  }

  const handleUnlockUser = async (user: User) => {
    try {
      await unlockUserMutation.mutateAsync(user.email)
    } catch (_err) {
      // Error is surfaced via mutation onError toast
    }
  }

  const isEditingExistingUser = dialogMode === 'edit' && editingUser !== null
  const dialogInitialEmail = isEditingExistingUser && editingUser ? editingUser.email : ''
  const dialogInitialName = isEditingExistingUser && editingUser ? editingUser.name : ''
  const dialogInitialRole: RoleName =
    isEditingExistingUser && editingUser ? pickPrimaryRole(editingUser.roles) : 'viewer'

  const dialogBusy =
    dialogMode === 'create'
      ? createUserMutation.isPending
      : updateProfileMutation.isPending || updateRolesMutation.isPending

  // All authenticated users can view this page; actions are gated by role.

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">Users</h1>
        <p className="mt-2 text-muted-foreground">
          Manage user access and permissions for the gateway
        </p>
      </div>

      <TablePane
        description={`${total} ${total === 1 ? 'user' : 'users'} configured`}
        empty={total === 0}
        emptyMessage="No users configured yet"
        error={queryError ? 'Failed to load users' : null}
        headerRight={
          isAdmin ? (
            <Button onClick={handleAddUser}>
              <Plus className="mr-2 h-4 w-4" />
              Add User
            </Button>
          ) : undefined
        }
        loading={!!isLoading}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
              <TableHead>Roles</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Added By</TableHead>
              <TableHead>Created</TableHead>
              {isAdmin && <TableHead className="text-right">Actions</TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((user) => {
              const locked = isUserLocked(user)
              return (
                <TableRow className={locked ? 'opacity-60' : ''} key={user.email}>
                  <TableCell>
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
                        {user.email === currentUser?.email && (
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
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {user.roles.map((role) => {
                        const cfg = AVAILABLE_ROLES[role as keyof typeof AVAILABLE_ROLES]
                        return (
                          <Badge className={cfg?.className} key={role} variant={'default'}>
                            {cfg?.label || role}
                          </Badge>
                        )
                      })}
                    </div>
                  </TableCell>
                  <TableCell>
                    {isUserLocked(user) ? (
                      <Badge variant="destructive">Locked</Badge>
                    ) : (
                      <Badge variant={'default'}>Active</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    <UserMetaCell
                      email={user.created_by_email ?? undefined}
                      name={user.created_by_name ?? undefined}
                    />
                  </TableCell>
                  <TableCell className="text-sm">
                    <TimeAgo date={user.created_at} />
                  </TableCell>
                  {isAdmin && (
                    <TableCell className="text-right">
                      <UserActions
                        currentUserEmail={currentUser?.email}
                        isUnlocking={unlockUserMutation.isPending}
                        onDelete={handleRequestDeleteUser}
                        onEdit={handleEditUser}
                        onLock={handleRequestLockUser}
                        onUnlock={handleUnlockUser}
                        user={user}
                      />
                    </TableCell>
                  )}
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        {total > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">
              Showing {start + 1}–{end} of {total} users
            </div>
            <div className="flex gap-2">
              <Button
                disabled={page === 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                variant="outline"
              >
                Previous
              </Button>
              <Button
                disabled={page === totalPages}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                variant="outline"
              >
                Next
              </Button>
            </div>
          </div>
        )}
      </TablePane>

      <UserEditDialog
        busy={dialogBusy}
        initialEmail={dialogInitialEmail}
        initialName={dialogInitialName}
        initialRole={dialogInitialRole}
        mode={dialogMode}
        onOpenChange={handleDialogOpenChange}
        onSubmit={handleDialogSubmit}
        open={isDialogOpen}
      />

      <ConfirmDeleteDialog
        busy={deleteUserMutation.isPending}
        confirmButtonText="Delete User"
        description={
          <>This action cannot be undone. Type DELETE to remove "{userToDelete?.email}".</>
        }
        inputId="confirm-delete-user"
        onConfirm={confirmDeleteUser}
        onOpenChange={handleDeleteDialogOpenChange}
        open={isDeleteOpen}
        title="Delete User"
      />

      <Dialog onOpenChange={handleLockDialogOpenChange} open={isLockDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Lock User Account</DialogTitle>
            <DialogDescription>
              This will immediately lock "{userToLock?.email}" and revoke all active sessions. The
              user will not be able to log in until unlocked.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <Label htmlFor="lock-reason">Reason for locking (required)</Label>
            <Textarea
              id="lock-reason"
              onChange={(e) => setLockReason(e.target.value)}
              placeholder="e.g., Security incident, policy violation, etc."
              rows={3}
              value={lockReason}
            />
          </div>
          <DialogFooter>
            <Button
              disabled={lockUserMutation.isPending}
              onClick={() => handleLockDialogOpenChange(false)}
              variant="outline"
            >
              Cancel
            </Button>
            <Button
              disabled={lockUserMutation.isPending || !lockReason.trim()}
              onClick={confirmLockUser}
              variant="destructive"
            >
              {lockUserMutation.isPending ? 'Locking Account...' : 'Lock Account'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
