import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog'
import { TablePane } from '@/components/table-pane'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { toast } from '@/components/ui/use-toast'
import type { UserEditDialogMode, UserEditDialogValues } from '@/components/user-edit-dialog'
import { UserEditDialog } from '@/components/user-edit-dialog'
import { UserLockDialog, useUnlockUser } from '@/components/user-lock-dialog'
import { useAuth } from '@/contexts/auth-context'
import { useMutation } from '@/hooks/use-mutation'
import { api, type RoleName, type UpdateUserRequest } from '@/lib/api'
import { DEFAULT_PER_PAGE } from '@/lib/constants'
import { QUERY_KEYS } from '@/lib/query-keys'
import { pickPrimaryRole } from '@/lib/user-roles'
import { canModifyUser, determineUserUpdatePlan, type User } from '@/pages/users/user-utils'
import { UsersTableRow } from '@/pages/users/users-table-row'

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
  const isAdmin = Boolean(currentUser?.roles?.includes('admin'))

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
        loading={Boolean(isLoading)}
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
            {rows.map((user) => (
              <UsersTableRow
                currentUserEmail={currentUser?.email ?? undefined}
                isAdmin={isAdmin}
                isUnlocking={unlockUserMutation.isPending}
                key={user.email}
                onDelete={handleRequestDeleteUser}
                onEdit={handleEditUser}
                onLock={handleRequestLockUser}
                onUnlock={handleUnlockUser}
                user={user}
              />
            ))}
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
