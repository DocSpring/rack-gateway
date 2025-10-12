import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, MoreVertical, Pencil, Plus, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { TimeAgo } from '@/components/time-ago'
import { toast } from '@/components/ui/use-toast'
import { ConfirmDeleteDialog } from '../components/confirm-delete-dialog'
import { TablePane } from '../components/table-pane'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../components/ui/dropdown-menu'
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
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../components/ui/tooltip'
import { UuidCell } from '../components/uuid-cell'
import { useAuth } from '../contexts/auth-context'
import { useStepUp } from '../contexts/step-up-context'
import { api } from '../lib/api'
import { DEFAULT_PER_PAGE } from '../lib/constants'
import { toastAPIError } from '../lib/error-utils'
import type { APIToken as APITokenModel, APITokenResponse } from '../lib/generated/gateway-types'
import { toFieldErrorMap, tokenFormSchema } from '../lib/validation'

export type APIToken = APITokenModel

type TokenRoleInfo = {
  name: string
  label: string
  description: string
  permissions: string[]
}

type TokenPermissionMetadata = {
  permissions: string[]
  roles: TokenRoleInfo[]
  default_permissions: string[]
  user_roles: string[]
  user_permissions: string[]
}

type PermissionOption = {
  value: string
  title: string
  description: string
  sortKey: string
}

type PermissionGroup = {
  key: string
  label: string
  sortKey: string
  options: PermissionOption[]
}

const WORD_DELIMITER_REGEX = /[-_\s]+/
const TOKEN_FORM_FIELDS = ['name', 'permissions'] as const

function normalizePermissions(perms: string[]): string[] {
  if (!perms || perms.length === 0) {
    return []
  }
  const unique = new Set(perms.map((p) => p.trim().toLowerCase()).filter(Boolean))
  return Array.from(unique).sort()
}

function permissionsEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) {
    return false
  }
  return a.every((perm, idx) => perm === b[idx])
}

function findMatchingRole(perms: string[], roles: TokenRoleInfo[]): string | null {
  for (const role of roles) {
    const rolePerms = normalizePermissions(role.permissions)
    if (permissionsEqual(perms, rolePerms)) {
      return role.name
    }
  }
  return null
}

function buildPermissionGroups(permissions: string[]): PermissionGroup[] {
  const groups = new Map<string, PermissionGroup>()

  for (const permission of permissions) {
    const { groupKey, groupLabel, groupSortKey, actionLabel } = derivePermissionParts(permission)
    const option: PermissionOption = {
      value: permission,
      title: actionLabel,
      description: permission,
      sortKey: actionLabel,
    }

    const existing = groups.get(groupKey)
    if (existing) {
      existing.options.push(option)
    } else {
      groups.set(groupKey, {
        key: groupKey,
        label: groupLabel,
        sortKey: groupSortKey,
        options: [option],
      })
    }
  }

  return Array.from(groups.values())
    .map((group) => ({
      ...group,
      options: group.options.sort((a, b) =>
        a.sortKey.localeCompare(b.sortKey, undefined, {
          sensitivity: 'base',
        })
      ),
    }))
    .sort((a, b) => a.sortKey.localeCompare(b.sortKey, undefined, { sensitivity: 'base' }))
}

function derivePermissionParts(permission: string): {
  groupKey: string
  groupLabel: string
  groupSortKey: string
  actionLabel: string
} {
  if (!permission.includes(':')) {
    return {
      groupKey: 'other',
      groupLabel: 'Other',
      groupSortKey: 'other',
      actionLabel: permission,
    }
  }

  const segments = permission.split(':')
  const resourceRaw = segments[1] || 'other'
  const actionRaw = segments.slice(2).join(':') || '*'

  const groupLabel = humanizeGroup(resourceRaw)
  const groupSortKey = groupLabel.toLowerCase()
  const actionLabel = humanizeAction(actionRaw)

  return {
    groupKey: resourceRaw || 'other',
    groupLabel,
    groupSortKey,
    actionLabel,
  }
}

function humanizeGroup(value: string): string {
  if (!value || value === '*') {
    return 'All'
  }
  return value
    .split(WORD_DELIMITER_REGEX)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function humanizeAction(value: string): string {
  if (!value || value === '*') {
    return 'all'
  }
  return value.replace(/_/g, ' ')
}

function TokenRow({
  token,
  deletePending,
  onDelete,
  onEdit,
  canEdit,
}: {
  token: APIToken
  deletePending: boolean
  onDelete: () => void
  onEdit: () => void
  canEdit: boolean
}) {
  const exp = token.expires_at ? new Date(token.expires_at) : null
  const isExpired = exp ? exp < new Date() : false
  return (
    <TableRow key={token.id}>
      <TableCell>
        <UuidCell label="Token ID" uuid={token.public_id} />
      </TableCell>
      <TableCell className="font-medium">{token.name}</TableCell>
      <TableCell>
        <Badge variant={isExpired ? 'destructive' : 'default'}>
          {isExpired ? 'Expired' : 'Active'}
        </Badge>
      </TableCell>
      <TableCell>{token.created_by_email || token.created_by_name || '-'}</TableCell>
      <TableCell>{token.last_used_at ? <TimeAgo date={token.last_used_at} /> : 'Never'}</TableCell>
      <TableCell>
        <TimeAgo date={token.created_at} />
      </TableCell>
      <TableCell className="text-right">
        {canEdit ? (
          <div className="flex justify-end">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button aria-label={`Actions for ${token.name}`} size="sm" variant="ghost">
                  <MoreVertical className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={onEdit}>
                  <Pencil className="h-4 w-4" />
                  Edit Token
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem disabled={deletePending} onClick={onDelete} variant="destructive">
                  <Trash2 className="h-4 w-4" />
                  Delete Token
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        ) : null}
      </TableCell>
    </TableRow>
  )
}

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
    Record<'name' | 'permissions', string | undefined>
  >({
    name: undefined,
    permissions: undefined,
  })
  const [editErrors, setEditErrors] = useState<Record<'name' | 'permissions', string | undefined>>({
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
      const response = await api.get<TokenPermissionMetadata>(
        '/.gateway/api/admin/tokens/permissions'
      )
      return response
    },
    staleTime: 5 * 60 * 1000,
  })

  // Fetch tokens
  const {
    data: tokens = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['tokens'],
    queryFn: async () => {
      const response = await api.get<APIToken[]>('/.gateway/api/admin/tokens')
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
  // per-row edit permission computed using ownership; global check not used here

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
    setEditPermissions(normalized)
    setEditActiveRole(findMatchingRole(normalized, roleShortcuts))
  }, [isEditOpen, editToken, roleShortcuts])

  // Create token mutation
  const createTokenMutation = useMutation({
    mutationFn: async (payload: { name: string; permissions: string[] }) => {
      const response = await api.post<APITokenResponse>('/.gateway/api/admin/tokens', {
        name: payload.name,
        permissions: payload.permissions,
      })
      return response
    },
    onSuccess: (data) => {
      setCreatedToken(data.token || '')
      setCreatedTokenUuid(data.api_token?.public_id || null)
      queryClient.invalidateQueries({ queryKey: ['tokens'] })
      toast.success('API token created successfully')
      setCreateErrors({ name: undefined, permissions: undefined })
    },
  })

  // Delete token mutation
  const deleteTokenMutation = useMutation({
    mutationFn: async (tokenPublicId: string) => {
      await api.delete(`/.gateway/api/admin/tokens/${tokenPublicId}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tokens'] })
      toast.success('Token deleted successfully')
      handleDeleteDialogOpenChange(false)
    },
  })

  // Update token mutation (name and permissions)
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
      await api.put(`/.gateway/api/admin/tokens/${publicId}`, {
        name,
        permissions,
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tokens'] })
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
        ...toFieldErrorMap(parsed.error, TOKEN_FORM_FIELDS),
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
      if (handleStepUpError(err, () => createTokenMutation.mutateAsync(payload))) {
        return
      }
      toastAPIError(err, 'Failed to create token')
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

  // Close create dialog without resetting content to avoid flash during fade-out
  const closeCreateModal = () => {
    setIsCreateOpen(false)
  }

  // Close and reset create dialog state (used by Cancel)
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
      if (handleStepUpError(err, () => deleteTokenMutation.mutateAsync(tokenPublicId))) {
        return
      }
      toastAPIError(err, 'Failed to delete token')
    }
  }

  const handleRoleShortcut = (role: TokenRoleInfo) => {
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

  const handleEditRoleShortcut = (role: TokenRoleInfo) => {
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
        ...toFieldErrorMap(parsed.error, TOKEN_FORM_FIELDS),
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
      if (handleStepUpError(err, () => updateTokenMutation.mutateAsync(args))) {
        return
      }
      toastAPIError(err, 'Failed to update token')
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

      {/* Edit Token Dialog */}
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

      {/* Delete Token Confirmation Dialog */}
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

function RoleShortcutButtons({
  roleShortcuts,
  activeRole,
  selectedPermissions,
  onRoleSelect,
  canAssignPermission,
}: {
  roleShortcuts: TokenRoleInfo[]
  activeRole: string | null
  selectedPermissions: string[]
  onRoleSelect: (role: TokenRoleInfo) => void
  canAssignPermission: (permission: string) => boolean
}) {
  return (
    <div className="space-y-2">
      <Label>Role Shortcuts</Label>
      <p className="text-muted-foreground text-sm">
        Choose a baseline permission set and optionally fine-tune the list below.
      </p>
      <div className="flex flex-wrap gap-2">
        {roleShortcuts.length === 0 ? (
          <Badge variant="outline">No predefined roles</Badge>
        ) : (
          roleShortcuts.map((role) => {
            const rolePermissions = normalizePermissions(role.permissions)
            const isRoleActive =
              activeRole === role.name && permissionsEqual(selectedPermissions, rolePermissions)
            const roleAllowed = rolePermissions.every(canAssignPermission)
            const button = (
              <Button
                disabled={!roleAllowed}
                key={role.name}
                onClick={() => onRoleSelect(role)}
                size="sm"
                variant={isRoleActive ? 'default' : 'outline'}
              >
                {role.label}
              </Button>
            )
            if (roleAllowed) {
              return button
            }
            return (
              <Tooltip delayDuration={150} key={role.name}>
                <TooltipTrigger asChild>{button}</TooltipTrigger>
                <TooltipContent align="start">
                  You don't have permission to assign this role.
                </TooltipContent>
              </Tooltip>
            )
          })
        )}
      </div>
    </div>
  )
}

function PermissionCheckboxGrid({
  availablePermissions,
  selectedPermissionsSet,
  onPermissionToggle,
  canAssignPermission,
  isLoading,
}: {
  availablePermissions: string[]
  selectedPermissionsSet: Set<string>
  onPermissionToggle: (permission: string) => void
  canAssignPermission: (permission: string) => boolean
  isLoading: boolean
}) {
  const groupedPermissions = useMemo(
    () => buildPermissionGroups(availablePermissions),
    [availablePermissions]
  )

  const topLevelOptions = useMemo(
    () => groupedPermissions.filter((group) => group.key === '*').flatMap((group) => group.options),
    [groupedPermissions]
  )

  const nestedGroups = useMemo(
    () => groupedPermissions.filter((group) => group.key !== '*'),
    [groupedPermissions]
  )

  const renderOption = (option: PermissionOption) => {
    const isSelected = selectedPermissionsSet.has(option.value)
    const assignable = canAssignPermission(option.value)

    if (assignable) {
      return (
        <label
          className="flex cursor-pointer items-start gap-3 rounded-md px-2 py-2 text-sm leading-5 transition-colors hover:bg-muted"
          key={option.value}
        >
          <input
            checked={isSelected}
            className="mt-1 h-4 w-4"
            onChange={() => onPermissionToggle(option.value)}
            type="checkbox"
          />
          <span className="font-normal">
            <span className="block font-medium capitalize">{option.title}</span>
            <span className="block text-muted-foreground text-xs">{option.description}</span>
          </span>
        </label>
      )
    }

    return (
      <Tooltip delayDuration={150} key={option.value}>
        <TooltipTrigger asChild>
          <label className="flex cursor-not-allowed items-start gap-3 rounded-md px-2 py-2 text-sm leading-5 opacity-60">
            <input
              aria-disabled={true}
              checked={isSelected}
              className="mt-1 h-4 w-4"
              disabled
              onChange={() => onPermissionToggle(option.value)}
              type="checkbox"
            />
            <span className="font-normal">
              <span className="block font-medium capitalize">{option.title}</span>
              <span className="block text-muted-foreground text-xs">{option.description}</span>
            </span>
          </label>
        </TooltipTrigger>
        <TooltipContent align="start">
          You don't have permission to perform that action.
        </TooltipContent>
      </Tooltip>
    )
  }

  return (
    <div className="space-y-2">
      <Label>Permissions</Label>
      {isLoading ? (
        <p className="text-muted-foreground text-sm">Loading permissions…</p>
      ) : (
        <div className="max-h-60 overflow-y-auto rounded-md border p-3">
          {groupedPermissions.length === 0 ? (
            <p className="text-muted-foreground text-sm">No permissions available.</p>
          ) : (
            <div className="space-y-4">
              {topLevelOptions.length > 0 && (
                <div className="space-y-1" key="__top-level-permissions">
                  {topLevelOptions.map((option) => renderOption(option))}
                </div>
              )}

              {nestedGroups.map((group) => (
                <div className="space-y-2" key={group.key}>
                  <p className="font-semibold text-foreground text-sm">{group.label}</p>
                  <div className="space-y-1">
                    {group.options.map((option) => renderOption(option))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function TokenPermissionsEditor({
  availablePermissions,
  roleShortcuts,
  activeRole,
  selectedPermissions,
  selectedPermissionsSet,
  onRoleSelect,
  onPermissionToggle,
  canAssignPermission,
  isPermissionLoading,
  error,
}: {
  availablePermissions: string[]
  roleShortcuts: TokenRoleInfo[]
  activeRole: string | null
  selectedPermissions: string[]
  selectedPermissionsSet: Set<string>
  onRoleSelect: (role: TokenRoleInfo) => void
  onPermissionToggle: (permission: string) => void
  canAssignPermission: (permission: string) => boolean
  isPermissionLoading: boolean
  error?: string
}) {
  return (
    <div className="space-y-4">
      <RoleShortcutButtons
        activeRole={activeRole}
        canAssignPermission={canAssignPermission}
        onRoleSelect={onRoleSelect}
        roleShortcuts={roleShortcuts}
        selectedPermissions={selectedPermissions}
      />
      <PermissionCheckboxGrid
        availablePermissions={availablePermissions}
        canAssignPermission={canAssignPermission}
        isLoading={isPermissionLoading}
        onPermissionToggle={onPermissionToggle}
        selectedPermissionsSet={selectedPermissionsSet}
      />
      {error ? <p className="text-destructive text-sm">{error}</p> : null}
    </div>
  )
}

type CreateTokenDialogProps = {
  activeRole: string | null
  availablePermissions: string[]
  canAssignPermission: (permission: string) => boolean
  errors: { name?: string; permissions?: string }
  createdToken: string | null
  createdTokenUuid: string | null
  isCreating: boolean
  isOpen: boolean
  isPermissionLoading: boolean
  onCancel: () => void
  onCopyToken: () => void
  onCopyUuid: () => void
  onOpenChange: (open: boolean) => void
  onPermissionToggle: (permission: string) => void
  onRoleSelect: (role: TokenRoleInfo) => void
  onSubmit: () => void | Promise<void>
  onTokenNameChange: (value: string) => void
  onClose: () => void
  roleShortcuts: TokenRoleInfo[]
  selectedPermissions: string[]
  selectedPermissionsSet: Set<string>
  tokenName: string
}

function CreateTokenDialog({
  activeRole,
  availablePermissions,
  canAssignPermission,
  errors,
  createdToken,
  createdTokenUuid,
  isCreating,
  isOpen,
  isPermissionLoading,
  onCancel,
  onCopyToken,
  onCopyUuid,
  onOpenChange,
  onPermissionToggle,
  onRoleSelect,
  onSubmit,
  onTokenNameChange,
  onClose,
  roleShortcuts,
  selectedPermissions,
  selectedPermissionsSet,
  tokenName,
}: CreateTokenDialogProps) {
  return (
    <Dialog onOpenChange={onOpenChange} open={isOpen}>
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
                  <Button onClick={onCopyUuid} size="icon" variant="ghost">
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
                  <Button onClick={onCopyToken} size="icon" variant="ghost">
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </div>
          ) : (
            <div className="space-y-4">
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
                  onChange={(e) => onTokenNameChange(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      const result = onSubmit()
                      if (result instanceof Promise) {
                        result.catch(() => {
                          /* errors handled by caller */
                        })
                      }
                    }
                  }}
                  placeholder="e.g., CI/CD Pipeline"
                  spellCheck={false}
                  value={tokenName}
                />
                {errors.name ? <p className="text-destructive text-sm">{errors.name}</p> : null}
              </div>
              <TokenPermissionsEditor
                activeRole={activeRole}
                availablePermissions={availablePermissions}
                canAssignPermission={canAssignPermission}
                error={errors.permissions}
                isPermissionLoading={isPermissionLoading}
                onPermissionToggle={onPermissionToggle}
                onRoleSelect={onRoleSelect}
                roleShortcuts={roleShortcuts}
                selectedPermissions={selectedPermissions}
                selectedPermissionsSet={selectedPermissionsSet}
              />
            </div>
          )}

          <DialogFooter>
            {createdToken ? (
              <Button onClick={onClose}>Done</Button>
            ) : (
              <>
                <Button onClick={onCancel} variant="outline">
                  Cancel
                </Button>
                <Button
                  disabled={isCreating || isPermissionLoading}
                  onClick={() => {
                    const result = onSubmit()
                    if (result instanceof Promise) {
                      result.catch(() => {
                        /* errors handled by caller */
                      })
                    }
                  }}
                >
                  {isCreating ? 'Creating...' : 'Create Token'}
                </Button>
              </>
            )}
          </DialogFooter>
        </TooltipProvider>
      </DialogContent>
    </Dialog>
  )
}
