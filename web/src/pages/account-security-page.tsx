import { ShieldAlert } from 'lucide-react'
import { BackupCodesCard } from '@/components/account-security/backup-codes-card'
import { EditMfaMethodDialog } from '@/components/account-security/edit-mfa-method-dialog'
import { EnrollmentDialog } from '@/components/account-security/enrollment-dialog'
import { MfaMethodsTable } from '@/components/account-security/mfa-methods-table'
import { MfaStatusCard } from '@/components/account-security/mfa-status-card'
import { TrustedDevicesCard } from '@/components/account-security/trusted-devices-card'
import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useAccountSecurityPage } from '@/pages/account-security/use-account-security'

export function AccountSecurityPage() {
  const {
    status,
    user,
    methods,
    trustedDevices,
    backupSummary,
    showBackupCard,
    hasBothMethods,
    showCliEnrollmentWarning,
    disableDialogOpen,
    setDisableDialogOpen,
    disableAllPending,
    deleteMethodPending,
    updatePreferredMethodPending,
    updateMethodPending,
    startTotpPending,
    startWebAuthnPending,
    confirmEnrollmentPending,
    regenerateCodesPending,
    trustDevicePending,
    enrollmentModalOpen,
    enrollmentStep,
    enrollment,
    recentBackupCodes,
    qrDataUrl,
    verificationCode,
    setVerificationCode,
    trustEnrollmentDevice,
    setTrustEnrollmentDevice,
    openDropdownId,
    setOpenDropdownId,
    editingMethod,
    editLabel,
    setEditLabel,
    editCliCapable,
    setEditCliCapable,
    handlePreferredMethodChange,
    handleDisableAllMfa,
    handleRegenerateCodes,
    handleDownloadCodes,
    handleCopy,
    handleCopyCodes,
    handleMethodEdit,
    handleMethodRemove,
    handleRevokeDevice,
    handleTrustDevice,
    handleEnrollmentOpenChange,
    handleEnrollmentCancel,
    handleEnrollmentBack,
    handleConfirmEnrollment,
    handleStartEnrollment,
    openMethodEnrollment,
    handleEditDialogCancel,
    handleEditDialogSubmit,
    isBusy,
  } = useAccountSecurityPage()

  return (
    <div className="space-y-8 p-8">
      <div className="space-y-2">
        <h1 className="font-bold text-3xl">Account Security</h1>
        <p className="text-muted-foreground">
          Manage multi-factor authentication and trusted devices.
        </p>
      </div>

      {showCliEnrollmentWarning ? (
        <Alert className="border-amber-500 bg-amber-500/10">
          <ShieldAlert className="size-4" />
          <div className="pl-7 font-semibold">MFA enrollment required for CLI login</div>
          <AlertDescription>
            Set up multi-factor authentication below to continue. Once enrollment is complete, rerun{' '}
            <span className="font-mono">rack-gateway login</span> in your terminal.
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-6 lg:grid-cols-2">
        <MfaStatusCard
          disableButtonDisabled={methods.length === 0 || deleteMethodPending || disableAllPending}
          enableButtonDisabled={startTotpPending || startWebAuthnPending || Boolean(enrollment)}
          enrollmentInProgress={Boolean(enrollment)}
          hasBothMethods={hasBothMethods}
          onDisableClick={() => setDisableDialogOpen(true)}
          onEnableClick={openMethodEnrollment}
          onPreferredMethodChange={handlePreferredMethodChange}
          preferredMethod={user?.preferred_mfa_method || 'totp'}
          preferredMethodPending={updatePreferredMethodPending}
          status={status}
        />
        {showBackupCard ? (
          <BackupCodesCard
            backupSummary={backupSummary}
            onDownloadCodes={handleDownloadCodes}
            onRegenerate={handleRegenerateCodes}
            recentBackupCodes={recentBackupCodes}
            regeneratePending={regenerateCodesPending}
          />
        ) : null}
      </div>

      {methods.length > 0 ? (
        <MfaMethodsTable
          addMethodDisabled={startTotpPending || startWebAuthnPending || Boolean(enrollment)}
          methods={methods}
          onAddMethod={openMethodEnrollment}
          onDropdownChange={setOpenDropdownId}
          onEditMethod={handleMethodEdit}
          onRemoveMethod={handleMethodRemove}
          openDropdownId={openDropdownId}
        />
      ) : null}

      <TrustedDevicesCard
        devices={trustedDevices}
        onRevoke={handleRevokeDevice}
        onTrustCurrentDevice={handleTrustDevice}
        showTrustButton={!user?.has_trusted_device && Boolean(status?.enrolled)}
        trustDevicePending={trustDevicePending}
      />

      <EnrollmentDialog
        confirmEnrollmentPending={confirmEnrollmentPending}
        enrollment={enrollment}
        enrollmentStep={enrollmentStep}
        onBackToSelection={handleEnrollmentBack}
        onCancel={handleEnrollmentCancel}
        onConfirmEnrollment={handleConfirmEnrollment}
        onCopyCodes={handleCopyCodes}
        onCopySecret={handleCopy}
        onDownloadCodes={handleDownloadCodes}
        onOpenChange={handleEnrollmentOpenChange}
        onSelectMethod={handleStartEnrollment}
        onTrustEnrollmentDeviceChange={setTrustEnrollmentDevice}
        onVerificationCodeChange={setVerificationCode}
        open={enrollmentModalOpen}
        qrDataUrl={qrDataUrl}
        recentBackupCodes={recentBackupCodes}
        startTotpPending={startTotpPending}
        startWebAuthnPending={startWebAuthnPending}
        status={status}
        trustEnrollmentDevice={trustEnrollmentDevice}
        verificationCode={verificationCode}
      />

      <ConfirmDeleteDialog
        busy={disableAllPending || deleteMethodPending}
        busyText="Disabling..."
        confirmButtonText="Disable MFA"
        confirmText="DISABLE"
        description={
          <>
            Type DISABLE to remove all registered authenticators and turn off MFA for your account.
          </>
        }
        inputId="confirm-disable-mfa"
        onConfirm={handleDisableAllMfa}
        onOpenChange={setDisableDialogOpen}
        open={disableDialogOpen}
        title="Disable MFA"
      />

      <EditMfaMethodDialog
        cliCapable={editCliCapable}
        isSubmitting={updateMethodPending}
        label={editLabel}
        methodType={editingMethod?.type ?? 'totp'}
        onCancel={handleEditDialogCancel}
        onCliCapableChange={setEditCliCapable}
        onLabelChange={setEditLabel}
        onSubmit={handleEditDialogSubmit}
        open={Boolean(editingMethod)}
      />

      {isBusy ? (
        <p className="text-muted-foreground text-sm">Loading latest security information…</p>
      ) : null}
    </div>
  )
}
