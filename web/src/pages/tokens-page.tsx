import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Copy, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
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
import { api } from '../lib/api'

interface APIToken {
  id: string
  name: string
  token?: string
  last_used: string | null
  created_at: string
  expires_at: string | null
}

export function TokensPage() {
  const queryClient = useQueryClient()
  const [isCreateOpen, setIsCreateOpen] = useState(false)
  const [newTokenName, setNewTokenName] = useState('')
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [isEditOpen, setIsEditOpen] = useState(false)
  const [editToken, setEditToken] = useState<APIToken | null>(null)
  const [editName, setEditName] = useState('')

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
  })

  const tokenList: APIToken[] = Array.isArray(tokens) ? tokens : []

  // Create token mutation
  const createTokenMutation = useMutation({
    mutationFn: async (name: string) => {
      const response = await api.post<APIToken>('/.gateway/api/admin/tokens', { name })
      return response
    },
    onSuccess: (data) => {
      setCreatedToken(data.token || '')
      queryClient.invalidateQueries({ queryKey: ['tokens'] })
      toast.success('API token created successfully')
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to create token')
    },
  })

  // Delete token mutation
  const deleteTokenMutation = useMutation({
    mutationFn: async (tokenId: string) => {
      await api.delete(`/.gateway/api/admin/tokens/${tokenId}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tokens'] })
      toast.success('Token deleted successfully')
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to delete token')
    },
  })

  // Update token name mutation
  const updateTokenMutation = useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) => {
      await api.put(`/.gateway/api/admin/tokens/${id}`, { name })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tokens'] })
      toast.success('Token renamed successfully')
      setIsEditOpen(false)
      setEditToken(null)
      setEditName('')
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to rename token')
    },
  })

  const handleCreateToken = () => {
    if (!newTokenName.trim()) {
      toast.error('Please enter a token name')
      return
    }
    createTokenMutation.mutate(newTokenName)
  }

  const handleCopyToken = () => {
    if (createdToken) {
      navigator.clipboard.writeText(createdToken)
      toast.success('Token copied to clipboard')
    }
  }

  const handleCloseDialog = () => {
    setIsCreateOpen(false)
    setNewTokenName('')
    setCreatedToken(null)
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

  if (error) {
    return (
      <div className="p-8">
        <Card>
          <CardHeader>
            <CardTitle className="text-destructive">Error</CardTitle>
            <CardDescription>Failed to load API tokens</CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">API Tokens</h1>
        <p className="mt-2 text-muted-foreground">
          Manage API tokens for programmatic access to the gateway
        </p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>API Tokens</CardTitle>
              <CardDescription>
                Tokens allow external services to authenticate with the gateway
              </CardDescription>
            </div>
            <Button onClick={() => setIsCreateOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              Create Token
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {tokenList.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">No API tokens created yet</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Last Used</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tokenList.map((token: APIToken) => {
                  const exp = token.expires_at ? new Date(token.expires_at) : null
                  const isExpired = exp ? exp < new Date() : false
                  return (
                    <TableRow key={token.id}>
                      <TableCell className="font-medium">{token.name}</TableCell>
                      <TableCell>
                        <Badge variant={isExpired ? 'destructive' : 'default'}>
                          {isExpired ? 'Expired' : 'Active'}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {token.last_used
                          ? format(new Date(token.last_used), 'MMM d, yyyy')
                          : 'Never'}
                      </TableCell>
                      <TableCell>
                        {(() => {
                          const d = token.created_at ? new Date(token.created_at) : null
                          return d && !Number.isNaN(d.getTime()) ? format(d, 'MMM d, yyyy') : '-'
                        })()}
                      </TableCell>
                      <TableCell>
                        {(() => {
                          const d = token.expires_at ? new Date(token.expires_at) : null
                          return d && !Number.isNaN(d.getTime()) ? format(d, 'MMM d, yyyy') : '-'
                        })()}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          <Button
                            aria-label={`Delete Token ${token.name}`}
                            disabled={deleteTokenMutation.isPending}
                            onClick={() => deleteTokenMutation.mutate(token.id)}
                            size="sm"
                            variant="ghost"
                          >
                            <Trash2 className="h-4 w-4 text-destructive" />
                          </Button>
                          <Button
                            aria-label={`Edit Token ${token.name}`}
                            onClick={() => {
                              setEditToken(token)
                              setEditName(token.name)
                              setIsEditOpen(true)
                            }}
                            size="sm"
                            variant="ghost"
                          >
                            <Pencil className="h-4 w-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Create Token Dialog */}
      <Dialog onOpenChange={handleCloseDialog} open={isCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create API Token</DialogTitle>
            <DialogDescription>
              {createdToken
                ? "Copy this token now. You won't be able to see it again."
                : 'Enter a name for the new API token'}
            </DialogDescription>
          </DialogHeader>

          {createdToken ? (
            <div className="space-y-4">
              <div className="break-all rounded-md bg-muted p-3 font-mono text-sm">
                {createdToken}
              </div>
              <Button className="w-full" onClick={handleCopyToken}>
                <Copy className="mr-2 h-4 w-4" />
                Copy Token
              </Button>
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
                  onChange={(e) => setNewTokenName(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleCreateToken()}
                  placeholder="e.g., CI/CD Pipeline"
                  spellCheck={false}
                  value={newTokenName}
                />
              </div>
            </div>
          )}

          <DialogFooter>
            {createdToken ? (
              <Button onClick={handleCloseDialog}>Done</Button>
            ) : (
              <>
                <Button onClick={handleCloseDialog} variant="outline">
                  Cancel
                </Button>
                <Button disabled={createTokenMutation.isPending} onClick={handleCreateToken}>
                  {createTokenMutation.isPending ? 'Creating...' : 'Create Token'}
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Token Dialog */}
      <Dialog onOpenChange={setIsEditOpen} open={isEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rename API Token</DialogTitle>
            <DialogDescription>Update the display name for this token.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="edit-name">Token Name</Label>
              <Input
                id="edit-name"
                onChange={(e) => setEditName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && editToken) {
                    updateTokenMutation.mutate({ id: editToken.id, name: editName })
                  }
                }}
                value={editName}
              />
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setIsEditOpen(false)} variant="outline">
              Cancel
            </Button>
            <Button
              disabled={updateTokenMutation.isPending || !editToken || !editName.trim()}
              onClick={() =>
                editToken && updateTokenMutation.mutate({ id: editToken.id, name: editName })
              }
            >
              {updateTokenMutation.isPending ? 'Saving...' : 'Save'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
