import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Edit2, Eye, Lock, MoreVertical, Plus, Trash2, Unlock } from 'lucide-react'
import { useState } from 'react'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { QUERY_KEYS } from '@/lib/query-keys'
import { ConfirmDeleteDialog } from '../components/confirm-delete-dialog'
import { TablePane } from '../components/table-pane'
import { TimeAgo } from '../components/time-ago'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../components/ui/dropdown-menu'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import type { UserEditDialogMode, UserEditDialogValues } from '../components/user-edit-dialog'
import { UserEditDialog } from '../components/user-edit-dialog'
import { UserLockDialog, useUnlockUser } from '../components/user-lock-dialog'
import { UserMetaCell } from '../components/user-meta-cell'
import { useAuth } from '../contexts/auth-context'
import { api, type RoleName, type UpdateUserRequest } from '../lib/api'
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

type UserUpdatePlan =
  | { type: 'none' }
  | { type: 'nameOnly'; name: string }
  | { type: 'full'; payload: UpdateUserRequest }

function determineUserUpdatePlan(original: User, values: UserEditDialogValues): UserUpdatePlan {
  const desiredRoles: RoleName[] = [values.role]
  const changedEmail = values.email !== original.email
  const changedName = values.name !== original.name
  const rolesChanged =
    original.roles.length !== desiredRoles.length ||
    desiredRoles.some((role) => !original.roles.includes(role))

  if (!changedEmail && changedName && !rolesChanged) {
    return { type: 'nameOnly', name: values.name }
  }

  if (!(changedEmail || changedName || rolesChanged)) {
    return { type: 'none' }
  }

  const payload: UpdateUserRequest = {}
  if (changedEmail) {
    payload.email = values.email
  }
  if (changedName) {
    payload.name = values.name
  }
  if (rolesChanged) {
    payload.roles = desiredRoles
  }

  return { type: 'full', payload }
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

  // Check if current user is admin
  const isAdmin = !!currentUser?.roles?.includes('admin')

  // Fetch users
  const {
    data: users = [],
    isLoading,
    error: queryError,
  } = useQuery({
    queryKey: QUERY_KEYS.USERS,
    queryFn: async () => {
      const response = await api.get<User[]>('/api/v1/users')
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
      await api.post('/api/v1/users', data)
    },
    onSuccess: () => {
      // handled in handleSaveUser to sequence refetch/close deterministically
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to create user')
    },
  })

  // Update user mutation (email/name/roles)
  const updateUserMutation = useMutation({
    mutationFn: async ({
      originalEmail,
      payload,
    }: {
      originalEmail: string
      payload: UpdateUserRequest
    }) => {
      await api.updateUser(originalEmail, payload)
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to update user')
    },
  })

  // Update user name mutation (step-up only)
  const updateUserNameMutation = useMutation({
    mutationFn: async ({ email, name }: { email: string; name: string }) => {
      await api.updateUserName(email, { name })
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to update user name')
    },
  })

  // Delete user mutation
  const deleteUserMutation = useMutation({
    mutationFn: async (email: string) => {
      await api.delete(`/api/v1/users/${email}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: QUERY_KEYS.USERS,
        refetchType: 'active',
      })
      toast.success('User deleted successfully')
      handleDeleteDialogOpenChange(false)
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to delete user')
    },
  })

  const unlockUserMutation = useUnlockUser()

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
      await queryClient.invalidateQueries({
        queryKey: QUERY_KEYS.USERS,
        refetchType: 'active',
      })
      await queryClient.refetchQueries({ queryKey: QUERY_KEYS.USERS })
      toast.success('User created successfully')
    } catch (err) {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to create user')
      throw err
    }
  }

  const updateExistingUser = async (original: User, values: UserEditDialogValues) => {
    const plan = determineUserUpdatePlan(original, values)
    if (plan.type === 'none') {
      return
    }

    try {
      if (plan.type === 'nameOnly') {
        await updateUserNameMutation.mutateAsync({ email: original.email, name: plan.name })
      } else {
        await updateUserMutation.mutateAsync({
          originalEmail: original.email,
          payload: plan.payload,
        })
      }
      await queryClient.invalidateQueries({
        queryKey: QUERY_KEYS.USERS,
        refetchType: 'active',
      })
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
      : updateUserMutation.isPending || updateUserNameMutation.isPending

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
            {/* biome-ignore lint/complexity/noExcessiveCognitiveComplexity: no need to extract this */}
            {rows.map((user) => {
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
                  <TableCell className={locked ? 'opacity-60' : ''}>
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
                  <TableCell className={locked ? 'opacity-60' : ''}>
                    {isUserLocked(user) ? (
                      <Badge variant="destructive">Locked</Badge>
                    ) : (
                      <Badge variant={'default'}>Active</Badge>
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

      {userToLock && (
        <UserLockDialog
          onOpenChange={handleLockDialogOpenChange}
          open={isLockDialogOpen}
          userEmail={userToLock.email}
        />
      )}
    </div>
  )
}
