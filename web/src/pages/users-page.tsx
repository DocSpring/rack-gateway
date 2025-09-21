import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Edit2, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
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
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import { useAuth } from '../contexts/auth-context'
import { api } from '../lib/api'
import { DEFAULT_PER_PAGE } from '../lib/constants'

interface User {
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
  const [editingUser, setEditingUser] = useState<User | null>(null)
  const [formData, setFormData] = useState({
    email: '',
    name: '',
  })
  const [selectedRole, setSelectedRole] = useState<string>('viewer')

  const rolePrecedence = ['admin', 'deployer', 'ops', 'viewer'] as const
  const pickHighestRole = (roles: string[] = []) => {
    for (const r of rolePrecedence) {
      if (roles.includes(r)) {
        return r
      }
    }
    return 'viewer'
  }

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
    setFormData({ email: '', name: '' })
    setSelectedRole('viewer')
    setIsDialogOpen(true)
  }

  const handleEditUser = (user: User) => {
    setEditingUser(user)
    setFormData({
      email: user.email,
      name: user.name,
    })
    setSelectedRole(pickHighestRole(user.roles))
    setIsDialogOpen(true)
  }

  const handleCloseDialog = () => {
    setIsDialogOpen(false)
    setEditingUser(null)
    setFormData({ email: '', name: '' })
    setSelectedRole('viewer')
  }

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    handleSaveUser()
  }

  // Smaller helpers to keep complexity down
  const finalizeUpdate = () => {
    queryClient.invalidateQueries({ queryKey: ['users'], refetchType: 'active' })
    toast.success('User updated successfully')
    handleCloseDialog()
  }

  const updateRolesOnly = () =>
    updateRolesMutation
      .mutateAsync({ email: formData.email, roles: [selectedRole] })
      .then(finalizeUpdate)
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : ''
        toast.error(message || 'Failed to update user')
      })

  const updateNameThenRoles = (originalEmail: string) =>
    updateProfileMutation
      .mutateAsync({ originalEmail, email: formData.email, name: formData.name })
      .then(() => updateRolesMutation.mutateAsync({ email: formData.email, roles: [selectedRole] }))
      .then(finalizeUpdate)
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : ''
        toast.error(message || 'Failed to update user')
      })

  const changeEmailFlow = (originalEmail: string) =>
    createUserMutation
      .mutateAsync({ email: formData.email, name: formData.name, roles: [selectedRole] })
      .then(() => deleteUserMutation.mutateAsync(originalEmail))
      .then(finalizeUpdate)
      .catch((err: unknown) => {
        const message = err instanceof Error ? err.message : ''
        toast.error(message || 'Failed to update user email')
      })

  const createUserFlow = async () => {
    try {
      await createUserMutation.mutateAsync({
        email: formData.email,
        name: formData.name,
        roles: [selectedRole],
      })
      // Close dialog early to avoid overlay hiding the table
      handleCloseDialog()
      // Optimistically add to cache for immediate visibility
      queryClient.setQueryData<User[] | undefined>(['users'], (prev) => {
        const arr = Array.isArray(prev) ? prev.slice() : []
        arr.push({
          email: formData.email,
          name: formData.name,
          roles: [selectedRole],
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
    }
  }

  const handleSaveUser = async () => {
    if (!(formData.email && formData.name && selectedRole)) {
      toast.error('Please fill in all fields')
      return
    }

    if (!editingUser) {
      await createUserFlow()
      return
    }

    const originalEmail = editingUser.email
    const changedEmail = formData.email !== originalEmail
    const changedName = formData.name !== editingUser.name

    if (changedEmail) {
      await changeEmailFlow(originalEmail)
      return
    }
    if (changedName) {
      await updateNameThenRoles(originalEmail)
      return
    }
    await updateRolesOnly()
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

  // Single role only; radios control selectedRole

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
                      {user.id ? (
                        <Link
                          className="underline hover:no-underline"
                          params={{ id: String(user.id) }}
                          to="/users/$id/audit_logs"
                        >
                          {user.name}
                        </Link>
                      ) : (
                        user.name
                      )}
                      {user.email === currentUser?.email && (
                        <Badge className="ml-2" variant="outline">
                          You
                        </Badge>
                      )}
                    </div>
                    <div className="text-muted-foreground text-sm">
                      {user.id ? (
                        <Link
                          className="underline hover:no-underline"
                          params={{ id: String(user.id) }}
                          to="/users/$id/audit_logs"
                        >
                          {user.email}
                        </Link>
                      ) : (
                        user.email
                      )}
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

      {/* User Dialog */}
      <Dialog onOpenChange={setIsDialogOpen} open={isDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingUser ? 'Edit User' : 'Add User'}</DialogTitle>
            <DialogDescription>
              {editingUser ? 'Update user roles and permissions' : 'Add a new user to the gateway'}
            </DialogDescription>
          </DialogHeader>

          <form className="space-y-4" noValidate={false} onSubmit={handleSubmit}>
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                autoCapitalize="none"
                autoComplete="email"
                autoCorrect="off"
                data-1p-ignore
                data-bwignore="true"
                data-lpignore="true"
                disabled={!isAdmin}
                id="email"
                inputMode="email"
                name="email"
                onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                placeholder="user@example.com"
                required
                spellCheck={false}
                type="email"
                value={formData.email}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="name">Name</Label>
              <Input
                autoCapitalize="none"
                autoComplete="off"
                autoCorrect="off"
                data-1p-ignore
                data-bwignore="true"
                data-lpignore="true"
                disabled={!isAdmin}
                id="name"
                inputMode="text"
                name="user_name"
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                placeholder="John Doe"
                required
                spellCheck={false}
                value={formData.name}
              />
            </div>

            <div className="space-y-2">
              <Label>Role</Label>
              <div className="space-y-2">
                {Object.entries(AVAILABLE_ROLES).map(([role, config]) => (
                  <label
                    className={`flex cursor-pointer items-center justify-between rounded-lg border p-3 transition-colors ${
                      selectedRole === role ? 'border-primary bg-primary/10' : 'hover:bg-accent'
                    }`}
                    key={role}
                  >
                    <div className="flex items-start gap-3">
                      <input
                        checked={selectedRole === role}
                        className="mt-1 h-4 w-4"
                        name="user_role"
                        onChange={() => setSelectedRole(role)}
                        type="radio"
                        value={role}
                      />
                      <div>
                        <div className="font-medium">{config.label}</div>
                        <div className="text-muted-foreground text-sm">{config.description}</div>
                      </div>
                    </div>
                  </label>
                ))}
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCloseDialog} type="button" variant="outline">
                Cancel
              </Button>
              <Button
                disabled={
                  createUserMutation.isPending ||
                  updateRolesMutation.isPending ||
                  updateProfileMutation.isPending
                }
                type="submit"
              >
                {(() => {
                  if (
                    createUserMutation.isPending ||
                    updateRolesMutation.isPending ||
                    updateProfileMutation.isPending
                  ) {
                    return 'Saving...'
                  }
                  return editingUser ? 'Update User' : 'Add User'
                })()}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
