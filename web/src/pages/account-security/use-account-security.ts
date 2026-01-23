import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation } from '@tanstack/react-router'
import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  type EnrollmentState,
  getDefaultLabelForType,
  type MFAMethodType,
} from '@/components/account-security/types'
import { formatCodeForDownload } from '@/components/account-security/utils'
import { toast } from '@/components/ui/use-toast'
import { useAuth } from '@/contexts/auth-context'
import { useStepUp } from '@/contexts/step-up-context'
import { getMFAStatus, type MFAStatusResponse } from '@/lib/api'
import { normalizeRedirectPath } from '@/lib/navigation'
import { QUERY_KEYS } from '@/lib/query-keys'
import { resolveWebRedirect } from '@/lib/routes'
import { useAccountSecurityMutations } from '@/pages/account-security/use-account-security-mutations'
import { useMFAEnrollment } from '@/pages/account-security/use-mfa-enrollment'
import { useMFAMethodEdit } from '@/pages/account-security/use-mfa-method-edit'

type MFAMethodRecord = NonNullable<MFAStatusResponse['methods']>[number]
type TrustedDeviceRecord = NonNullable<MFAStatusResponse['trusted_devices']>[number]

type UseAccountSecurityPageResult = {
  status?: MFAStatusResponse
  user: ReturnType<typeof useAuth>['user']
  methods: MFAMethodRecord[]
  trustedDevices: TrustedDeviceRecord[]
  backupSummary: MFAStatusResponse['backup_codes'] | undefined
  showBackupCard: boolean
  hasBothMethods: boolean
  showCliEnrollmentWarning: boolean
  disableDialogOpen: boolean
  setDisableDialogOpen: (open: boolean) => void
  disableAllPending: boolean
  deleteMethodPending: boolean
  updatePreferredMethodPending: boolean
  updateMethodPending: boolean
  startTotpPending: boolean
  startWebAuthnPending: boolean
  confirmEnrollmentPending: boolean
  regenerateCodesPending: boolean
  trustDevicePending: boolean
  enrollmentModalOpen: boolean
  enrollmentStep: 'method-selection' | 'totp-setup'
  enrollment: EnrollmentState | null
  recentBackupCodes: string[] | null
  qrDataUrl: string | null
  verificationCode: string
  setVerificationCode: (value: string) => void
  trustEnrollmentDevice: boolean
  setTrustEnrollmentDevice: (value: boolean) => void
  openDropdownId: number | null
  setOpenDropdownId: (id: number | null) => void
  editingMethod: { id: number; label: string; type: string; cliCapable: boolean } | null
  editLabel: string
  setEditLabel: (label: string) => void
  editCliCapable: boolean
  setEditCliCapable: (value: boolean) => void
  handlePreferredMethodChange: (value: string) => void
  handleDisableAllMfa: () => void
  handleRegenerateCodes: () => void
  handleDownloadCodes: (codes: string[]) => void
  handleCopy: (value: string) => Promise<void>
  handleCopyCodes: (codes: string[]) => Promise<void>
  handleMethodEdit: (method: MFAMethodRecord) => void
  handleMethodRemove: (method: MFAMethodRecord) => void
  handleRevokeDevice: (device: TrustedDeviceRecord) => void
  handleTrustDevice: () => void
  handleEnrollmentOpenChange: (open: boolean) => void
  handleEnrollmentCancel: () => void
  handleEnrollmentBack: () => void
  handleConfirmEnrollment: () => void
  handleStartEnrollment: (type: MFAMethodType) => void
  openMethodEnrollment: () => void
  handleEditDialogCancel: () => void
  handleEditDialogSubmit: () => void
  isBusy: boolean
}

export function useAccountSecurityPage(): UseAccountSecurityPageResult {
  const queryClient = useQueryClient()
  const location = useLocation()
  const { refresh: refreshUser, user } = useAuth()
  const { openStepUp, handleStepUpError, isVerifying: isGlobalStepUpVerifying } = useStepUp()
  const [recentBackupCodes, setRecentBackupCodes] = useState<string[] | null>(null)
  const [disableDialogOpen, setDisableDialogOpen] = useState(false)
  const [disableAllPending, setDisableAllPending] = useState(false)
  const [autoPrompted, setAutoPrompted] = useState(false)
  const [openDropdownId, setOpenDropdownId] = useState<number | null>(null)

  const enrollment = useMFAEnrollment()
  const methodEdit = useMFAMethodEdit()

  const searchParams = useMemo(() => new URLSearchParams(location.search ?? ''), [location.search])
  const promptMfa = searchParams.get('mfa') === 'verify'
  const redirectParam = searchParams.get('redirect')
  const redirectTarget = useMemo(() => normalizeRedirectPath(redirectParam), [redirectParam])
  const enrollmentRequiredFlag = searchParams.get('enrollment') === 'required'
  const enrollmentChannel = searchParams.get('channel') ?? undefined
  const cliState = searchParams.get('state') ?? undefined

  const {
    data: status,
    isLoading,
    isFetching,
  } = useQuery<MFAStatusResponse>({
    queryKey: QUERY_KEYS.MFA_STATUS,
    queryFn: getMFAStatus,
    refetchOnWindowFocus: true,
    staleTime: 30_000,
  })

  const needsStepUp = useMemo(() => {
    if (!status?.recent_step_up_expires_at) {
      return true
    }
    const expires = new Date(status.recent_step_up_expires_at).getTime()
    return Number.isNaN(expires) || expires <= Date.now()
  }, [status?.recent_step_up_expires_at])

  const invalidateStatus = useCallback(
    () => queryClient.invalidateQueries({ queryKey: QUERY_KEYS.MFA_STATUS }),
    [queryClient]
  )

  const {
    startTOTPMutation,
    startWebAuthnMutation,
    confirmEnrollmentMutation,
    updateMethodMutation,
    deleteMethodMutation,
    revokeDeviceMutation,
    regenerateCodesMutation,
    trustDeviceMutation,
    updatePreferredMethodMutation,
  } = useAccountSecurityMutations({
    closeEnrollmentModal: enrollment.closeEnrollmentModal,
    invalidateStatus,
    refreshUser,
    pendingEditMethod: enrollment.pendingEditMethod,
    setPendingEditMethod: enrollment.setPendingEditMethod,
    setEnrollment: enrollment.setEnrollment,
    setEnrollmentStep: enrollment.setEnrollmentStep,
    setVerificationCode: enrollment.setVerificationCode,
    setTrustEnrollmentDevice: enrollment.setTrustEnrollmentDevice,
    setRecentBackupCodes,
    setEditingMethod: methodEdit.setEditingMethod,
    setEditLabel: methodEdit.setEditLabel,
    setOpenDropdownId,
    redirectTarget,
    enrollmentChannel,
    cliState,
  })

  const runWithStepUp = useCallback(
    (action: () => Promise<void>) => {
      const wrappedAction = async () => {
        await action()
      }
      const actionWithStatusRefresh = async () => {
        await invalidateStatus()
        await wrappedAction()
      }
      wrappedAction().catch((error) => {
        handleStepUpError(error, actionWithStatusRefresh)
      })
    },
    [handleStepUpError, invalidateStatus]
  )

  const handleConfirmEnrollment = useCallback(() => {
    if (!enrollment.enrollment?.method_id) {
      toast.error('Enrollment session expired. Start again.')
      return
    }
    const methodType: MFAMethodType = 'totp'
    enrollment.setPendingEditMethod({ id: enrollment.enrollment.method_id, type: methodType })
    const normalizedLabel = getDefaultLabelForType(methodType)
    confirmEnrollmentMutation.mutate({
      method_id: enrollment.enrollment.method_id,
      code: enrollment.verificationCode,
      trust_device: enrollment.trustEnrollmentDevice,
      label: normalizedLabel,
    })
  }, [
    confirmEnrollmentMutation,
    enrollment.enrollment?.method_id,
    enrollment.setPendingEditMethod,
    enrollment.trustEnrollmentDevice,
    enrollment.verificationCode,
  ])

  const handleDownloadCodes = useCallback((codes: string[]) => {
    const blob = new Blob([formatCodeForDownload(codes)], {
      type: 'text/plain;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'rack-gateway-backup-codes.txt'
    link.click()
    URL.revokeObjectURL(url)
  }, []) // No external dependencies

  const handleCopy = useCallback(async (value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      toast.success('Copied to clipboard')
    } catch {
      toast.error('Failed to copy to clipboard')
    }
  }, []) // toast is a stable reference

  const handleCopyCodes = useCallback(async (codes: string[]) => {
    try {
      await navigator.clipboard.writeText(codes.join('\n'))
      toast.success('Backup codes copied')
    } catch {
      toast.error('Unable to copy backup codes')
    }
  }, []) // toast is a stable reference

  const handleRegenerateCodes = useCallback(
    () =>
      runWithStepUp(async () => {
        await regenerateCodesMutation.mutateAsync()
      }),
    [regenerateCodesMutation, runWithStepUp]
  )

  const handleMethodRemove = useCallback(
    (method: MFAMethodRecord) => {
      if (!method.id) {
        toast.error('Unable to determine method identifier')
        return
      }
      runWithStepUp(async () => {
        await deleteMethodMutation.mutateAsync(method.id as number)
      })
    },
    [deleteMethodMutation, runWithStepUp]
  )
  const handleRevokeDevice = useCallback(
    (device: TrustedDeviceRecord) => {
      if (!device.id) {
        toast.error('Unable to determine device identifier')
        return
      }
      runWithStepUp(async () => {
        await revokeDeviceMutation.mutateAsync(device.id as number)
      })
    },
    [revokeDeviceMutation, runWithStepUp]
  )
  const handleTrustDevice = useCallback(
    () =>
      runWithStepUp(async () => {
        await trustDeviceMutation.mutateAsync()
      }),
    [runWithStepUp, trustDeviceMutation]
  )

  const handleStartEnrollment = useCallback(
    (type: MFAMethodType) => {
      if (type === 'webauthn') {
        startWebAuthnMutation.mutate()
        return
      }
      startTOTPMutation.mutate()
    },
    [startTOTPMutation, startWebAuthnMutation]
  )

  const handlePreferredMethodChange = useCallback(
    (value: string) => {
      const method: string | undefined = value === 'none' ? undefined : value
      updatePreferredMethodMutation.mutate({
        preferred_method: method,
      })
    },
    [updatePreferredMethodMutation]
  )

  const handleDisableAllMfa = useCallback(() => {
    const methods = status?.methods ?? []
    if (methods.length === 0) {
      setDisableDialogOpen(false)
      return
    }
    const performDisable = async () => {
      setDisableAllPending(true)
      try {
        for (const method of methods) {
          if (!method.id) {
            toast.error('Unable to determine method identifier')
            continue
          }
          await deleteMethodMutation.mutateAsync(method.id as number)
        }
        toast.success('Multi-factor authentication disabled')
      } finally {
        setDisableAllPending(false)
      }
    }
    setDisableDialogOpen(false)
    runWithStepUp(performDisable)
  }, [deleteMethodMutation, runWithStepUp, status?.methods])

  useEffect(() => {
    if (
      promptMfa &&
      !autoPrompted &&
      status?.required &&
      status?.enrolled &&
      needsStepUp &&
      !isLoading &&
      !isFetching &&
      !isGlobalStepUpVerifying
    ) {
      setAutoPrompted(true)
      openStepUp({
        action: async () => {
          await invalidateStatus()
          if (promptMfa && redirectTarget) {
            const destination = resolveWebRedirect(redirectTarget)
            if (typeof window !== 'undefined') {
              window.location.assign(destination)
            }
          }
        },
      })
    }
  }, [
    promptMfa,
    autoPrompted,
    status?.required,
    status?.enrolled,
    needsStepUp,
    isLoading,
    isFetching,
    isGlobalStepUpVerifying,
    openStepUp,
    invalidateStatus,
    redirectTarget,
  ])

  const methods = status?.methods ?? []
  const trustedDevices = status?.trusted_devices ?? []
  const backupSummary = status?.backup_codes
  const showBackupCard = Boolean(status?.enrolled)
  const hasTOTP = methods.some((m) => m.type === 'totp')
  const hasWebAuthn = methods.some((m) => m.type === 'webauthn')
  const hasBothMethods = hasTOTP && hasWebAuthn
  const showCliEnrollmentWarning =
    enrollmentRequiredFlag && enrollmentChannel === 'cli' && !status?.enrolled

  const isBusy =
    isLoading ||
    isFetching ||
    startTOTPMutation.isPending ||
    startWebAuthnMutation.isPending ||
    confirmEnrollmentMutation.isPending

  const handleEditDialogSubmit = useCallback(() => {
    const { editingMethod } = methodEdit
    if (!editingMethod) {
      return
    }
    runWithStepUp(async () => {
      await updateMethodMutation.mutateAsync({
        methodId: editingMethod.id,
        label: methodEdit.editLabel.trim(),
        ...(editingMethod.type === 'webauthn' && {
          cliCapable: methodEdit.editCliCapable,
        }),
      })
    })
  }, [methodEdit, runWithStepUp, updateMethodMutation])

  return {
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
    deleteMethodPending: deleteMethodMutation.isPending,
    updatePreferredMethodPending: updatePreferredMethodMutation.isPending,
    updateMethodPending: updateMethodMutation.isPending,
    startTotpPending: startTOTPMutation.isPending,
    startWebAuthnPending: startWebAuthnMutation.isPending,
    confirmEnrollmentPending: confirmEnrollmentMutation.isPending,
    regenerateCodesPending: regenerateCodesMutation.isPending,
    trustDevicePending: trustDeviceMutation.isPending,
    enrollmentModalOpen: enrollment.enrollmentModalOpen,
    enrollmentStep: enrollment.enrollmentStep,
    enrollment: enrollment.enrollment,
    recentBackupCodes,
    qrDataUrl: enrollment.qrDataUrl,
    verificationCode: enrollment.verificationCode,
    setVerificationCode: enrollment.setVerificationCode,
    trustEnrollmentDevice: enrollment.trustEnrollmentDevice,
    setTrustEnrollmentDevice: enrollment.setTrustEnrollmentDevice,
    openDropdownId,
    setOpenDropdownId,
    editingMethod: methodEdit.editingMethod,
    editLabel: methodEdit.editLabel,
    setEditLabel: methodEdit.setEditLabel,
    editCliCapable: methodEdit.editCliCapable,
    setEditCliCapable: methodEdit.setEditCliCapable,
    handlePreferredMethodChange,
    handleDisableAllMfa,
    handleRegenerateCodes,
    handleDownloadCodes,
    handleCopy,
    handleCopyCodes,
    handleMethodEdit: methodEdit.handleMethodEdit,
    handleMethodRemove,
    handleRevokeDevice,
    handleTrustDevice,
    handleEnrollmentOpenChange: enrollment.handleEnrollmentOpenChange,
    handleEnrollmentCancel: enrollment.handleEnrollmentCancel,
    handleEnrollmentBack: enrollment.handleEnrollmentBack,
    handleConfirmEnrollment,
    handleStartEnrollment,
    openMethodEnrollment: enrollment.openMethodEnrollment,
    handleEditDialogCancel: methodEdit.handleEditDialogCancel,
    handleEditDialogSubmit,
    isBusy,
  }
}
