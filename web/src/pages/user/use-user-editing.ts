import type { QueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useCallback, useState } from 'react'

import type { UserEditDialogValues } from '@/components/user-edit-dialog'
import { useMutation } from '@/hooks/use-mutation'
import type { GatewayUser, RoleName } from '@/lib/api'
import { api } from '@/lib/api'
import { QUERY_KEYS } from '@/lib/query-keys'

import type { EditPlan } from '@/pages/user/edit-plan'
import {
  executeEditPlan,
  performProfileUpdate,
  performRoleUpdate,
  submitUserEdits,
} from '@/pages/user/edit-plan'

type ToastApi = {
  error: (message: string) => void
  success: (message: string) => void
}

type UseUserEditingArgs = {
  decodedEmail: string
  user: GatewayUser | undefined
  currentPrimaryRole: RoleName
  queryClient: QueryClient
  toastApi: ToastApi
}

export function useUserEditing({
  decodedEmail,
  user,
  currentPrimaryRole,
  queryClient,
  toastApi,
}: UseUserEditingArgs) {
  const navigate = useNavigate()
  const [isEditOpen, setIsEditOpen] = useState(false)

  const updateProfileMutation = useMutation({
    mutationFn: async ({
      originalEmail,
      email: nextEmail,
      name,
    }: {
      originalEmail: string
      email: string
      name: string
    }) => {
      await api.put(`/api/v1/users/${encodeURIComponent(originalEmail)}`, {
        email: nextEmail,
        name,
      })
    },
  })

  const updateRolesMutation = useMutation({
    mutationFn: async ({ email: targetEmail, roles }: { email: string; roles: string[] }) => {
      await api.put(`/api/v1/users/${encodeURIComponent(targetEmail)}/roles`, {
        roles,
      })
    },
  })

  const isEditBusy = updateProfileMutation.isPending || updateRolesMutation.isPending

  const applyProfileUpdate = useCallback(
    (shouldUpdate: boolean, originalEmail: string, nextEmail: string, nextName: string) =>
      performProfileUpdate({
        shouldUpdate,
        mutate: updateProfileMutation.mutateAsync,
        originalEmail,
        nextEmail,
        nextName,
      }),
    [updateProfileMutation]
  )

  const applyRoleUpdate = useCallback(
    (shouldUpdate: boolean, targetEmail: string, roles: RoleName[]) =>
      performRoleUpdate({
        shouldUpdate,
        mutate: updateRolesMutation.mutateAsync,
        targetEmail,
        roles,
      }),
    [updateRolesMutation]
  )

  const invalidateUserData = useCallback(
    async (targetEmail: string) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.USER, targetEmail] }),
        queryClient.invalidateQueries({
          queryKey: [...QUERY_KEYS.USER_SESSIONS, targetEmail],
        }),
        queryClient.invalidateQueries({
          queryKey: [...QUERY_KEYS.USER_AUDIT_LOGS, targetEmail],
        }),
      ])
    },
    [queryClient]
  )

  const invalidateUsersList = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: QUERY_KEYS.USERS })
  }, [queryClient])

  const navigateToUser = useCallback(
    async (emailToNavigate: string) => {
      await navigate({
        to: '/users/$email',
        params: { email: emailToNavigate },
        replace: true,
      })
    },
    [navigate]
  )

  const executePlan = useCallback(
    (plan: EditPlan) =>
      executeEditPlan(plan, {
        applyProfileUpdate,
        applyRoleUpdate,
        invalidateUserData,
        invalidateUsersList,
        navigateToUser,
      }),
    [applyProfileUpdate, applyRoleUpdate, invalidateUserData, invalidateUsersList, navigateToUser]
  )

  const handleOpenEdit = useCallback(() => {
    setIsEditOpen(true)
  }, [])

  const handleEditSubmit = useCallback(
    (values: UserEditDialogValues) =>
      submitUserEdits({
        user,
        decodedEmail,
        values,
        toastApi,
        executePlan,
      }),
    [decodedEmail, executePlan, toastApi, user]
  )

  const dialogInitialEmail = user?.email ?? decodedEmail
  const dialogInitialName = user?.name ?? decodedEmail
  const dialogInitialRole = user ? currentPrimaryRole : ('viewer' as RoleName)

  return {
    isEditOpen,
    setIsEditOpen,
    isEditBusy,
    handleOpenEdit,
    handleEditSubmit,
    dialogInitialEmail,
    dialogInitialName,
    dialogInitialRole,
  }
}
