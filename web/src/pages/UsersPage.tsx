import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { UserEditModal } from '../components/UserEditModal'
import { useAuth } from '../contexts/AuthContext'
import type { UserConfig } from '../lib/api'
import { AVAILABLE_ROLES, apiService } from '../lib/api'

export function UsersPage() {
  const queryClient = useQueryClient()
  const { user } = useAuth()
  const [editingUser, setEditingUser] = useState<{ email: string; user: UserConfig } | null>(null)
  const [isAddingUser, setIsAddingUser] = useState(false)

  // Check if current user is admin
  const isAdmin = user?.roles?.includes('admin') || false
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
    mutationFn: ({ email, user }: { email: string; user: UserConfig }) =>
      apiService.saveUser(email, user),
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
    if (!isAdmin) return
    setIsAddingUser(true)
    setEditingUser({ email: '', user: { name: '', roles: [] } })
  }

  const handleEditUser = (email: string, user: UserConfig) => {
    if (!isAdmin) return
    setIsAddingUser(false)
    setEditingUser({ email, user })
  }

  const handleSaveUser = (email: string, user: UserConfig) => {
    if (!isAdmin) return
    saveUserMutation.mutate({ email, user })
  }

  const handleDeleteUser = (email: string) => {
    if (!isAdmin) return
    if (confirm(`Are you sure you want to delete ${email}?`)) {
      deleteUserMutation.mutate(email)
    }
  }

  // Check permissions
  if (!canViewUsers) {
    return (
      <div className="bg-yellow-50 border border-yellow-200 rounded-md p-4">
        <h3 className="text-sm font-medium text-yellow-800">Access Denied</h3>
        <p className="mt-1 text-sm text-yellow-700">
          You don't have permission to view the user management interface.
        </p>
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="flex justify-center py-8">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="bg-red-50 border border-red-200 rounded-md p-4">
        <p className="text-sm text-red-800">Failed to load users: {String(error)}</p>
      </div>
    )
  }

  const users = config?.users || {}

  return (
    <div>
      <div className="sm:flex sm:items-center sm:justify-between mb-6">
        <div>
          <h2 className="text-2xl font-bold text-gray-900">Users</h2>
          {config?.domain && (
            <p className="mt-1 text-sm text-gray-600">
              Domain: <span className="font-medium">{config.domain}</span>
            </p>
          )}
          {!isAdmin && (
            <p className="mt-1 text-sm text-amber-600">
              Read-only access (admin role required for modifications)
            </p>
          )}
        </div>
        {isAdmin && (
          <button
            onClick={handleAddUser}
            className="mt-3 sm:mt-0 inline-flex items-center px-4 py-2 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
          >
            Add User
          </button>
        )}
      </div>

      <div className="bg-white shadow overflow-hidden sm:rounded-md">
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
                <div className="px-4 py-4 sm:px-6 hover:bg-gray-50">
                  <div className="flex items-center justify-between">
                    <div className="flex-1">
                      <div className="flex items-center">
                        <p className="text-sm font-medium text-gray-900">{email}</p>
                        {email === user?.email && (
                          <span className="ml-2 inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-100 text-green-800">
                            You
                          </span>
                        )}
                      </div>
                      <p className="mt-1 text-sm text-gray-600">{userConfig.name}</p>
                      <div className="mt-2 flex flex-wrap gap-1">
                        {userConfig.roles.map((role) => (
                          <span
                            key={role}
                            className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800"
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
                          onClick={() => handleEditUser(email, userConfig)}
                          className="text-blue-600 hover:text-blue-900 text-sm font-medium"
                        >
                          Edit
                        </button>
                        <button
                          onClick={() => handleDeleteUser(email)}
                          className="text-red-600 hover:text-red-900 text-sm font-medium"
                          disabled={email === user?.email}
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
          user={editingUser.user}
          isNew={isAddingUser}
          onSave={handleSaveUser}
          onClose={() => {
            setEditingUser(null)
            setIsAddingUser(false)
          }}
        />
      )}
    </div>
  )
}
