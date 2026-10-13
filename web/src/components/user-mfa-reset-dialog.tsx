import { useQueryClient } from '@tanstack/react-query'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import { api } from '@/lib/api'
import { QUERY_KEYS } from '@/lib/query-keys'
import { Button } from './ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog'

type UserMFAResetDialogProps = {
  userEmail: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

// Resets a user's MFA, for a user who has lost every method and their backup codes. Their API tokens are kept.
export function UserMFAResetDialog({ userEmail, open, onOpenChange }: UserMFAResetDialogProps) {
  const queryClient = useQueryClient()

  const resetMutation = useMutation({
    mutationFn: (email: string) => api.resetUserMFA(email),
    onSuccess: () => {
      toast.success('MFA reset. The user sets up a new method when they next sign in.')
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.USERS })
      queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.USER, userEmail] })
      queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.USER_SESSIONS, userEmail] })
      onOpenChange(false)
    },
  })

  const confirmReset = async () => {
    try {
      await resetMutation.mutateAsync(userEmail)
    } catch {
      // The mutation shows the error
    }
  }

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reset MFA</DialogTitle>
          <DialogDescription>
            This removes the MFA methods, backup codes and trusted devices of "{userEmail}" and
            signs them out everywhere. Their API tokens are kept. They set up a new method when they
            next sign in, and they're emailed about the reset.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            disabled={resetMutation.isPending}
            onClick={() => onOpenChange(false)}
            variant="outline"
          >
            Cancel
          </Button>
          <Button disabled={resetMutation.isPending} onClick={confirmReset} variant="destructive">
            {resetMutation.isPending ? 'Resetting MFA...' : 'Reset MFA'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
