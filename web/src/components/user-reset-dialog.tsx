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

// What an admin can reset for another user. Both sign the user out and keep their API tokens.
export type UserReset = 'mfa' | 'sign-in'

interface ResetCopy {
  title: string
  pending: string
  success: string
  description: (email: string) => string
  run: (email: string) => Promise<unknown>
}

const RESETS: Record<UserReset, ResetCopy> = {
  // For a user who has lost every MFA method and their backup codes.
  mfa: {
    title: 'Reset MFA',
    pending: 'Resetting MFA...',
    success: 'MFA reset. The user sets up a new method when they next sign in.',
    description: (email) =>
      `This removes the MFA methods, backup codes and trusted devices of "${email}" and signs them out everywhere. Their API tokens are kept. They set up a new method when they next sign in, and they're emailed about the reset.`,
    run: (email) => api.resetUserMFA(email),
  },
  // A user is bound to the Google account that first signed in as them; this lets a new one in, for example after
  // their Google account was re-created.
  'sign-in': {
    title: 'Reset Sign-in',
    pending: 'Resetting sign-in...',
    success:
      'Sign-in reset. The next Google account to sign in with this email is bound to the user.',
    description: (email) =>
      `"${email}" can only sign in with the Google account that first signed in as them. This unbinds it, so the next Google account to sign in with this email is bound to the user, and signs them out everywhere. Their MFA methods and API tokens are kept. If the address now belongs to someone else, delete the user instead.`,
    run: (email) => api.resetUserSignIn(email),
  },
}

interface UserResetDialogProps {
  reset: UserReset
  userEmail: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UserResetDialog({ reset, userEmail, open, onOpenChange }: UserResetDialogProps) {
  const queryClient = useQueryClient()
  const copy = RESETS[reset]

  const resetMutation = useMutation({
    mutationFn: (email: string) => copy.run(email),
    onSuccess: () => {
      toast.success(copy.success)
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
          <DialogTitle>{copy.title}</DialogTitle>
          <DialogDescription>{copy.description(userEmail)}</DialogDescription>
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
            {resetMutation.isPending ? copy.pending : copy.title}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
