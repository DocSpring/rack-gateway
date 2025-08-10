import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Plus, Edit2, Trash2, RefreshCw, UserCheck, UserX } from 'lucide-react'
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import { Badge } from '../components/ui/badge'
import { toast } from 'sonner'
import { api } from '../lib/api'
import { useAuth } from '../contexts/auth-context'

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
  const { data: users = [], isLoading, error, refetch } = useQuery({
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
    onError: (error: any) => {
      toast.error(error.message || 'Failed to create user')
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
    onError: (error: any) => {
      toast.error(error.message || 'Failed to update roles')
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
    onError: (error: any) => {
      toast.error(error.message || 'Failed to update user status')
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
    onError: (error: any) => {
      toast.error(error.message || 'Failed to delete user')
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
    if (!formData.email || !formData.name || formData.roles.length === 0) {
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
    setFormData(prev => ({
      ...prev,
      roles: prev.roles.includes(role)
        ? prev.roles.filter(r => r !== role)
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
              You don't have permission to view the user management interface.
              Admin role is required.
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="p-8">
        <div className="flex items-center justify-center h-64">
          <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      </div>
    )
  }

  if (error) {
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
        <h1 className="text-3xl font-bold">Users</h1>
        <p className="text-muted-foreground mt-2">
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
            <div className="text-center py-8 text-muted-foreground">
              No users configured yet
            </div>
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
                            <Badge variant="outline" className="ml-2">You</Badge>
                          )}
                        </div>
                        <div className="text-sm text-muted-foreground">{user.email}</div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {user.roles.map((role) => (
                          <Badge
                            key={role}
                            variant={AVAILABLE_ROLES[role as keyof typeof AVAILABLE_ROLES]?.color as any}
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
                      {format(new Date(user.created_at), 'MMM d, yyyy')}
                    </TableCell>
                    <TableCell className="text-sm">
                      {format(new Date(user.updated_at), 'MMM d, yyyy')}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleEditUser(user)}
                        >
                          <Edit2 className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => suspendUserMutation.mutate({
                            email: user.email,
                            suspended: !user.suspended,
                          })}
                          disabled={user.email === currentUser?.email}
                        >
                          {user.suspended ? (
                            <UserCheck className="h-4 w-4 text-green-600" />
                          ) : (
                            <UserX className="h-4 w-4 text-orange-600" />
                          )}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteUser(user.email)}
                          disabled={user.email === currentUser?.email}
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
      <Dialog open={isDialogOpen} onOpenChange={setIsDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingUser ? 'Edit User' : 'Add User'}</DialogTitle>
            <DialogDescription>
              {editingUser
                ? 'Update user roles and permissions'
                : 'Add a new user to the gateway'}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                placeholder="user@example.com"
                value={formData.email}
                onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                disabled={!!editingUser}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="name">Name</Label>
              <Input
                id="name"
                placeholder="John Doe"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                disabled={!!editingUser}
              />
            </div>

            <div className="space-y-2">
              <Label>Roles</Label>
              <div className="space-y-2">
                {Object.entries(AVAILABLE_ROLES).map(([role, config]) => (
                  <div
                    key={role}
                    className={`flex items-center justify-between p-3 rounded-lg border cursor-pointer transition-colors ${
                      formData.roles.includes(role)
                        ? 'bg-primary/10 border-primary'
                        : 'hover:bg-accent'
                    }`}
                    onClick={() => toggleRoleSelection(role)}
                  >
                    <div>
                      <div className="font-medium">{config.label}</div>
                      <div className="text-sm text-muted-foreground">
                        {config.description}
                      </div>
                    </div>
                    <input
                      type="checkbox"
                      checked={formData.roles.includes(role)}
                      onChange={() => {}}
                      className="h-4 w-4"
                    />
                  </div>
                ))}
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={handleCloseDialog}>
              Cancel
            </Button>
            <Button
              onClick={handleSaveUser}
              disabled={createUserMutation.isPending || updateRolesMutation.isPending}
            >
              {createUserMutation.isPending || updateRolesMutation.isPending
                ? 'Saving...'
                : editingUser
                ? 'Update User'
                : 'Add User'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}