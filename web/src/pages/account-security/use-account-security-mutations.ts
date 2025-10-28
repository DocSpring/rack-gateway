import {
  DEFAULT_WEBAUTHN_LABEL,
  type EnrollmentState,
  getDefaultLabelForType,
  type MFAMethodType,
} from '@/components/account-security/types'
import { toast } from '@/components/ui/use-toast'
import { useMutation } from '@/hooks/use-mutation'
import {
  type BackupCodesResponse,
  confirmTOTPEnrollment,
  confirmWebAuthnEnrollment,
  deleteMFAMethod,
  regenerateBackupCodes,
  revokeTrustedDevice,
  startTOTPEnrollment,
  startWebAuthnEnrollment,
  trustCurrentDevice,
  updateMFAMethod,
  updatePreferredMFAMethod,
} from '@/lib/api'
import { getErrorMessage } from '@/lib/error-utils'
import {
  createCredential,
  prepareCreationOptions,
  serializeRegistrationCredential,
} from '@/lib/webauthn-utils'

type AccountSecurityMutationsDeps = {
  closeEnrollmentModal: () => void
  invalidateStatus: () => Promise<unknown> | undefined
  refreshUser: () => Promise<unknown>
  pendingEditMethod: { id: number; type: MFAMethodType } | null
  setPendingEditMethod: (value: { id: number; type: MFAMethodType } | null) => void
  setEnrollment: (value: EnrollmentState | null) => void
  setEnrollmentStep: (value: 'method-selection' | 'totp-setup') => void
  setVerificationCode: (value: string) => void
  setTrustEnrollmentDevice: (value: boolean) => void
  setRecentBackupCodes: (codes: string[] | null) => void
  setEditingMethod: (value: { id: number; label: string } | null) => void
  setEditLabel: (value: string) => void
  setOpenDropdownId: (value: number | null) => void
}

export function useAccountSecurityMutations({
  closeEnrollmentModal,
  invalidateStatus,
  refreshUser,
  pendingEditMethod,
  setPendingEditMethod,
  setEnrollment,
  setEnrollmentStep,
  setVerificationCode,
  setTrustEnrollmentDevice,
  setRecentBackupCodes,
  setEditingMethod,
  setEditLabel,
  setOpenDropdownId,
}: AccountSecurityMutationsDeps) {
  const startTOTPMutation = useMutation({
    mutationFn: startTOTPEnrollment,
    onSuccess: (data) => {
      setEnrollment({ ...data, enrollmentType: 'totp' })
      setVerificationCode('')
      setTrustEnrollmentDevice(true)
      setRecentBackupCodes(data.backup_codes ?? null)
      setEnrollmentStep('totp-setup')
      toast.success('MFA enrollment started')
    },
  })

  const startWebAuthnMutation = useMutation({
    mutationFn: startWebAuthnEnrollment,
    showToastError: false,
    onSuccess: async (data) => {
      try {
        if (!data.public_key_options) {
          throw new Error('No public key options received from server')
        }
        const webAuthnOptions = prepareCreationOptions(data.public_key_options)
        const credential = await createCredential({
          publicKey: webAuthnOptions,
        })
        if (!credential) {
          throw new Error('No credential created')
        }
        const credentialForBackend = serializeRegistrationCredential(
          credential as PublicKeyCredential
        )
        const methodId = data.method_id ?? 0
        await confirmWebAuthnEnrollment({
          method_id: methodId,
          credential: credentialForBackend,
          label: DEFAULT_WEBAUTHN_LABEL,
        })
        setRecentBackupCodes(data.backup_codes ?? null)
        toast.success('WebAuthn enrollment completed')
        if (methodId) {
          setEditingMethod({ id: methodId, label: DEFAULT_WEBAUTHN_LABEL })
          setEditLabel(DEFAULT_WEBAUTHN_LABEL)
        }
        setPendingEditMethod(null)
        closeEnrollmentModal()
        await invalidateStatus()
        refreshUser().catch(() => {
          /* noop */
        })
      } catch (error) {
        const msg = getErrorMessage(error)
        toast.error(`WebAuthn enrollment failed: ${msg}`)
        setEnrollment(null)
      }
    },
    onError: (error) => {
      const message = getErrorMessage(error)
      if (message.includes('not configured') || message.includes('WebAuthn')) {
        startTOTPMutation.mutate()
      } else {
        toast.error(message)
      }
    },
  })

  const confirmEnrollmentMutation = useMutation({
    mutationFn: confirmTOTPEnrollment,
    onSuccess: async () => {
      toast.success('Multi-factor authentication enabled')
      if (pendingEditMethod) {
        const defaultLabel = getDefaultLabelForType(pendingEditMethod.type)
        setEditingMethod({ id: pendingEditMethod.id, label: defaultLabel })
        setEditLabel(defaultLabel)
        setPendingEditMethod(null)
      }
      closeEnrollmentModal()
      await invalidateStatus()
      refreshUser().catch(() => {
        /* noop */
      })
    },
    onError: () => {
      setPendingEditMethod(null)
    },
  })

  const updateMethodMutation = useMutation({
    mutationFn: ({ methodId, label }: { methodId: number; label: string }) =>
      updateMFAMethod(methodId, { label }),
    onSuccess: () => {
      toast.success('MFA method updated')
      invalidateStatus()
      setEditingMethod(null)
      setEditLabel('')
    },
  })

  const deleteMethodMutation = useMutation({
    mutationFn: deleteMFAMethod,
    onSuccess: () => {
      toast.success('MFA method removed')
      invalidateStatus()
      refreshUser().catch(() => {
        /* noop */
      })
    },
    onSettled: () => {
      setOpenDropdownId(null)
    },
  })

  const revokeDeviceMutation = useMutation({
    mutationFn: revokeTrustedDevice,
    onSuccess: () => {
      toast.success('Trusted device revoked')
      invalidateStatus()
    },
  })

  const regenerateCodesMutation = useMutation({
    mutationFn: regenerateBackupCodes,
    onSuccess: (response: BackupCodesResponse) => {
      const codes = response.backup_codes ?? []
      setRecentBackupCodes(codes)
      toast.success('Backup codes regenerated')
      invalidateStatus()
    },
  })

  const trustDeviceMutation = useMutation({
    mutationFn: trustCurrentDevice,
    onSuccess: async (response) => {
      toast.success(
        response.trusted_device_cookie ? 'This device is now trusted' : 'Device trusted'
      )
      await invalidateStatus()
      refreshUser().catch(() => {
        /* noop */
      })
    },
  })

  const updatePreferredMethodMutation = useMutation({
    mutationFn: updatePreferredMFAMethod,
    onSuccess: async () => {
      await invalidateStatus()
      await refreshUser().catch(() => {
        /* noop */
      })
      toast.success('Preferred MFA method updated')
    },
  })

  return {
    startTOTPMutation,
    startWebAuthnMutation,
    confirmEnrollmentMutation,
    updateMethodMutation,
    deleteMethodMutation,
    revokeDeviceMutation,
    regenerateCodesMutation,
    trustDeviceMutation,
    updatePreferredMethodMutation,
  }
}
