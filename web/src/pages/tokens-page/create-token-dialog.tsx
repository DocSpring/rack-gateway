import { useForm } from '@tanstack/react-form'
import { Copy } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from '@/components/ui/use-toast'
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
import type { TokenPermissionMetadata, TokenRoleInfo } from './types'
import { useTokenMutations } from './use-token-mutations'

export function CreateTokenDialog({
  isOpen,
  onClose,
  availablePermissions,
  roleShortcuts,
  canAssignPermission,
  isPermissionLoading,
  permissionMetadata,
}: {
  isOpen: boolean
  onClose: () => void
  availablePermissions: string[]
  roleShortcuts: TokenRoleInfo[]
  canAssignPermission: (permission: string) => boolean
  isPermissionLoading: boolean
  permissionMetadata: TokenPermissionMetadata | undefined
}) {
  const { createToken, handleStepUpError } = useTokenMutations()
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [createdTokenUuid, setCreatedTokenUuid] = useState<string | null>(null)
  const [nameError, setNameError] = useState<string | null>(null)

  const form = useForm({
    defaultValues: {
      name: '',
      permissions: [] as string[],
    },
    onSubmit: async ({ value }) => {
      // Validate with Zod
      const result = tokenFormSchema.safeParse(value)
      if (!result.success) {
        const errors = result.error.format()
        const nextNameError = errors.name?._errors?.[0]
        if (nextNameError) {
          setNameError(nextNameError)
          form.setFieldMeta('name', (meta) => ({
            ...meta,
            errors: [nextNameError],
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

      setNameError(null)

      try {
        const response = await createToken.mutateAsync(result.data)
        setCreatedToken(response.token || '')
        setCreatedTokenUuid(response.api_token?.public_id || null)
      } catch (err) {
        handleStepUpError(err, () => createToken.mutateAsync(result.data))
      }
    },
  })

  // Set default permissions when dialog opens
  useEffect(() => {
    if (!(isOpen && permissionMetadata)) {
      return
    }
    const currentPermissions = form.getFieldValue('permissions')
    if (currentPermissions.length > 0) {
      return
    }
    const defaults = normalizePermissions(permissionMetadata.default_permissions ?? [])
    form.setFieldValue('permissions', defaults)
  }, [isOpen, permissionMetadata, form])

  // Reset form when dialog closes
  useEffect(() => {
    if (!isOpen) {
      const timer = window.setTimeout(() => {
        form.reset()
        setCreatedToken(null)
        setCreatedTokenUuid(null)
        setNameError(null)
      }, 180)
      return () => window.clearTimeout(timer)
    }
  }, [isOpen, form])

  const handleRoleSelect = (role: TokenRoleInfo, fieldApi: any) => {
    const normalized = normalizePermissions(role.permissions)
    if (!normalized.every(canAssignPermission)) {
      return
    }
    fieldApi.setValue(normalized)
  }

  const handlePermissionToggle = (permission: string, fieldApi: any) => {
    if (!canAssignPermission(permission)) {
      return
    }
    const current = fieldApi.state.value
    const nextSet = new Set(current)
    if (nextSet.has(permission)) {
      nextSet.delete(permission)
    } else {
      nextSet.add(permission)
    }
    const next = Array.from(nextSet).sort()
    fieldApi.setValue(next)
  }

  const handleClose = () => {
    onClose()
  }

  const copyToClipboard = (value: string, successMessage: string) => {
    if (!value) return
    navigator.clipboard
      .writeText(value)
      .then(() => toast.success(successMessage))
      .catch(() => toast.error('Failed to copy to clipboard'))
  }

  return (
    <Dialog onOpenChange={(open) => !open && handleClose()} open={isOpen}>
      <DialogContent className="sm:max-w-xl">
        <TooltipProvider>
          <DialogHeader>
            <DialogTitle>Create API Token</DialogTitle>
            <DialogDescription>
              {createdToken
                ? "Copy the token secret now. You won't be able to see it again."
                : 'Enter a name for the new API token'}
            </DialogDescription>
          </DialogHeader>

          {createdToken ? (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>Token UUID</Label>
                <div className="flex items-center gap-2">
                  <div className="flex-1 break-all rounded-md bg-muted p-3 font-mono text-sm">
                    {createdTokenUuid}
                  </div>
                  <Button
                    onClick={() =>
                      copyToClipboard(createdTokenUuid || '', 'UUID copied to clipboard')
                    }
                    size="icon"
                    variant="ghost"
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>
              <div className="space-y-2">
                <Label>Token Secret</Label>
                <div className="flex items-center gap-2">
                  <div className="flex-1 break-all rounded-md bg-muted p-3 font-mono text-sm">
                    {createdToken}
                  </div>
                  <Button
                    onClick={() => copyToClipboard(createdToken, 'Token copied to clipboard')}
                    size="icon"
                    variant="ghost"
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </div>
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault()
                e.stopPropagation()
                form.handleSubmit()
              }}
            >
              <div className="space-y-4">
                <form.Field name="name">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="name">Token Name</Label>
                      <Input
                        autoCapitalize="none"
                        autoComplete="off"
                        autoCorrect="off"
                        data-1p-ignore
                        data-bwignore="true"
                        data-lpignore="true"
                        id="name"
                        inputMode="text"
                        name="token_name"
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') {
                            form.handleSubmit()
                          }
                        }}
                        placeholder="e.g., CI/CD Pipeline"
                        spellCheck={false}
                        value={field.state.value}
                      />
                      {(nameError ?? field.state.meta.errors[0]) && (
                        <p className="text-destructive text-sm">
                          {String(nameError ?? field.state.meta.errors[0])}
                        </p>
                      )}
                    </div>
                  )}
                </form.Field>

                <form.Field name="permissions">
                  {(field) => {
                    const permissions = field.state.value
                    const permissionsSet = useMemo(() => new Set(permissions), [permissions])
                    const activeRole = useMemo(
                      () => findMatchingRole(permissions, roleShortcuts),
                      [permissions, roleShortcuts]
                    )
                    return (
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
                        onPermissionToggle={(permission) =>
                          handlePermissionToggle(permission, field)
                        }
                        onRoleSelect={(role) => handleRoleSelect(role, field)}
                        roleShortcuts={roleShortcuts}
                        selectedPermissions={permissions}
                        selectedPermissionsSet={permissionsSet}
                      />
                    )
                  }}
                </form.Field>
              </div>
            </form>
          )}

          <DialogFooter>
            {createdToken ? (
              <Button onClick={handleClose}>Done</Button>
            ) : (
              <>
                <Button onClick={handleClose} type="button" variant="outline">
                  Cancel
                </Button>
                <Button
                  disabled={createToken.isPending || isPermissionLoading}
                  onClick={() => form.handleSubmit()}
                  type="button"
                >
                  {createToken.isPending ? 'Creating...' : 'Create Token'}
                </Button>
              </>
            )}
          </DialogFooter>
        </TooltipProvider>
      </DialogContent>
    </Dialog>
  )
}
