import { useEffect, useState } from 'react'
import type { UserConfig } from '../lib/api'
import { AVAILABLE_ROLES } from '../lib/api'

interface UserEditModalProps {
  email: string
  user: UserConfig
  isNew: boolean
  onSave: (email: string, user: UserConfig) => void
  onClose: () => void
}

// Move regex to top level as per ultracite rules
const EMAIL_REGEX = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export function UserEditModal({
  email: initialEmail,
  user,
  isNew,
  onSave,
  onClose,
}: UserEditModalProps) {
  const [email, setEmail] = useState(initialEmail)
  const [name, setName] = useState(user.name)
  const [selectedRoles, setSelectedRoles] = useState<string[]>(user.roles)
  const [errors, setErrors] = useState<{ email?: string; name?: string; roles?: string }>({})

  useEffect(() => {
    setEmail(initialEmail)
    setName(user.name)
    setSelectedRoles(user.roles)
  }, [initialEmail, user])

  const validateForm = () => {
    const newErrors: typeof errors = {}

    if (!email.trim()) {
      newErrors.email = 'Email is required'
    } else if (!EMAIL_REGEX.test(email)) {
      newErrors.email = 'Invalid email format'
    }

    if (!name.trim()) {
      newErrors.name = 'Name is required'
    }

    if (selectedRoles.length === 0) {
      newErrors.roles = 'At least one role is required'
    }

    setErrors(newErrors)
    return Object.keys(newErrors).length === 0
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (validateForm()) {
      onSave(email, { name, roles: selectedRoles })
    }
  }

  const toggleRole = (role: string) => {
    setSelectedRoles((previous) =>
      previous.includes(role) ? previous.filter((r) => r !== role) : [...previous, role]
    )
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-gray-500 bg-opacity-75 p-4">
      <div className="w-full max-w-md rounded-lg bg-white p-6">
        <h3 className="mb-4 font-medium text-gray-900 text-lg">
          {isNew ? 'Add User' : 'Edit User'}
        </h3>

        <form className="space-y-4" onSubmit={handleSubmit}>
          <div>
            <label className="block font-medium text-gray-700 text-sm" htmlFor="email">
              Email
            </label>
            <input
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 disabled:bg-gray-100 sm:text-sm"
              disabled={!isNew}
              id="email"
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@example.com"
              type="email"
              value={email}
            />
            {errors.email ? <p className="mt-1 text-red-600 text-sm">{errors.email}</p> : null}
          </div>

          <div>
            <label className="block font-medium text-gray-700 text-sm" htmlFor="name">
              Name
            </label>
            <input
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 sm:text-sm"
              id="name"
              onChange={(e) => setName(e.target.value)}
              placeholder="John Doe"
              type="text"
              value={name}
            />
            {errors.name ? <p className="mt-1 text-red-600 text-sm">{errors.name}</p> : null}
          </div>

          <fieldset>
            <legend className="mb-2 block font-medium text-gray-700 text-sm">Roles</legend>
            <div className="space-y-2">
              {Object.entries(AVAILABLE_ROLES).map(([key, role]) => (
                <label className="flex items-start" key={key}>
                  <input
                    checked={selectedRoles.includes(key)}
                    className="mt-1 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                    onChange={() => toggleRole(key)}
                    type="checkbox"
                  />
                  <div className="ml-3">
                    <span className="font-medium text-gray-700 text-sm">{role.name}</span>
                    <p className="text-gray-500 text-xs">{role.description}</p>
                  </div>
                </label>
              ))}
            </div>
            {errors.roles ? <p className="mt-1 text-red-600 text-sm">{errors.roles}</p> : null}
          </fieldset>

          <div className="flex justify-end space-x-3 pt-4">
            <button
              className="rounded-md border border-gray-300 px-4 py-2 font-medium text-gray-700 text-sm hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
              onClick={onClose}
              type="button"
            >
              Cancel
            </button>
            <button
              className="rounded-md border border-transparent bg-blue-600 px-4 py-2 font-medium text-sm text-white shadow-sm hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
              type="submit"
            >
              {isNew ? 'Add User' : 'Save Changes'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
