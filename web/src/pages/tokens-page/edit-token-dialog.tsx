import { useForm } from '@tanstack/react-form'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo } from 'react'
import { QUERY_KEYS } from '@/lib/query-keys'
import { Button } from '../../components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../components/ui/dialog'
import { Input } from '../../components/ui/input'
import { Label } from '../../components/ui/label'
import { TooltipProvider } from '../../components/ui/tooltip'
import { tokenFormSchema } from '../../lib/validation'
import { TokenPermissionsEditor } from './permission-components'
import { findMatchingRole, normalizePermissions } from './permission-utils'
import type { APIToken, TokenRoleInfo } from './types'
import { useTokenMutations } from './use-token-mutations'

export function EditTokenDialog({
  tokenId,
  isOpen,
  onClose,
  availablePermissions,
  roleShortcuts,
  canAssignPermission,
  isPermissionLoading,
}: {
  tokenId: string | null
  isOpen: boolean
  onClose: () => void
  availablePermissions: string[]
  roleShortcuts: TokenRoleInfo[]
  canAssignPermission: (permission: string) => boolean
  isPermissionLoading: boolean
}) {
  const { updateToken, handleStepUpError } = useTokenMutations()

  const { data: tokens = [] } = useQuery<APIToken[]>({
    queryKey: QUERY_KEYS.TOKENS,
    enabled: false, // Just reading from cache, not fetching
  })

  const token = tokens.find((t) => t.public_id === tokenId)

  const form = useForm({
    defaultValues: {
      name: '',
      permissions: [] as string[],
    },
    onSubmit: async ({ value }) => {
      if (!token) {
        return
      }

      // Validate with Zod
      const result = tokenFormSchema.safeParse(value)
      if (!result.success) {
        const errors = result.error.format()
        const nameError = errors.name?._errors?.[0]
        if (nameError) {
          form.setFieldMeta('name', (meta) => ({
            ...meta,
            errors: [nameError],
          }))
        }
        const permissionsError = errors.permissions?._errors?.[0]
        if (permissionsError) {
          form.setFieldMeta('permissions', (meta) => ({
            ...meta,
            errors: [permissionsError],
          }))
        }
        return
      }

      try {
        await updateToken.mutateAsync({
          publicId: token.public_id,
          name: result.data.name,
          permissions: result.data.permissions,
        })
        onClose()
      } catch (err) {
        handleStepUpError(err, () =>
          updateToken.mutateAsync({
            publicId: token.public_id,
            name: result.data.name,
            permissions: result.data.permissions,
          })
        )
      }
    },
  })

  // Reset form when dialog opens/token changes
  useEffect(() => {
    if (isOpen && token) {
      const normalized = normalizePermissions(token.permissions ?? [])
      form.setFieldValue('name', token.name)
      form.setFieldValue('permissions', normalized)
    }
  }, [isOpen, token, form])

  const permissions = form.getFieldValue('permissions')
  const permissionsSet = useMemo(() => new Set(permissions), [permissions])
  const activeRole = useMemo(
    () => findMatchingRole(permissions, roleShortcuts),
    [permissions, roleShortcuts]
  )

  const handleRoleSelect = (role: TokenRoleInfo) => {
    const normalized = normalizePermissions(role.permissions)
    if (!normalized.every(canAssignPermission)) {
      return
    }
    form.setFieldValue('permissions', normalized)
  }

  const handlePermissionToggle = (permission: string) => {
    if (!canAssignPermission(permission)) {
      return
    }
    const current = form.getFieldValue('permissions')
    const nextSet = new Set(current)
    if (nextSet.has(permission)) {
      nextSet.delete(permission)
    } else {
      nextSet.add(permission)
    }
    const next = Array.from(nextSet).sort()
    form.setFieldValue('permissions', next)
  }

  return (
    <Dialog
      onOpenChange={(open) => {
        if (!open) {
          onClose()
        }
      }}
      open={isOpen}
    >
      <DialogContent>
        <TooltipProvider>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              e.stopPropagation()
              form.handleSubmit()
            }}
          >
            <DialogHeader>
              <DialogTitle>Edit API Token</DialogTitle>
              <DialogDescription>Update the name and permissions for this token.</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <form.Field name="name">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="edit-name">Token Name</Label>
                    <Input
                      id="edit-name"
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          form.handleSubmit()
                        }
                      }}
                      value={field.state.value}
                    />
                    {field.state.meta.errors.length > 0 && (
                      <p className="text-destructive text-sm">
                        {String(field.state.meta.errors[0])}
                      </p>
                    )}
                  </div>
                )}
              </form.Field>

              <form.Field name="permissions">
                {(field) => (
                  <TokenPermissionsEditor
                    activeRole={activeRole}
                    availablePermissions={availablePermissions}
                    canAssignPermission={canAssignPermission}
                    error={
                      field.state.meta.errors.length > 0
                        ? String(field.state.meta.errors[0])
                        : undefined
                    }
                    isPermissionLoading={isPermissionLoading}
                    onPermissionToggle={handlePermissionToggle}
                    onRoleSelect={handleRoleSelect}
                    roleShortcuts={roleShortcuts}
                    selectedPermissions={permissions}
                    selectedPermissionsSet={permissionsSet}
                  />
                )}
              </form.Field>
            </div>
            <DialogFooter>
              <Button onClick={() => onClose()} type="button" variant="outline">
                Cancel
              </Button>
              <Button
                disabled={updateToken.isPending || isPermissionLoading || !token}
                type="submit"
              >
                {updateToken.isPending ? 'Saving...' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </TooltipProvider>
      </DialogContent>
    </Dialog>
  )
}
