import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { format } from 'date-fns'
import { Copy, Plus, Trash2, RefreshCw } from 'lucide-react'
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
import { Badge } from '../components/ui/badge'
import { toast } from 'sonner'
import { api } from '../lib/api'

interface APIToken {
  id: string
  name: string
  token?: string
  last_used: string | null
  created_at: string
  expires_at: string
}

export function TokensPage() {
  const queryClient = useQueryClient()
  const [isCreateOpen, setIsCreateOpen] = useState(false)
  const [newTokenName, setNewTokenName] = useState('')
  const [createdToken, setCreatedToken] = useState<string | null>(null)

  // Fetch tokens
  const { data: tokens = [], isLoading, error, refetch } = useQuery({
    queryKey: ['tokens'],
    queryFn: async () => {
      const response = await api.get<APIToken[]>('/.gateway/admin/tokens')
      return response
    },
  })

  // Create token mutation
  const createTokenMutation = useMutation({
    mutationFn: async (name: string) => {
      const response = await api.post<APIToken>('/.gateway/admin/tokens', { name })
      return response
    },
    onSuccess: (data) => {
      setCreatedToken(data.token || '')
      queryClient.invalidateQueries({ queryKey: ['tokens'] })
      toast.success('API token created successfully')
    },
    onError: (error: any) => {
      toast.error(error.message || 'Failed to create token')
    },
  })

  // Delete token mutation
  const deleteTokenMutation = useMutation({
    mutationFn: async (tokenId: string) => {
      await api.delete(`/.gateway/admin/tokens/${tokenId}`)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tokens'] })
      toast.success('Token deleted successfully')
    },
    onError: (error: any) => {
      toast.error(error.message || 'Failed to delete token')
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
            <CardDescription>Failed to load API tokens</CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="text-3xl font-bold">API Tokens</h1>
        <p className="text-muted-foreground mt-2">
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
          {tokens.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No API tokens created yet
            </div>
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
                {tokens.map((token) => {
                  const isExpired = new Date(token.expires_at) < new Date()
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
                        {format(new Date(token.created_at), 'MMM d, yyyy')}
                      </TableCell>
                      <TableCell>
                        {format(new Date(token.expires_at), 'MMM d, yyyy')}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => deleteTokenMutation.mutate(token.id)}
                          disabled={deleteTokenMutation.isPending}
                        >
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
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
      <Dialog open={isCreateOpen} onOpenChange={handleCloseDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create API Token</DialogTitle>
            <DialogDescription>
              {createdToken
                ? 'Copy this token now. You won\'t be able to see it again.'
                : 'Enter a name for the new API token'}
            </DialogDescription>
          </DialogHeader>

          {createdToken ? (
            <div className="space-y-4">
              <div className="p-3 bg-muted rounded-md font-mono text-sm break-all">
                {createdToken}
              </div>
              <Button onClick={handleCopyToken} className="w-full">
                <Copy className="mr-2 h-4 w-4" />
                Copy Token
              </Button>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="name">Token Name</Label>
                <Input
                  id="name"
                  placeholder="e.g., CI/CD Pipeline"
                  value={newTokenName}
                  onChange={(e) => setNewTokenName(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleCreateToken()}
                />
              </div>
            </div>
          )}

          <DialogFooter>
            {createdToken ? (
              <Button onClick={handleCloseDialog}>Done</Button>
            ) : (
              <>
                <Button variant="outline" onClick={handleCloseDialog}>
                  Cancel
                </Button>
                <Button
                  onClick={handleCreateToken}
                  disabled={createTokenMutation.isPending}
                >
                  {createTokenMutation.isPending ? 'Creating...' : 'Create Token'}
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}