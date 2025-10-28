import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation } from '@tanstack/react-router'
import QRCode from 'qrcode'
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
  editingMethod: { id: number; label: string } | null
  editLabel: string
  setEditLabel: (label: string) => void
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
  const [enrollmentModalOpen, setEnrollmentModalOpen] = useState(false)
  const [enrollmentStep, setEnrollmentStep] = useState<'method-selection' | 'totp-setup'>(
    'method-selection'
  )
  const [enrollment, setEnrollment] = useState<EnrollmentState | null>(null)
  const [verificationCode, setVerificationCode] = useState('')
  const [trustEnrollmentDevice, setTrustEnrollmentDevice] = useState(true)
  const [qrDataUrl, setQrDataUrl] = useState<string | null>(null)
  const [recentBackupCodes, setRecentBackupCodes] = useState<string[] | null>(null)
  const [disableDialogOpen, setDisableDialogOpen] = useState(false)
  const [disableAllPending, setDisableAllPending] = useState(false)
  const [autoPrompted, setAutoPrompted] = useState(false)
  const [editingMethod, setEditingMethod] = useState<{ id: number; label: string } | null>(null)
  const [pendingEditMethod, setPendingEditMethod] = useState<{
    id: number
    type: MFAMethodType
  } | null>(null)
  const [editLabel, setEditLabel] = useState('')
  const [openDropdownId, setOpenDropdownId] = useState<number | null>(null)

  const closeEnrollmentModal = useCallback(() => {
    setEnrollmentModalOpen(false)
    setEnrollmentStep('method-selection')
    setEnrollment(null)
    setQrDataUrl(null)
    setVerificationCode('')
    setTrustEnrollmentDevice(true)
    setPendingEditMethod(null)
  }, [])

  const searchParams = useMemo(() => new URLSearchParams(location.search ?? ''), [location.search])
  const promptMfa = searchParams.get('mfa') === 'verify'
  const redirectParam = searchParams.get('redirect')
  const redirectTarget = useMemo(() => normalizeRedirectPath(redirectParam), [redirectParam])
  const enrollmentRequiredFlag = searchParams.get('enrollment') === 'required'
  const enrollmentChannel = searchParams.get('channel') ?? undefined

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

  useEffect(() => {
    if (
      !enrollment ||
      enrollment.enrollmentType !== 'totp' ||
      !('uri' in enrollment) ||
      !enrollment.uri
    ) {
      setQrDataUrl(null)
      return
    }
    let cancelled = false
    QRCode.toDataURL(enrollment.uri, { margin: 1, scale: 5 })
      .then((data) => {
        if (!cancelled) {
          setQrDataUrl(data)
        }
      })
      .catch(() => {
        if (!cancelled) {
          setQrDataUrl(null)
        }
      })
    return () => {
      cancelled = true
    }
  }, [enrollment])

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
  })

  const runWithStepUp = (action: () => Promise<void>) => {
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
  }

  const handleConfirmEnrollment = () => {
    if (!enrollment?.method_id) {
      toast.error('Enrollment session expired. Start again.')
      return
    }
    const methodType: MFAMethodType = 'totp'
    setPendingEditMethod({ id: enrollment.method_id, type: methodType })
    const normalizedLabel = getDefaultLabelForType(methodType)
    confirmEnrollmentMutation.mutate({
      method_id: enrollment.method_id,
      code: verificationCode,
      trust_device: trustEnrollmentDevice,
      label: normalizedLabel,
    })
  }

  const handleDownloadCodes = (codes: string[]) => {
    const blob = new Blob([formatCodeForDownload(codes)], {
      type: 'text/plain;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'rack-gateway-backup-codes.txt'
    link.click()
    URL.revokeObjectURL(url)
  }

  const handleCopy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      toast.success('Copied to clipboard')
    } catch {
      toast.error('Failed to copy to clipboard')
    }
  }

  const handleCopyCodes = async (codes: string[]) => {
    try {
      await navigator.clipboard.writeText(codes.join('\n'))
      toast.success('Backup codes copied')
    } catch {
      toast.error('Unable to copy backup codes')
    }
  }

  const handleRegenerateCodes = () =>
    runWithStepUp(async () => {
      await regenerateCodesMutation.mutateAsync()
    })

  const openMethodEnrollment = () => {
    setEnrollmentModalOpen(true)
    setEnrollmentStep('method-selection')
  }

  const handleMethodEdit = (method: MFAMethodRecord) => {
    if (!method.id) {
      toast.error('Unable to determine method identifier')
      return
    }
    const label = method.label ?? getDefaultLabelForType(method.type)
    setEditingMethod({ id: method.id as number, label })
    setEditLabel(label)
  }

  const handleMethodRemove = (method: MFAMethodRecord) => {
    if (!method.id) {
      toast.error('Unable to determine method identifier')
      return
    }
    runWithStepUp(async () => {
      await deleteMethodMutation.mutateAsync(method.id as number)
    })
  }

  const handleRevokeDevice = (device: TrustedDeviceRecord) => {
    if (!device.id) {
      toast.error('Unable to determine device identifier')
      return
    }
    runWithStepUp(async () => {
      await revokeDeviceMutation.mutateAsync(device.id as number)
    })
  }

  const handleTrustDevice = () =>
    runWithStepUp(async () => {
      await trustDeviceMutation.mutateAsync()
    })

  const handleEnrollmentOpenChange = (open: boolean) => {
    if (open) {
      setEnrollmentModalOpen(true)
      return
    }
    closeEnrollmentModal()
  }

  const handleEnrollmentCancel = () => {
    closeEnrollmentModal()
  }

  const handleEnrollmentBack = () => {
    setEnrollmentStep('method-selection')
    setEnrollment(null)
    setQrDataUrl(null)
    setVerificationCode('')
  }

  const handleStartEnrollment = (type: MFAMethodType) => {
    if (type === 'webauthn') {
      startWebAuthnMutation.mutate()
      return
    }
    startTOTPMutation.mutate()
  }

  const handlePreferredMethodChange = (value: string) => {
    const method: string | undefined = value === 'none' ? undefined : value
    updatePreferredMethodMutation.mutate({
      preferred_method: method,
    })
  }

  const handleDisableAllMfa = () => {
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
  }

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

  const handleEditDialogCancel = () => {
    setEditingMethod(null)
    setEditLabel('')
  }

  const handleEditDialogSubmit = () => {
    if (!editingMethod) {
      return
    }
    runWithStepUp(async () => {
      await updateMethodMutation.mutateAsync({
        methodId: editingMethod.id,
        label: editLabel.trim(),
      })
    })
  }

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
  }
}
