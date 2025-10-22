import { useQuery } from '@tanstack/react-query'
import { QUERY_KEYS } from '@/lib/query-keys'
import { ConfirmDeleteDialog } from '../../components/confirm-delete-dialog'
import type { APIToken } from './types'
import { useTokenMutations } from './use-token-mutations'

export function DeleteTokenDialog({
  tokenId,
  isOpen,
  onClose,
}: {
  tokenId: string | null
  isOpen: boolean
  onClose: () => void
}) {
  const { deleteToken, handleStepUpError } = useTokenMutations()

  const { data: tokensData } = useQuery<APIToken[]>({
    queryKey: QUERY_KEYS.TOKENS,
    enabled: false, // Just reading from cache, not fetching
  })

  const tokens = Array.isArray(tokensData) ? tokensData : []

  const token = tokens.find((t) => t.public_id === tokenId)

  const handleConfirmDelete = async () => {
    if (!token) {
      return
    }
    try {
      await deleteToken.mutateAsync(token.public_id)
      onClose()
    } catch (err) {
      handleStepUpError(err, () => deleteToken.mutateAsync(token.public_id))
    }
  }

  return (
    <ConfirmDeleteDialog
      busy={deleteToken.isPending}
      confirmButtonText="Delete Token"
      description={<>This action cannot be undone. Type DELETE to remove "{token?.name}".</>}
      inputId="confirm-delete-token"
      onConfirm={() => {
        handleConfirmDelete().catch(() => {
          /* errors handled in handler */
        })
      }}
      onOpenChange={(open) => {
        if (!open) {
          onClose()
        }
      }}
      open={isOpen}
      title="Delete API Token"
    />
  )
}
