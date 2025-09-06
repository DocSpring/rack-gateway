import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Edit2, Plus, RefreshCw, Trash2, UserCheck, UserX } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
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

interface User {
  email: string
  name: string
  roles: string[]
  created_at: string
  updated_at: string
  suspended: boolean
}

const AVAILABLE_ROLES = {
  viewer: {
    label: 'Viewer',
    description: 'Read-only access to apps and logs',
    color: 'default',
  },
  ops: {
    label: 'Operations',
    description: 'Can manage processes and access systems',
    color: 'secondary',
  },
  deployer: {
    label: 'Deployer',
    description: 'Can deploy apps and manage configurations',
    color: 'outline',
  },
  admin: {
    label: 'Administrator',
    description: 'Full access to all resources',
    color: 'destructive',
  },
}

export function UsersPage() {
  const queryClient = useQueryClient()
  const { user: currentUser } = useAuth()
  const [isDialogOpen, setIsDialogOpen] = useState(false)
  const [editingUser, setEditingUser] = useState<User | null>(null)
  const [formData, setFormData] = useState({
    email: '',
    name: '',
    roles: [] as string[],
  })

  // Check if current user is admin
  const isAdmin = currentUser?.roles?.includes('admin')

  // Fetch users
  const {
    data: users = [],
    isLoading,
    error: queryError,
  } = useQuery({
    queryKey: ['users'],
    queryFn: async () => {
      const response = await api.get<User[]>('/.gateway/admin/users')
      return response
    },
  })

  // Create user mutation
  const createUserMutation = useMutation({
    mutationFn: async (data: { email: string; name: string; roles: string[] }) => {
      await api.post('/.gateway/admin/users', data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      toast.success('User created successfully')
      handleCloseDialog()
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to create user')
    },
  })

  // Update user roles mutation
  const updateRolesMutation = useMutation({
    mutationFn: async ({ email, roles }: { email: string; roles: string[] }) => {
      await api.put(`/.gateway/admin/users/${email}/roles`, { roles })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      toast.success('User roles updated successfully')
      handleCloseDialog()
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to update roles')
    },
  })

  // Suspend/unsuspend user mutation
  const suspendUserMutation = useMutation({
    mutationFn: async ({ email, suspended }: { email: string; suspended: boolean }) => {
      await api.put(`/.gateway/admin/users/${email}/suspend`, { suspended })
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      toast.success(`User ${variables.suspended ? 'suspended' : 'unsuspended'} successfully`)
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to update user status')
    },
  })

  // Delete user mutation
  const deleteUserMutation = useMutation({
    mutationFn: async (email: string) => {
      await api.delete(`/.gateway/admin/users/${email}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      toast.success('User deleted successfully')
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to delete user')
    },
  })

  const handleAddUser = () => {
    setEditingUser(null)
    setFormData({ email: '', name: '', roles: ['viewer'] })
    setIsDialogOpen(true)
  }

  const handleEditUser = (user: User) => {
    setEditingUser(user)
    setFormData({
      email: user.email,
      name: user.name,
      roles: user.roles,
    })
    setIsDialogOpen(true)
  }

  const handleCloseDialog = () => {
    setIsDialogOpen(false)
    setEditingUser(null)
    setFormData({ email: '', name: '', roles: [] })
  }

  const handleSaveUser = () => {
    if (!(formData.email && formData.name) || formData.roles.length === 0) {
      toast.error('Please fill in all fields')
      return
    }

    if (editingUser) {
      updateRolesMutation.mutate({ email: formData.email, roles: formData.roles })
    } else {
      createUserMutation.mutate(formData)
    }
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

  const toggleRoleSelection = (role: string) => {
    setFormData((prev) => ({
      ...prev,
      roles: prev.roles.includes(role)
        ? prev.roles.filter((r) => r !== role)
        : [...prev.roles, role],
    }))
  }

  if (!isAdmin) {
    return (
      <div className="p-8">
        <Card>
          <CardHeader>
            <CardTitle className="text-destructive">Access Denied</CardTitle>
            <CardDescription>
              You don't have permission to view the user management interface. Admin role is
              required.
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="p-8">
        <div className="flex h-64 items-center justify-center">
          <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </div>
    )
  }

  if (queryError) {
    return (
      <div className="p-8">
        <Card>
          <CardHeader>
            <CardTitle className="text-destructive">Error</CardTitle>
            <CardDescription>Failed to load users</CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">Users</h1>
        <p className="mt-2 text-muted-foreground">
          Manage user access and permissions for the gateway
        </p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>Users</CardTitle>
              <CardDescription>
                {users.length} {users.length === 1 ? 'user' : 'users'} configured
              </CardDescription>
            </div>
            <Button onClick={handleAddUser}>
              <Plus className="mr-2 h-4 w-4" />
              Add User
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {users.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">No users configured yet</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>User</TableHead>
                  <TableHead>Roles</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>Updated</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.map((user) => (
                  <TableRow key={user.email}>
                    <TableCell>
                      <div>
                        <div className="font-medium">
                          {user.name}
                          {user.email === currentUser?.email && (
                            <Badge className="ml-2" variant="outline">
                              You
                            </Badge>
                          )}
                        </div>
                        <div className="text-muted-foreground text-sm">{user.email}</div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {user.roles.map((role) => (
                          <Badge
                            key={role}
                            variant={
                              AVAILABLE_ROLES[role as keyof typeof AVAILABLE_ROLES]?.color as
                                | 'default'
                                | 'secondary'
                                | 'outline'
                                | 'destructive'
                            }
                          >
                            {AVAILABLE_ROLES[role as keyof typeof AVAILABLE_ROLES]?.label || role}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={user.suspended ? 'destructive' : 'default'}>
                        {user.suspended ? 'Suspended' : 'Active'}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm">
                      {(() => {
                        const d = user.created_at ? new Date(user.created_at) : null
                        return d && !Number.isNaN(d.getTime()) ? format(d, 'MMM d, yyyy') : '-'
                      })()}
                    </TableCell>
                    <TableCell className="text-sm">
                      {(() => {
                        const d = user.updated_at ? new Date(user.updated_at) : null
                        return d && !Number.isNaN(d.getTime()) ? format(d, 'MMM d, yyyy') : '-'
                      })()}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button onClick={() => handleEditUser(user)} size="sm" variant="ghost">
                          <Edit2 className="h-4 w-4" />
                        </Button>
                        <Button
                          disabled={user.email === currentUser?.email}
                          onClick={() =>
                            suspendUserMutation.mutate({
                              email: user.email,
                              suspended: !user.suspended,
                            })
                          }
                          size="sm"
                          variant="ghost"
                        >
                          {user.suspended ? (
                            <UserCheck className="h-4 w-4 text-green-600" />
                          ) : (
                            <UserX className="h-4 w-4 text-orange-600" />
                          )}
                        </Button>
                        <Button
                          disabled={user.email === currentUser?.email}
                          onClick={() => handleDeleteUser(user.email)}
                          size="sm"
                          variant="ghost"
                        >
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* User Dialog */}
      <Dialog onOpenChange={setIsDialogOpen} open={isDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingUser ? 'Edit User' : 'Add User'}</DialogTitle>
            <DialogDescription>
              {editingUser ? 'Update user roles and permissions' : 'Add a new user to the gateway'}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                autoCapitalize="none"
                autoComplete="off"
                autoCorrect="off"
                data-1p-ignore
                data-bwignore="true"
                data-lpignore="true"
                disabled={!!editingUser}
                id="email"
                inputMode="email"
                name="user_email"
                onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                placeholder="user@example.com"
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
                disabled={!!editingUser}
                id="name"
                inputMode="text"
                name="user_name"
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                placeholder="John Doe"
                spellCheck={false}
                value={formData.name}
              />
            </div>

            <div className="space-y-2">
              <Label>Roles</Label>
              <div className="space-y-2">
                {Object.entries(AVAILABLE_ROLES).map(([role, config]) => (
                  <button
                    className={`flex cursor-pointer items-center justify-between rounded-lg border p-3 transition-colors ${
                      formData.roles.includes(role)
                        ? 'border-primary bg-primary/10'
                        : 'hover:bg-accent'
                    }`}
                    key={role}
                    onClick={() => toggleRoleSelection(role)}
                    type="button"
                  >
                    <div>
                      <div className="font-medium">{config.label}</div>
                      <div className="text-muted-foreground text-sm">{config.description}</div>
                    </div>
                    <input
                      checked={formData.roles.includes(role)}
                      className="h-4 w-4"
                      onChange={() => toggleRoleSelection(role)}
                      type="checkbox"
                    />
                  </button>
                ))}
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button onClick={handleCloseDialog} variant="outline">
              Cancel
            </Button>
            <Button
              disabled={createUserMutation.isPending || updateRolesMutation.isPending}
              onClick={handleSaveUser}
            >
              {(() => {
                if (createUserMutation.isPending || updateRolesMutation.isPending) {
                  return 'Saving...'
                }
                return editingUser ? 'Update User' : 'Add User'
              })()}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
