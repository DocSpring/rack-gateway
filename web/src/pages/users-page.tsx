import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Edit2, Eye, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from '@/components/ui/use-toast'
import { TablePane } from '../components/table-pane'
import { TimeAgo } from '../components/time-ago'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
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

export function UsersPage() {
  const queryClient = useQueryClient()
  const { user: currentUser } = useAuth()
  const [isDialogOpen, setIsDialogOpen] = useState(false)
  const [dialogMode, setDialogMode] = useState<UserEditDialogMode>('create')
  const [editingUser, setEditingUser] = useState<User | null>(null)

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
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to delete user')
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

  const handleDeleteUser = (email: string) => {
    if (email === currentUser?.email) {
      toast.error("You can't delete your own account")
      return
    }

    if (confirm(`Are you sure you want to delete ${email}?`)) {
      deleteUserMutation.mutate(email)
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
            {rows.map((user) => (
              <TableRow key={user.email}>
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
                  <Badge variant={'default'}>Active</Badge>
                </TableCell>
                <TableCell className="text-sm">
                  {user.created_by_email || user.created_by_name || '-'}
                </TableCell>
                <TableCell className="text-sm">
                  <TimeAgo date={user.created_at} />
                </TableCell>
                {isAdmin && (
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <Button asChild size="sm" variant="outline">
                        <Link params={{ email: user.email }} to="/users/$email">
                          <Eye className="mr-1 h-4 w-4" /> View
                        </Link>
                      </Button>
                      <Button
                        aria-label={`Edit User ${user.email}`}
                        onClick={() => handleEditUser(user)}
                        size="sm"
                        variant="ghost"
                      >
                        <Edit2 className="h-4 w-4" />
                      </Button>
                      <Button
                        aria-label={`Delete User ${user.email}`}
                        disabled={user.email === currentUser?.email}
                        onClick={() => handleDeleteUser(user.email)}
                        size="sm"
                        variant="ghost"
                      >
                        <Trash2 className="h-4 w-4 text-destructive" />
                      </Button>
                    </div>
                  </TableCell>
                )}
              </TableRow>
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
    </div>
  )
}
