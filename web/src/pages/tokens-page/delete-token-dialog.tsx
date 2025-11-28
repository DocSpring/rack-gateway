import { useQuery } from '@tanstack/react-query'
import { QUERY_KEYS } from '@/lib/query-keys'
import { ConfirmDeleteDialog } from '../../components/confirm-delete-dialog'
import { useStepUp } from '../../contexts/step-up-context'
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
  const { deleteToken } = useTokenMutations()
  const { runWithMFAGuard } = useStepUp()

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
    // runWithMFAGuard throws on MFA cancel, so if we reach onClose() the deletion succeeded
    await runWithMFAGuard(() => deleteToken.mutateAsync(token.public_id))
    onClose()
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
