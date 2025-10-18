import { useQuery } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { QUERY_KEYS } from '@/lib/query-keys'
import { TablePane } from '../../components/table-pane'
import { Button } from '../../components/ui/button'
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../components/ui/table'
import { useAuth } from '../../contexts/auth-context'
import { api } from '../../lib/api'
import { DEFAULT_PER_PAGE } from '../../lib/constants'
import { CreateTokenDialog } from './create-token-dialog'
import { DeleteTokenDialog } from './delete-token-dialog'
import { EditTokenDialog } from './edit-token-dialog'
import { normalizePermissions } from './permission-utils'
import { TokenRow } from './token-row'
import type { APIToken, TokenPermissionMetadata } from './types'

export type { APIToken } from './types'

type ModalState =
  | { type: 'closed' }
  | { type: 'create' }
  | { type: 'edit'; tokenId: string }
  | { type: 'delete'; tokenId: string }

export function TokensPage() {
  return <TokensPageInner />
}

function TokensPageInner() {
  const { user: currentUser } = useAuth()
  const [modal, setModal] = useState<ModalState>({ type: 'closed' })
  const [page, setPage] = useState(1)

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
  })

  const tokenList: APIToken[] = Array.isArray(tokens) ? tokens : []
  const perPage = DEFAULT_PER_PAGE
  const total = tokenList.length
  const totalPages = Math.max(1, Math.ceil(total / perPage))
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
            <Button onClick={() => setModal({ type: 'create' })}>
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
                  deletePending={false}
                  key={token.id}
                  onDelete={() => {
                    if (!canEdit) {
                      return
                    }
                    setModal({ type: 'delete', tokenId: token.public_id })
                  }}
                  onEdit={() => {
                    if (!canEdit) {
                      return
                    }
                    setModal({ type: 'edit', tokenId: token.public_id })
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
        availablePermissions={availablePermissions}
        canAssignPermission={canAssignPermission}
        isOpen={modal.type === 'create'}
        isPermissionLoading={isPermissionLoading}
        onClose={() => setModal({ type: 'closed' })}
        permissionMetadata={permissionMetadata}
        roleShortcuts={roleShortcuts}
      />

      <EditTokenDialog
        availablePermissions={availablePermissions}
        canAssignPermission={canAssignPermission}
        isOpen={modal.type === 'edit'}
        isPermissionLoading={isPermissionLoading}
        onClose={() => setModal({ type: 'closed' })}
        roleShortcuts={roleShortcuts}
        tokenId={modal.type === 'edit' ? modal.tokenId : null}
      />

      <DeleteTokenDialog
        isOpen={modal.type === 'delete'}
        onClose={() => setModal({ type: 'closed' })}
        tokenId={modal.type === 'delete' ? modal.tokenId : null}
      />
    </div>
  )
}
