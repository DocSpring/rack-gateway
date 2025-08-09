import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { UserEditModal } from '../components/user-edit-modal'
import { useAuth } from '../contexts/auth-context'
import type { UserConfig } from '../lib/api'
import { AVAILABLE_ROLES, apiService } from '../lib/api'

export function UsersPage() {
  const queryClient = useQueryClient()
  const { user } = useAuth()
  const [editingUser, setEditingUser] = useState<{ email: string; user: UserConfig } | null>(null)
  const [isAddingUser, setIsAddingUser] = useState(false)

  // Check if current user is admin
  const isAdmin = user?.roles?.includes('admin')
  const canViewUsers = isAdmin || user?.roles?.includes('ops') || user?.roles?.includes('deployer')

  // Fetch config
  const {
    data: config,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['config'],
    queryFn: () => apiService.getConfig(),
    enabled: canViewUsers, // Only fetch if user has permission
  })

  // Save user mutation
  const saveUserMutation = useMutation({
    mutationFn: ({ email, user: userConfig }: { email: string; user: UserConfig }) =>
      apiService.saveUser(email, userConfig),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
      setEditingUser(null)
      setIsAddingUser(false)
    },
  })

  // Delete user mutation
  const deleteUserMutation = useMutation({
    mutationFn: (email: string) => apiService.deleteUser(email),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
    },
  })

  const handleAddUser = () => {
    if (!isAdmin) {
      return
    }
    setIsAddingUser(true)
    setEditingUser({ email: '', user: { name: '', roles: [] } })
  }

  const handleEditUser = (email: string, userConfig: UserConfig) => {
    if (!isAdmin) {
      return
    }
    setIsAddingUser(false)
    setEditingUser({ email, user: userConfig })
  }

  const handleSaveUser = (email: string, userConfig: UserConfig) => {
    if (!isAdmin) {
      return
    }
    saveUserMutation.mutate({ email, user: userConfig })
  }

  const handleDeleteUser = (email: string) => {
    if (!isAdmin) {
      return
    }
    if (confirm(`Are you sure you want to delete ${email}?`)) {
      deleteUserMutation.mutate(email)
    }
  }

  // Check permissions
  if (!canViewUsers) {
    return (
      <div className="rounded-md border border-yellow-200 bg-yellow-50 p-4">
        <h3 className="font-medium text-sm text-yellow-800">Access Denied</h3>
        <p className="mt-1 text-sm text-yellow-700">
          You don't have permission to view the user management interface.
        </p>
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="flex justify-center py-8">
        <div className="h-8 w-8 animate-spin rounded-full border-blue-600 border-b-2" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="rounded-md border border-red-200 bg-red-50 p-4">
        <p className="text-red-800 text-sm">Failed to load users: {String(error)}</p>
      </div>
    )
  }

  const users = config?.users || {}

  return (
    <div>
      <div className="mb-6 sm:flex sm:items-center sm:justify-between">
        <div>
          <h2 className="font-bold text-2xl text-gray-900">Users</h2>
          {config?.domain && (
            <p className="mt-1 text-gray-600 text-sm">
              Domain: <span className="font-medium">{config.domain}</span>
            </p>
          )}
          {!isAdmin && (
            <p className="mt-1 text-amber-600 text-sm">
              Read-only access (admin role required for modifications)
            </p>
          )}
        </div>
        {isAdmin && (
          <button
            className="mt-3 inline-flex items-center rounded-md border border-transparent bg-blue-600 px-4 py-2 font-medium text-sm text-white shadow-sm hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 sm:mt-0"
            onClick={handleAddUser}
            type="button"
          >
            Add User
          </button>
        )}
      </div>

      <div className="overflow-hidden bg-white shadow sm:rounded-md">
        {Object.keys(users).length === 0 ? (
          <div className="px-4 py-12 text-center text-gray-500">
            {isAdmin
              ? 'No users configured. Click "Add User" to get started.'
              : 'No users configured.'}
          </div>
        ) : (
          <ul className="divide-y divide-gray-200">
            {Object.entries(users).map(([email, userConfig]) => (
              <li key={email}>
                <div className="px-4 py-4 hover:bg-gray-50 sm:px-6">
                  <div className="flex items-center justify-between">
                    <div className="flex-1">
                      <div className="flex items-center">
                        <p className="font-medium text-gray-900 text-sm">{email}</p>
                        {email === user?.email && (
                          <span className="ml-2 inline-flex items-center rounded bg-green-100 px-2 py-0.5 font-medium text-green-800 text-xs">
                            You
                          </span>
                        )}
                      </div>
                      <p className="mt-1 text-gray-600 text-sm">{userConfig.name}</p>
                      <div className="mt-2 flex flex-wrap gap-1">
                        {userConfig.roles.map((role) => (
                          <span
                            className="inline-flex items-center rounded-full bg-blue-100 px-2.5 py-0.5 font-medium text-blue-800 text-xs"
                            key={role}
                            title={
                              AVAILABLE_ROLES[role as keyof typeof AVAILABLE_ROLES]?.description
                            }
                          >
                            {role}
                          </span>
                        ))}
                      </div>
                    </div>
                    {isAdmin && (
                      <div className="flex items-center space-x-2">
                        <button
                          className="font-medium text-blue-600 text-sm hover:text-blue-900"
                          onClick={() => handleEditUser(email, userConfig)}
                          type="button"
                        >
                          Edit
                        </button>
                        <button
                          className="font-medium text-red-600 text-sm hover:text-red-900"
                          disabled={email === user?.email}
                          onClick={() => handleDeleteUser(email)}
                          type="button"
                        >
                          Delete
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      {editingUser && isAdmin && (
        <UserEditModal
          email={editingUser.email}
          isNew={isAddingUser}
          onClose={() => {
            setEditingUser(null)
            setIsAddingUser(false)
          }}
          onSave={handleSaveUser}
          user={editingUser.user}
        />
      )}
    </div>
  )
}
