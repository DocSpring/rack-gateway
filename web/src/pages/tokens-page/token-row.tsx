import { MoreVertical, Pencil, Trash2 } from 'lucide-react'
import { TimeAgo } from '@/components/time-ago'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../../components/ui/dropdown-menu'
import { TableCell, TableRow } from '../../components/ui/table'
import { UuidCell } from '../../components/uuid-cell'
import type { APIToken } from './types'

export function TokenRow({
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
