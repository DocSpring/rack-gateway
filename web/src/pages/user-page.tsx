import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { Edit2, Lock, RefreshCw, Trash2, Unlock } from 'lucide-react'
import { useMemo, useState } from 'react'
import { AuditLogsPane } from '@/components/audit-logs-pane'
import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import type { UserEditDialogValues } from '@/components/user-edit-dialog'
import { UserEditDialog } from '@/components/user-edit-dialog'
import { UserLockDialog, useUnlockUser } from '@/components/user-lock-dialog'
import { useAuth } from '@/contexts/auth-context'
import { useMutation } from '@/hooks/use-mutation'
import type { GatewayUser, RoleName, UserSessionSummary } from '@/lib/api'
import { api } from '@/lib/api'
import { QUERY_KEYS } from '@/lib/query-keys'
import { pickPrimaryRole } from '@/lib/user-roles'
import { LockedNotice } from '@/pages/user/locked-notice'
import { RoleBadges } from '@/pages/user/role-badges'
import { type SessionId, SessionTable } from '@/pages/user/session-table'
import { useUserAuditLogs } from '@/pages/user/use-user-audit-logs'

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: User management screen orchestrates multiple flows
export function UserPage() {
  const { email } = useParams({ from: '/users/$email' }) as { email: string }
  const decodedEmail = useMemo(() => decodeURIComponent(email), [email])
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { user: currentUser } = useAuth()
  const [pendingSessionId, setPendingSessionId] = useState<SessionId | null>(null)
  const [isEditOpen, setIsEditOpen] = useState(false)
  const [isDeleteOpen, setIsDeleteOpen] = useState(false)
  const [isLockDialogOpen, setIsLockDialogOpen] = useState(false)

  const {
    data: user,
    isLoading: userLoading,
    error: userError,
  } = useQuery<GatewayUser, Error>({
    queryKey: [...QUERY_KEYS.USER, decodedEmail],
    queryFn: () => api.getUser(decodedEmail),
    retry: 1,
  })

  const currentPrimaryRole = useMemo(() => pickPrimaryRole(user?.roles ?? []), [user?.roles])

  const {
    data: sessions = [],
    isLoading: sessionsLoading,
    error: sessionsError,
  } = useQuery<UserSessionSummary[], Error>({
    queryKey: [...QUERY_KEYS.USER_SESSIONS, decodedEmail],
    queryFn: () => api.listUserSessions(decodedEmail),
    enabled: !!user,
    refetchOnWindowFocus: true,
  })

  const {
    logs: auditLogs,
    totalCount: auditTotal,
    currentPage: currentAuditPage,
    totalPages: auditTotalPages,
    firstRowIndex: auditFirstRowIndex,
    lastRowIndex: auditLastRowIndex,
    loading: auditLoading,
    error: auditError,
    goToPreviousPage: handleAuditPrevPage,
    goToNextPage: handleAuditNextPage,
  } = useUserAuditLogs(user ? decodedEmail : null, Boolean(user))

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

  type EditPlan = {
    originalEmail: string
    routeEmail: string
    trimmedEmail: string
    trimmedName: string
    desiredRoles: RoleName[]
    emailChanged: boolean
    profileChanged: boolean
    shouldUpdateRoles: boolean
  }

  const applyProfileUpdate = async (
    shouldUpdate: boolean,
    originalEmail: string,
    nextEmail: string,
    nextName: string
  ) => {
    if (!shouldUpdate) {
      return
    }
    await updateProfileMutation.mutateAsync({
      originalEmail,
      email: nextEmail,
      name: nextName,
    })
  }

  const applyRoleUpdate = async (shouldUpdate: boolean, targetEmail: string, roles: RoleName[]) => {
    if (!shouldUpdate) {
      return
    }
    await updateRolesMutation.mutateAsync({ email: targetEmail, roles })
  }

  const invalidateUserData = async (targetEmail: string) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.USER, targetEmail] }),
      queryClient.invalidateQueries({
        queryKey: [...QUERY_KEYS.USER_SESSIONS, targetEmail],
      }),
      queryClient.invalidateQueries({
        queryKey: [...QUERY_KEYS.USER_AUDIT_LOGS, targetEmail],
      }),
    ])
  }

  const handleOpenEdit = () => {
    setIsEditOpen(true)
  }

  const buildEditPlan = (
    existingUser: GatewayUser,
    values: UserEditDialogValues
  ): { error: string } | { plan: EditPlan } => {
    const trimmedEmail = values.email.trim()
    const trimmedName = values.name.trim()

    if (!(trimmedEmail && trimmedName)) {
      return { error: 'Email and name are required' }
    }

    const desiredRoles: RoleName[] = [values.role]
    const existingRoles = existingUser.roles ?? []
    const currentEmail = existingUser.email ?? ''
    const currentName = existingUser.name ?? ''
    const emailChanged = trimmedEmail !== currentEmail
    const profileChanged = emailChanged || trimmedName !== currentName
    const rolesChanged =
      existingRoles.length !== desiredRoles.length ||
      desiredRoles.some((role) => !existingRoles.includes(role))

    return {
      plan: {
        originalEmail: currentEmail,
        routeEmail: decodedEmail,
        trimmedEmail,
        trimmedName,
        desiredRoles,
        emailChanged,
        profileChanged,
        shouldUpdateRoles: rolesChanged || emailChanged,
      },
    }
  }

  const executeEditPlan = async (plan: EditPlan) => {
    await applyProfileUpdate(
      plan.profileChanged,
      plan.originalEmail,
      plan.trimmedEmail,
      plan.trimmedName
    )
    await applyRoleUpdate(plan.shouldUpdateRoles, plan.trimmedEmail, plan.desiredRoles)

    const invalidations: Promise<unknown>[] = [
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.USERS }),
      invalidateUserData(plan.routeEmail),
    ]
    if (plan.emailChanged) {
      invalidations.push(invalidateUserData(plan.trimmedEmail))
    }
    await Promise.all(invalidations)

    if (plan.emailChanged) {
      await navigate({
        to: '/users/$email',
        params: { email: plan.trimmedEmail },
        replace: true,
      })
    }
  }

  const handleEditSubmit = async ({
    email: nextEmail,
    name: nextName,
    role,
  }: UserEditDialogValues) => {
    if (!user) {
      return
    }

    const planResult = buildEditPlan(user, {
      email: nextEmail,
      name: nextName,
      role,
    })
    if ('error' in planResult) {
      toast.error(planResult.error)
      throw new Error(planResult.error)
    }

    try {
      await executeEditPlan(planResult.plan)
      toast.success('User updated successfully')
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to update user')
      throw error
    }
  }

  const revokeSessionMutation = useMutation({
    mutationFn: (sessionId: SessionId) => api.revokeUserSession(decodedEmail, sessionId),
    onMutate: (sessionId) => {
      setPendingSessionId(sessionId)
    },
    onSuccess: () => {
      toast.success('Session revoked')
      queryClient.invalidateQueries({
        queryKey: [...QUERY_KEYS.USER_SESSIONS, decodedEmail],
      })
    },
    onError: () => {
      toast.error('Failed to revoke session')
    },
    onSettled: () => {
      setPendingSessionId(null)
    },
  })

  const revokeAllMutation = useMutation({
    mutationFn: () => api.revokeAllUserSessions(decodedEmail),
    onSuccess: () => {
      toast.success('All sessions revoked')
      queryClient.invalidateQueries({
        queryKey: [...QUERY_KEYS.USER_SESSIONS, decodedEmail],
      })
    },
    onError: () => {
      toast.error('Failed to revoke sessions')
    },
  })

  const unlockUserMutation = useUnlockUser()

  const handleRequestLockUser = () => {
    if (currentUser?.email === decodedEmail) {
      toast.error("You can't lock your own account")
      return
    }
    setIsLockDialogOpen(true)
  }

  const handleUnlockUser = async () => {
    await unlockUserMutation.mutateAsync(decodedEmail)
  }

  const deleteUserMutation = useMutation({
    mutationFn: () => api.delete(`/api/v1/users/${encodeURIComponent(decodedEmail)}`),
    onSuccess: () => {
      toast.success('User deleted successfully')
      setIsDeleteOpen(false)
      queryClient.invalidateQueries({ queryKey: QUERY_KEYS.USERS })
      queryClient.removeQueries({ queryKey: [...QUERY_KEYS.USER, decodedEmail] })
      queryClient.removeQueries({ queryKey: [...QUERY_KEYS.USER_SESSIONS, decodedEmail] })
      queryClient.removeQueries({ queryKey: [...QUERY_KEYS.USER_AUDIT_LOGS, decodedEmail] })
      navigate({ to: '/users', replace: true })
    },
    onError: (error: unknown) => {
      toast.error(error instanceof Error ? error.message : 'Failed to delete user')
    },
  })

  const handleRequestDelete = () => {
    if (currentUser?.email === decodedEmail) {
      toast.error("You can't delete your own account")
      return
    }
    if (!user) {
      toast.error('User is not loaded yet')
      return
    }
    setIsDeleteOpen(true)
  }

  const handleDeleteDialogOpenChange = (open: boolean) => {
    if (deleteUserMutation.isPending) {
      return
    }
    setIsDeleteOpen(open)
  }

  const confirmDeleteUser = () => {
    if (!user || deleteUserMutation.isPending) {
      return
    }
    deleteUserMutation.mutate()
  }

  if (userError) {
    return (
      <div className="space-y-4">
        <h1 className="font-semibold text-2xl">User</h1>
        <p className="text-destructive text-sm">Unable to load user: {userError.message}</p>
      </div>
    )
  }

  const dialogInitialEmail = user?.email ?? decodedEmail
  const dialogInitialName = user?.name ?? decodedEmail
  const dialogInitialRole = user ? currentPrimaryRole : 'viewer'

  return (
    <div className="space-y-8 p-8">
      <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div>
          <h1 className="font-semibold text-3xl">
            {userLoading ? 'Loading…' : user?.name || decodedEmail}
          </h1>
          <p className="text-muted-foreground" data-testid="user-email">
            {decodedEmail}
          </p>
          {user?.roles && user.roles.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-2">
              <RoleBadges roles={user.roles ?? []} />
            </div>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2 md:justify-end">
          <Button
            disabled={userLoading || !user || isEditBusy}
            onClick={handleOpenEdit}
            variant="secondary"
          >
            <Edit2 className="mr-2 h-4 w-4" /> Edit
          </Button>
          {user?.locked_at ? (
            <Button
              disabled={unlockUserMutation.isPending || userLoading}
              onClick={handleUnlockUser}
              variant="secondary"
            >
              {unlockUserMutation.isPending ? (
                <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <Unlock className="mr-2 h-4 w-4" />
              )}
              Unlock Account
            </Button>
          ) : (
            <Button
              disabled={userLoading || !user}
              onClick={handleRequestLockUser}
              variant="secondary"
            >
              <Lock className="mr-2 h-4 w-4" />
              Lock Account
            </Button>
          )}
          <Button
            disabled={revokeAllMutation.isPending || userLoading || sessions.length === 0}
            onClick={() => revokeAllMutation.mutate()}
            variant="destructive"
          >
            {revokeAllMutation.isPending && <RefreshCw className="mr-2 h-4 w-4 animate-spin" />}
            Sign Out Everywhere
          </Button>
          <Button
            disabled={userLoading || !user || deleteUserMutation.isPending}
            onClick={handleRequestDelete}
            variant="destructive"
          >
            {deleteUserMutation.isPending ? (
              <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
            ) : (
              <Trash2 className="mr-2 h-4 w-4" />
            )}
            Delete User
          </Button>
        </div>
      </div>

      <div className="space-y-6">
        <LockedNotice
          lockedAt={user?.locked_at}
          lockedByEmail={user?.locked_by_email ?? null}
          lockedByName={user?.locked_by_name ?? null}
          lockedReason={user?.locked_reason ?? null}
        />

        <Card data-testid="user-sessions-card">
          <CardHeader>
            <CardTitle>Active Sessions</CardTitle>
          </CardHeader>
          <CardContent>
            <SessionTable
              disableActions={revokeAllMutation.isPending}
              error={sessionsError ? 'Failed to load sessions' : null}
              loading={sessionsLoading || userLoading}
              onRevoke={(id) => revokeSessionMutation.mutate(id)}
              pendingSessionId={pendingSessionId}
              sessions={sessions}
            />
          </CardContent>
        </Card>

        <div data-testid="user-audit-logs">
          <AuditLogsPane
            currentPage={currentAuditPage}
            disableNext={currentAuditPage >= auditTotalPages}
            disablePrevious={currentAuditPage <= 1}
            emptyMessage="No audit logs for this user"
            error={auditError}
            firstRowIndex={auditFirstRowIndex}
            lastRowIndex={auditLastRowIndex}
            loading={auditLoading}
            logs={auditLogs}
            onNextPage={handleAuditNextPage}
            onPreviousPage={handleAuditPrevPage}
            title="Audit Logs"
            totalCount={auditTotal}
            totalPages={auditTotalPages}
          />
        </div>
      </div>

      <UserEditDialog
        busy={isEditBusy}
        initialEmail={dialogInitialEmail}
        initialName={dialogInitialName}
        initialRole={dialogInitialRole}
        mode="edit"
        onOpenChange={setIsEditOpen}
        onSubmit={handleEditSubmit}
        open={isEditOpen}
      />

      <ConfirmDeleteDialog
        busy={deleteUserMutation.isPending}
        confirmButtonText="Delete User"
        description={<>This action cannot be undone. Type DELETE to remove {decodedEmail}.</>}
        inputId="confirm-delete-user"
        onConfirm={confirmDeleteUser}
        onOpenChange={handleDeleteDialogOpenChange}
        open={isDeleteOpen}
        title="Delete User"
      />

      <UserLockDialog
        onOpenChange={setIsLockDialogOpen}
        open={isLockDialogOpen}
        userEmail={decodedEmail}
      />
    </div>
  )
}
