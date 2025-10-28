import { useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { useMemo } from 'react'

import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog'
import { toast } from '@/components/ui/use-toast'
import { UserEditDialog } from '@/components/user-edit-dialog'
import { UserLockDialog } from '@/components/user-lock-dialog'
import { useAuth } from '@/contexts/auth-context'
import type { RoleName } from '@/lib/api'
import { UserAuditLogsSection } from '@/pages/user/audit-logs-section'
import { UserHeaderSection } from '@/pages/user/header-section'
import { LockedNotice } from '@/pages/user/locked-notice'
import { UserSessionsCard } from '@/pages/user/sessions-card'
import { useUserAuditLogs } from '@/pages/user/use-user-audit-logs'
import { useUserDangerZone } from '@/pages/user/use-user-danger-zone'
import { useUserDetails } from '@/pages/user/use-user-details'
import { useUserEditing } from '@/pages/user/use-user-editing'
import { useUserSessions } from '@/pages/user/use-user-sessions'
import { useUserSessionsControl } from '@/pages/user/use-user-sessions-control'

export function UserPage() {
  const { email } = useParams({ from: '/users/$email' }) as { email: string }
  const decodedEmail = useMemo(() => decodeURIComponent(email), [email])
  const queryClient = useQueryClient()
  const { user: currentUser } = useAuth()
  const currentUserEmail = currentUser?.email ?? null

  const { user, userLoading, userError, currentPrimaryRole } = useUserDetails(decodedEmail)
  const { sessions, sessionsLoading, sessionsError } = useUserSessions(decodedEmail, Boolean(user))

  const {
    logs,
    totalCount,
    totalPages,
    firstRowIndex,
    lastRowIndex,
    loading: auditLoading,
    error: auditError,
    currentPage: currentAuditPage,
    goToPreviousPage: handleAuditPrevPage,
    goToNextPage: handleAuditNextPage,
  } = useUserAuditLogs(user ? decodedEmail : null, Boolean(user))

  const editing = useUserEditing({
    decodedEmail,
    user,
    currentPrimaryRole: (currentPrimaryRole ?? 'viewer') as RoleName,
    queryClient,
    toastApi: toast,
  })

  const sessionControl = useUserSessionsControl({
    decodedEmail,
    queryClient,
    toastApi: toast,
  })

  const dangerZone = useUserDangerZone({
    decodedEmail,
    user,
    currentUserEmail,
    queryClient,
    toastApi: toast,
  })

  if (userError) {
    return (
      <div className="space-y-4">
        <h1 className="font-semibold text-2xl">User</h1>
        <p className="text-destructive text-sm">Unable to load user: {userError.message}</p>
      </div>
    )
  }

  const dialogInitialEmail = editing.dialogInitialEmail
  const dialogInitialName = editing.dialogInitialName
  const dialogInitialRole = editing.dialogInitialRole

  return (
    <div className="space-y-8 p-8">
      <UserHeaderSection
        decodedEmail={decodedEmail}
        deletePending={dangerZone.deleteUserMutation.isPending}
        hasSessions={sessions.length > 0}
        isEditBusy={editing.isEditBusy}
        locked={Boolean(user?.locked_at)}
        onDelete={dangerZone.handleRequestDeleteUser}
        onEdit={editing.handleOpenEdit}
        onLock={dangerZone.handleRequestLockUser}
        onSignOutEverywhere={() => sessionControl.revokeAllMutation.mutate()}
        onUnlock={dangerZone.handleUnlockUser}
        signOutPending={sessionControl.revokeAllMutation.isPending}
        unlockPending={dangerZone.unlockUserMutation.isPending}
        user={user}
        userLoading={userLoading}
      />

      <div className="space-y-6">
        <LockedNotice
          lockedAt={user?.locked_at}
          lockedByEmail={user?.locked_by_email ?? null}
          lockedByName={user?.locked_by_name ?? null}
          lockedReason={user?.locked_reason ?? null}
        />

        <UserSessionsCard
          onRevoke={(id) => sessionControl.revokeSessionMutation.mutate(id)}
          pendingSessionId={sessionControl.pendingSessionId}
          revokeAllPending={sessionControl.revokeAllMutation.isPending}
          sessions={sessions}
          sessionsError={sessionsError}
          sessionsLoading={sessionsLoading}
          userLoading={userLoading}
        />

        <UserAuditLogsSection
          currentPage={currentAuditPage}
          error={auditError}
          firstRowIndex={firstRowIndex}
          lastRowIndex={lastRowIndex}
          loading={auditLoading}
          logs={logs}
          onNextPage={handleAuditNextPage}
          onPreviousPage={handleAuditPrevPage}
          totalCount={totalCount}
          totalPages={totalPages}
        />
      </div>

      <UserEditDialog
        busy={editing.isEditBusy}
        initialEmail={dialogInitialEmail}
        initialName={dialogInitialName}
        initialRole={dialogInitialRole}
        mode="edit"
        onOpenChange={editing.setIsEditOpen}
        onSubmit={editing.handleEditSubmit}
        open={editing.isEditOpen}
      />

      <ConfirmDeleteDialog
        busy={dangerZone.deleteUserMutation.isPending}
        confirmButtonText="Delete User"
        description={<>This action cannot be undone. Type DELETE to remove {decodedEmail}.</>}
        inputId="confirm-delete-user"
        onConfirm={dangerZone.confirmDeleteUser}
        onOpenChange={dangerZone.handleDeleteDialogOpenChange}
        open={dangerZone.isDeleteOpen}
        title="Delete User"
      />

      <UserLockDialog
        onOpenChange={dangerZone.handleLockDialogOpenChange}
        open={dangerZone.isLockDialogOpen}
        userEmail={dangerZone.userToLock?.email ?? decodedEmail}
      />
    </div>
  )
}
