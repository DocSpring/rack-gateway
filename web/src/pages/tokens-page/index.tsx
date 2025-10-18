import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { QUERY_KEYS } from '@/lib/query-keys'
import { ConfirmDeleteDialog } from '../../components/confirm-delete-dialog'
import { TablePane } from '../../components/table-pane'
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
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../components/ui/table'
import { TooltipProvider } from '../../components/ui/tooltip'
import { useAuth } from '../../contexts/auth-context'
import { useStepUp } from '../../contexts/step-up-context'
import { api } from '../../lib/api'
import { DEFAULT_PER_PAGE } from '../../lib/constants'
import type { APITokenResponse } from '../../lib/generated/gateway-types'
import { toFieldErrorMap, tokenFormSchema } from '../../lib/validation'
import { CreateTokenDialog } from './create-token-dialog'
import { TokenPermissionsEditor } from './permission-components'
import { findMatchingRole, normalizePermissions, permissionsEqual } from './permission-utils'
import { TokenRow } from './token-row'
import type { APIToken, TOKEN_FORM_FIELDS, TokenPermissionMetadata } from './types'

export type { APIToken } from './types'

export function TokensPage() {
  return <TokensPageInner />
}

function TokensPageInner() {
  const queryClient = useQueryClient()
  const { user: currentUser } = useAuth()
  const { handleStepUpError } = useStepUp()
  const [isCreateOpen, setIsCreateOpen] = useState(false)
  const [newTokenName, setNewTokenName] = useState('')
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [createdTokenUuid, setCreatedTokenUuid] = useState<string | null>(null)
  const [isEditOpen, setIsEditOpen] = useState(false)
  const [editToken, setEditToken] = useState<APIToken | null>(null)
  const [editName, setEditName] = useState('')
  const [editPermissions, setEditPermissions] = useState<string[]>([])
  const [editActiveRole, setEditActiveRole] = useState<string | null>(null)
  const [isDeleteOpen, setIsDeleteOpen] = useState(false)
  const [tokenToDelete, setTokenToDelete] = useState<APIToken | null>(null)
  const [selectedPermissions, setSelectedPermissions] = useState<string[]>([])
  const [activeRole, setActiveRole] = useState<string | null>(null)
  const [createErrors, setCreateErrors] = useState<
    Record<(typeof TOKEN_FORM_FIELDS)[number], string | undefined>
  >({
    name: undefined,
    permissions: undefined,
  })
  const [editErrors, setEditErrors] = useState<
    Record<(typeof TOKEN_FORM_FIELDS)[number], string | undefined>
  >({
    name: undefined,
    permissions: undefined,
  })

  const clearCreateError = (field: keyof typeof createErrors) => {
    if (createErrors[field]) {
      setCreateErrors((prev) => ({ ...prev, [field]: undefined }))
    }
  }

  const clearEditError = (field: keyof typeof editErrors) => {
    if (editErrors[field]) {
      setEditErrors((prev) => ({ ...prev, [field]: undefined }))
    }
  }

  const handleCreateNameChange = (value: string) => {
    setNewTokenName(value)
    clearCreateError('name')
  }

  const { data: permissionMetadata, isLoading: isPermissionLoading } = useQuery({
    queryKey: ['token-permissions'],
    queryFn: async () => {
      const response = await api.get<TokenPermissionMetadata>('/api/v1/api-tokens/permissions')
      return response
    },
    staleTime: 5 * 60 * 1000,
  })

  const {
    data: tokens = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: QUERY_KEYS.TOKENS,
    queryFn: async () => {
      const response = await api.get<APIToken[]>('/api/v1/api-tokens')
      return response
    },
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const tokenList: APIToken[] = Array.isArray(tokens) ? tokens : []
  const perPage = DEFAULT_PER_PAGE
  const total = tokenList.length
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const [page, setPage] = useState(1)
  const start = (page - 1) * perPage
  const end = Math.min(start + perPage, total)
  const rows = tokenList.slice(start, end)

  const roles = currentUser?.roles || []
  const isAdmin = roles.includes('admin')
  const isDeployer = roles.includes('deployer')
  const canCreate = isAdmin || isDeployer

  const availablePermissions = useMemo(
    () => normalizePermissions(permissionMetadata?.permissions ?? []),
    [permissionMetadata]
  )
  const roleShortcuts = permissionMetadata?.roles ?? []
  const userPermissions = useMemo(
    () => normalizePermissions(permissionMetadata?.user_permissions ?? []),
    [permissionMetadata]
  )
  const userPermissionsSet = useMemo(() => new Set(userPermissions), [userPermissions])
  const hasWildcardPermission = userPermissionsSet.has('convox:*:*')
  const selectedPermissionsSet = useMemo(() => new Set(selectedPermissions), [selectedPermissions])
  const editPermissionsSet = useMemo(() => new Set(editPermissions), [editPermissions])

  const canAssignPermission = (permission: string): boolean => {
    if (hasWildcardPermission) {
      return true
    }
    if (userPermissionsSet.has(permission)) {
      return true
    }
    for (const perm of userPermissionsSet) {
      if (perm.endsWith(':*') && permission.startsWith(perm.slice(0, -1))) {
        return true
      }
    }
    return false
  }

  useEffect(() => {
    if (!(isCreateOpen && permissionMetadata)) {
      return
    }
    if (selectedPermissions.length > 0) {
      return
    }
    const defaults = normalizePermissions(permissionMetadata.default_permissions ?? [])
    setSelectedPermissions(defaults)
    setActiveRole(findMatchingRole(defaults, roleShortcuts))
  }, [isCreateOpen, permissionMetadata, roleShortcuts, selectedPermissions.length])

  useEffect(() => {
    if (!(isEditOpen && editToken)) {
      return
    }

    const normalized = normalizePermissions(editToken.permissions ?? [])
    if (!permissionsEqual(normalized, editPermissions)) {
      setEditPermissions(normalized)
    }

    const nextRole = findMatchingRole(normalized, roleShortcuts)
    if (nextRole !== editActiveRole) {
      setEditActiveRole(nextRole)
    }
  }, [isEditOpen, editToken, roleShortcuts, editPermissions, editActiveRole])

  const createTokenMutation = useMutation({
    mutationFn: async (payload: { name: string; permissions: string[] }) => {
      const response = await api.post<APITokenResponse>('/api/v1/api-tokens', {
        name: payload.name,
        permissions: payload.permissions,
      })
      return response
    },
    onSuccess: (data) => {
      setCreatedToken(data.token || '')
      setCreatedTokenUuid(data.api_token?.public_id || null)
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.TOKENS })
      toast.success('API token created successfully')
      setCreateErrors({ name: undefined, permissions: undefined })
    },
  })

  const deleteTokenMutation = useMutation({
    mutationFn: async (tokenPublicId: string) => {
      await api.delete(`/api/v1/api-tokens/${tokenPublicId}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.TOKENS })
      toast.success('Token deleted successfully')
      handleDeleteDialogOpenChange(false)
    },
  })

  const updateTokenMutation = useMutation({
    mutationFn: async ({
      publicId,
      name,
      permissions,
    }: {
      publicId: string
      name: string
      permissions: string[]
    }) => {
      await api.put(`/api/v1/api-tokens/${publicId}`, {
        name,
        permissions,
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.TOKENS })
      toast.success('Token updated successfully')
      setIsEditOpen(false)
      setEditToken(null)
      setEditName('')
      setEditPermissions([])
      setEditActiveRole(null)
      setEditErrors({ name: undefined, permissions: undefined })
    },
  })

  const copyToClipboard = (value: string, successMessage: string) => {
    if (!value) return
    navigator.clipboard
      .writeText(value)
      .then(() => toast.success(successMessage))
      .catch(() => toast.error('Failed to copy to clipboard'))
  }

  const handleCreateToken = async () => {
    const parsed = tokenFormSchema.safeParse({
      name: newTokenName,
      permissions: selectedPermissions,
    })

    if (!parsed.success) {
      setCreateErrors((prev) => ({
        ...prev,
        ...toFieldErrorMap(parsed.error, ['name', 'permissions'] as const),
      }))
      return
    }

    const payload = parsed.data

    setCreateErrors({ name: undefined, permissions: undefined })
    setNewTokenName(payload.name)
    setSelectedPermissions(payload.permissions)

    try {
      await createTokenMutation.mutateAsync(payload)
    } catch (err) {
      handleStepUpError(err, () => createTokenMutation.mutateAsync(payload))
    }
  }

  const handleCopyToken = () => {
    if (createdToken) {
      copyToClipboard(createdToken, 'Token copied to clipboard')
    }
  }

  const handleCopyUuid = () => {
    if (createdTokenUuid) {
      copyToClipboard(createdTokenUuid, 'UUID copied to clipboard')
    }
  }

  const resetCreateState = useCallback(() => {
    setNewTokenName('')
    setCreatedToken(null)
    setCreatedTokenUuid(null)
    setSelectedPermissions([])
    setActiveRole(null)
    setCreateErrors({ name: undefined, permissions: undefined })
  }, [])

  const closeCreateModal = () => {
    setIsCreateOpen(false)
  }

  const closeCreateModalAndReset = () => {
    setIsCreateOpen(false)
    resetCreateState()
  }

  const openDeleteDialog = (t: APIToken) => {
    setTokenToDelete(t)
    setIsDeleteOpen(true)
  }

  const handleDeleteDialogOpenChange = (open: boolean) => {
    setIsDeleteOpen(open)
    if (!open) {
      setTokenToDelete(null)
    }
  }

  const confirmDeleteToken = async () => {
    if (!tokenToDelete) {
      return
    }
    const tokenPublicId = tokenToDelete.public_id
    try {
      await deleteTokenMutation.mutateAsync(tokenPublicId)
    } catch (err) {
      handleStepUpError(err, () => deleteTokenMutation.mutateAsync(tokenPublicId))
    }
  }

  const handleRoleShortcut = (role: import('./types').TokenRoleInfo) => {
    const normalized = normalizePermissions(role.permissions)
    if (!normalized.every(canAssignPermission)) {
      return
    }
    setSelectedPermissions(normalized)
    setActiveRole(role.name)
    clearCreateError('permissions')
  }

  const handlePermissionToggle = (permission: string) => {
    if (!canAssignPermission(permission)) {
      return
    }
    clearCreateError('permissions')
    setSelectedPermissions((prev) => {
      const nextSet = new Set(prev)
      if (nextSet.has(permission)) {
        nextSet.delete(permission)
      } else {
        nextSet.add(permission)
      }
      const next = Array.from(nextSet).sort()
      setActiveRole(findMatchingRole(next, roleShortcuts))
      return next
    })
  }

  const handleEditRoleShortcut = (role: import('./types').TokenRoleInfo) => {
    const normalized = normalizePermissions(role.permissions)
    if (!normalized.every(canAssignPermission)) {
      return
    }
    setEditPermissions(normalized)
    setEditActiveRole(role.name)
    clearEditError('permissions')
  }

  const handleEditPermissionToggle = (permission: string) => {
    if (!canAssignPermission(permission)) {
      return
    }
    clearEditError('permissions')
    setEditPermissions((prev) => {
      const nextSet = new Set(prev)
      if (nextSet.has(permission)) {
        nextSet.delete(permission)
      } else {
        nextSet.add(permission)
      }
      const next = Array.from(nextSet).sort()
      setEditActiveRole(findMatchingRole(next, roleShortcuts))
      return next
    })
  }

  useEffect(() => {
    if (!isCreateOpen) {
      const timer = window.setTimeout(() => {
        resetCreateState()
      }, 180)
      return () => window.clearTimeout(timer)
    }
  }, [isCreateOpen, resetCreateState])

  const handleUpdateToken = async () => {
    if (!editToken) {
      return
    }
    const parsed = tokenFormSchema.safeParse({
      name: editName,
      permissions: editPermissions,
    })

    if (!parsed.success) {
      setEditErrors((prev) => ({
        ...prev,
        ...toFieldErrorMap(parsed.error, ['name', 'permissions'] as const),
      }))
      return
    }

    const payload = parsed.data

    setEditErrors({ name: undefined, permissions: undefined })
    setEditName(payload.name)
    setEditPermissions(payload.permissions)

    const args = {
      publicId: editToken.public_id,
      name: payload.name,
      permissions: payload.permissions,
    }

    try {
      await updateTokenMutation.mutateAsync(args)
    } catch (err) {
      handleStepUpError(err, () => updateTokenMutation.mutateAsync(args))
    }
  }

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">API Tokens</h1>
        <p className="mt-2 text-muted-foreground">
          Manage API tokens for programmatic access to the gateway
        </p>
      </div>

      <TablePane
        description="Tokens allow external services to authenticate with the gateway"
        empty={tokenList.length === 0}
        emptyMessage="No API tokens created yet"
        error={error ? 'Failed to load API tokens' : null}
        headerRight={
          canCreate ? (
            <Button
              onClick={() => {
                setNewTokenName('')
                setCreatedToken(null)
                setCreatedTokenUuid(null)
                setSelectedPermissions([])
                setActiveRole(null)
                setCreateErrors({ name: undefined, permissions: undefined })
                setIsCreateOpen(true)
              }}
            >
              <Plus className="mr-2 h-4 w-4" />
              Create Token
            </Button>
          ) : undefined
        }
        loading={!!isLoading}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Token ID</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Created By</TableHead>
              <TableHead>Last Used</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((token: APIToken) => {
              const isOwner =
                token.created_by_email && currentUser?.email
                  ? token.created_by_email.toLowerCase() === currentUser.email.toLowerCase()
                  : false
              const canEdit = isAdmin || (isDeployer && isOwner)
              return (
                <TokenRow
                  canEdit={canEdit}
                  deletePending={deleteTokenMutation.isPending}
                  key={token.id}
                  onDelete={() => {
                    if (!canEdit) {
                      return
                    }
                    openDeleteDialog(token)
                  }}
                  onEdit={() => {
                    if (!canEdit) {
                      return
                    }
                    setEditToken(token)
                    setEditName(token.name)
                    const normalized = normalizePermissions(token.permissions ?? [])
                    setEditPermissions(normalized)
                    setEditActiveRole(findMatchingRole(normalized, roleShortcuts))
                    setEditErrors({ name: undefined, permissions: undefined })
                    setIsEditOpen(true)
                  }}
                  token={token}
                />
              )
            })}
          </TableBody>
        </Table>

        {total > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">
              Showing {start + 1}–{end} of {total} tokens
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

      <CreateTokenDialog
        activeRole={activeRole}
        availablePermissions={availablePermissions}
        canAssignPermission={canAssignPermission}
        createdToken={createdToken}
        createdTokenUuid={createdTokenUuid}
        errors={createErrors}
        isCreating={createTokenMutation.isPending}
        isOpen={isCreateOpen}
        isPermissionLoading={isPermissionLoading}
        onCancel={closeCreateModalAndReset}
        onClose={closeCreateModal}
        onCopyToken={handleCopyToken}
        onCopyUuid={handleCopyUuid}
        onOpenChange={setIsCreateOpen}
        onPermissionToggle={handlePermissionToggle}
        onRoleSelect={handleRoleShortcut}
        onSubmit={handleCreateToken}
        onTokenNameChange={handleCreateNameChange}
        roleShortcuts={roleShortcuts}
        selectedPermissions={selectedPermissions}
        selectedPermissionsSet={selectedPermissionsSet}
        tokenName={newTokenName}
      />

      <Dialog
        onOpenChange={(open) => {
          setIsEditOpen(open)
          if (!open) {
            setEditToken(null)
            setEditName('')
            setEditPermissions([])
            setEditActiveRole(null)
            setEditErrors({ name: undefined, permissions: undefined })
          }
        }}
        open={isEditOpen}
      >
        <DialogContent>
          <TooltipProvider>
            <DialogHeader>
              <DialogTitle>Edit API Token</DialogTitle>
              <DialogDescription>Update the name and permissions for this token.</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="edit-name">Token Name</Label>
                <Input
                  id="edit-name"
                  onChange={(e) => {
                    setEditName(e.target.value)
                    clearEditError('name')
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      handleUpdateToken().catch(() => {
                        /* errors handled by handler */
                      })
                    }
                  }}
                  value={editName}
                />
                {editErrors.name ? (
                  <p className="text-destructive text-sm">{editErrors.name}</p>
                ) : null}
              </div>
              <TokenPermissionsEditor
                activeRole={editActiveRole}
                availablePermissions={availablePermissions}
                canAssignPermission={canAssignPermission}
                error={editErrors.permissions}
                isPermissionLoading={isPermissionLoading}
                onPermissionToggle={handleEditPermissionToggle}
                onRoleSelect={handleEditRoleShortcut}
                roleShortcuts={roleShortcuts}
                selectedPermissions={editPermissions}
                selectedPermissionsSet={editPermissionsSet}
              />
            </div>
            <DialogFooter>
              <Button onClick={() => setIsEditOpen(false)} variant="outline">
                Cancel
              </Button>
              <Button
                disabled={updateTokenMutation.isPending || isPermissionLoading || !editToken}
                onClick={() => {
                  handleUpdateToken().catch(() => {
                    /* errors handled by handler */
                  })
                }}
              >
                {updateTokenMutation.isPending ? 'Saving...' : 'Save'}
              </Button>
            </DialogFooter>
          </TooltipProvider>
        </DialogContent>
      </Dialog>

      <ConfirmDeleteDialog
        busy={deleteTokenMutation.isPending}
        confirmButtonText="Delete Token"
        description={
          <>This action cannot be undone. Type DELETE to remove "{tokenToDelete?.name}".</>
        }
        inputId="confirm-delete-token"
        onConfirm={() => {
          confirmDeleteToken().catch(() => {
            /* errors handled in handler */
          })
        }}
        onOpenChange={handleDeleteDialogOpenChange}
        open={isDeleteOpen}
        title="Delete API Token"
      />
    </div>
  )
}
