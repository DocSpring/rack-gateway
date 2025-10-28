type ToastHandlers = {
  error: (message: string) => void
}

type LockUserRequest = {
  currentUserEmail: string | null
  targetEmail: string
  userLoaded: boolean
  setIsLockDialogOpen: (open: boolean) => void
  toastApi: ToastHandlers
}

export function requestLockUser({
  currentUserEmail,
  targetEmail,
  userLoaded,
  setIsLockDialogOpen,
  toastApi,
}: LockUserRequest): void {
  if (currentUserEmail && currentUserEmail === targetEmail) {
    toastApi.error("You can't lock your own account")
    return
  }

  if (!userLoaded) {
    toastApi.error('User is not loaded yet')
    return
  }

  setIsLockDialogOpen(true)
}

type DeleteUserRequest = {
  currentUserEmail: string | null
  targetEmail: string
  userLoaded: boolean
  setIsDeleteOpen: (open: boolean) => void
  toastApi: ToastHandlers
}

export function requestDeleteUser({
  currentUserEmail,
  targetEmail,
  userLoaded,
  setIsDeleteOpen,
  toastApi,
}: DeleteUserRequest): void {
  if (currentUserEmail && currentUserEmail === targetEmail) {
    toastApi.error("You can't delete your own account")
    return
  }

  if (!userLoaded) {
    toastApi.error('User is not loaded yet')
    return
  }

  setIsDeleteOpen(true)
}

export function toggleDeleteDialog({
  open,
  isPending,
  setIsDeleteOpen,
}: {
  open: boolean
  isPending: boolean
  setIsDeleteOpen: (open: boolean) => void
}): void {
  if (isPending) {
    return
  }

  setIsDeleteOpen(open)
}
