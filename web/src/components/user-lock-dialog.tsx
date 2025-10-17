import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
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
import { Label } from './ui/label'
import { Textarea } from './ui/textarea'

type UserLockDialogProps = {
  userEmail: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UserLockDialog({ userEmail, open, onOpenChange }: UserLockDialogProps) {
  const queryClient = useQueryClient()
  const [lockReason, setLockReason] = useState('')

  const lockUserMutation = useMutation({
    mutationFn: ({ email, reason }: { email: string; reason: string }) =>
      api.lockUser(email, reason),
    onSuccess: () => {
      toast.success('User account locked successfully')
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.USERS })
      queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.USER, userEmail] })
      onOpenChange(false)
      setLockReason('')
    },
    onError: (error: Error) => {
      const message = error.message
      toast.error(message || 'Failed to lock user account')
    },
  })

  const confirmLockUser = async () => {
    if (!lockReason.trim()) {
      toast.error('Lock reason is required')
      return
    }
    try {
      await lockUserMutation.mutateAsync({
        email: userEmail,
        reason: lockReason,
      })
    } catch {
      // Error handled by mutation onError
    }
  }

  const handleOpenChange = (isOpen: boolean) => {
    onOpenChange(isOpen)
    if (!isOpen) {
      setLockReason('')
    }
  }

  return (
    <Dialog onOpenChange={handleOpenChange} open={open}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Lock User Account</DialogTitle>
          <DialogDescription>
            This will immediately lock "{userEmail}" and revoke all active sessions. The user will
            not be able to log in until unlocked.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Label htmlFor="lock-reason">Reason for locking (required)</Label>
          <Textarea
            id="lock-reason"
            onChange={(e) => setLockReason(e.target.value)}
            placeholder="e.g., Security incident, suspicious activity..."
            value={lockReason}
          />
        </div>
        <DialogFooter>
          <Button
            disabled={lockUserMutation.isPending}
            onClick={() => handleOpenChange(false)}
            variant="outline"
          >
            Cancel
          </Button>
          <Button
            disabled={lockUserMutation.isPending || !lockReason.trim()}
            onClick={confirmLockUser}
            variant="destructive"
          >
            {lockUserMutation.isPending ? 'Locking Account...' : 'Lock Account'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function useUnlockUser() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (email: string) => api.unlockUser(email),
    onSuccess: (_, email) => {
      toast.success('User account unlocked successfully')
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.USERS })
      queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.USER, email] })
    },
    onError: (error: Error) => {
      const message = error.message
      toast.error(message || 'Failed to unlock user account')
    },
  })
}
