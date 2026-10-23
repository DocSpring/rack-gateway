import { useNavigate } from '@tanstack/react-router'
import { useCallback, useState } from 'react'
import { useUnlockUser } from '@/components/user-lock-dialog'
import { useMutation } from '@/hooks/use-mutation'
import type { GatewayUser } from '@/lib/api'
import { api } from '@/lib/api'
import { apiPath } from '@/lib/api-path'
import { QUERY_KEYS } from '@/lib/query-keys'
import {
  requestDeleteUser,
  requestLockUser,
  toggleDeleteDialog,
} from '@/pages/user/user-dialog-actions'

interface ToastApi {
  error: (message: string) => void
  success: (message: string) => void
}

interface UseUserDangerZoneArgs {
  decodedEmail: string
  user: GatewayUser | undefined
  currentUserEmail: string | null
  queryClient: import('@tanstack/react-query').QueryClient
  toastApi: ToastApi
}

export function useUserDangerZone({
  decodedEmail,
  user,
  currentUserEmail,
  queryClient,
  toastApi,
}: UseUserDangerZoneArgs) {
  const navigate = useNavigate()
  const [isDeleteOpen, setIsDeleteOpen] = useState(false)
  const [userToDelete, setUserToDelete] = useState<GatewayUser | null>(null)
  const [isLockDialogOpen, setIsLockDialogOpen] = useState(false)
  const [userToLock, setUserToLock] = useState<GatewayUser | null>(null)
  const [isResetMFAOpen, setIsResetMFAOpen] = useState(false)
  const [isResetSignInOpen, setIsResetSignInOpen] = useState(false)

  const unlockUserMutation = useUnlockUser()

  const deleteUserMutation = useMutation({
    mutationFn: () => api.delete(apiPath`/api/v1/users/${decodedEmail}`),
    onSuccess: () => {
      toastApi.success('User deleted successfully')
      setIsDeleteOpen(false)
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.USERS })
      queryClient.removeQueries({ queryKey: [...QUERY_KEYS.USER, decodedEmail] })
      queryClient.removeQueries({ queryKey: [...QUERY_KEYS.USER_SESSIONS, decodedEmail] })
      queryClient.removeQueries({ queryKey: [...QUERY_KEYS.USER_AUDIT_LOGS, decodedEmail] })
      navigate({ to: '/users', replace: true })
    },
    onError: (error: unknown) => {
      toastApi.error(error instanceof Error ? error.message : 'Failed to delete user')
    },
  })

  const handleRequestLockUser = useCallback(() => {
    requestLockUser({
      currentUserEmail,
      targetEmail: decodedEmail,
      userLoaded: Boolean(user),
      setIsLockDialogOpen,
      toastApi,
    })
    if (user && (!currentUserEmail || currentUserEmail !== decodedEmail)) {
      setUserToLock(user)
    }
  }, [currentUserEmail, decodedEmail, toastApi, user])

  const handleUnlockUser = useCallback(
    () => unlockUserMutation.mutateAsync(decodedEmail),
    [decodedEmail, unlockUserMutation]
  )

  const handleRequestDeleteUser = useCallback(() => {
    requestDeleteUser({
      currentUserEmail,
      targetEmail: decodedEmail,
      userLoaded: Boolean(user),
      setIsDeleteOpen,
      toastApi,
    })
    if (user && (!currentUserEmail || currentUserEmail !== decodedEmail)) {
      setUserToDelete(user)
    }
  }, [currentUserEmail, decodedEmail, toastApi, user])

  const handleDeleteDialogOpenChange = useCallback(
    (open: boolean) => {
      toggleDeleteDialog({
        open,
        isPending: deleteUserMutation.isPending,
        setIsDeleteOpen,
      })
      if (!open) {
        setUserToDelete(null)
      }
    },
    [deleteUserMutation.isPending]
  )

  const confirmDeleteUser = useCallback(() => {
    if (!userToDelete) {
      return
    }
    deleteUserMutation.mutate()
  }, [deleteUserMutation, userToDelete])

  const handleLockDialogOpenChange = useCallback((open: boolean) => {
    setIsLockDialogOpen(open)
    if (!open) {
      setUserToLock(null)
    }
  }, [])

  return {
    isDeleteOpen,
    userToDelete,
    handleRequestDeleteUser,
    handleDeleteDialogOpenChange,
    confirmDeleteUser,
    deleteUserMutation,
    isLockDialogOpen,
    userToLock,
    handleLockDialogOpenChange,
    handleRequestLockUser,
    handleUnlockUser,
    unlockUserMutation,
    // Admins reset another user's MFA; their own is managed on the Account Security page.
    canResetMFA: Boolean(user?.mfa_enrolled) && currentUserEmail !== decodedEmail,
    isResetMFAOpen,
    setIsResetMFAOpen,
    // Admins reset another user's sign-in account, never their own.
    canResetSignIn: Boolean(user) && currentUserEmail !== decodedEmail,
    isResetSignInOpen,
    setIsResetSignInOpen,
  }
}
