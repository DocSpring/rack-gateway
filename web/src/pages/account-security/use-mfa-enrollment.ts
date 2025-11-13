import QRCode from 'qrcode'
import { useCallback, useEffect, useState } from 'react'
import type { EnrollmentState, MFAMethodType } from '@/components/account-security/types'

type UseMFAEnrollmentResult = {
  enrollmentModalOpen: boolean
  enrollmentStep: 'method-selection' | 'totp-setup'
  enrollment: EnrollmentState | null
  verificationCode: string
  setVerificationCode: (value: string) => void
  trustEnrollmentDevice: boolean
  setTrustEnrollmentDevice: (value: boolean) => void
  qrDataUrl: string | null
  pendingEditMethod: { id: number; type: MFAMethodType } | null
  setPendingEditMethod: (value: { id: number; type: MFAMethodType } | null) => void
  setEnrollment: (value: EnrollmentState | null) => void
  setEnrollmentStep: (value: 'method-selection' | 'totp-setup') => void
  closeEnrollmentModal: () => void
  openMethodEnrollment: () => void
  handleEnrollmentOpenChange: (open: boolean) => void
  handleEnrollmentCancel: () => void
  handleEnrollmentBack: () => void
}

export function useMFAEnrollment(): UseMFAEnrollmentResult {
  const [enrollmentModalOpen, setEnrollmentModalOpen] = useState(false)
  const [enrollmentStep, setEnrollmentStep] = useState<'method-selection' | 'totp-setup'>(
    'method-selection'
  )
  const [enrollment, setEnrollment] = useState<EnrollmentState | null>(null)
  const [verificationCode, setVerificationCode] = useState('')
  const [trustEnrollmentDevice, setTrustEnrollmentDevice] = useState(true)
  const [qrDataUrl, setQrDataUrl] = useState<string | null>(null)
  const [pendingEditMethod, setPendingEditMethod] = useState<{
    id: number
    type: MFAMethodType
  } | null>(null)

  const closeEnrollmentModal = useCallback(() => {
    setEnrollmentModalOpen(false)
    setEnrollmentStep('method-selection')
    setEnrollment(null)
    setQrDataUrl(null)
    setVerificationCode('')
    setTrustEnrollmentDevice(true)
    setPendingEditMethod(null)
  }, [])

  const openMethodEnrollment = useCallback(() => {
    setEnrollmentModalOpen(true)
    setEnrollmentStep('method-selection')
  }, [])

  const handleEnrollmentOpenChange = useCallback(
    (open: boolean) => {
      if (open) {
        setEnrollmentModalOpen(true)
        return
      }
      closeEnrollmentModal()
    },
    [closeEnrollmentModal]
  )

  const handleEnrollmentCancel = useCallback(() => {
    closeEnrollmentModal()
  }, [closeEnrollmentModal])

  const handleEnrollmentBack = useCallback(() => {
    setEnrollmentStep('method-selection')
    setEnrollment(null)
    setQrDataUrl(null)
    setVerificationCode('')
  }, [])

  // Generate QR code when TOTP enrollment starts
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

  return {
    enrollmentModalOpen,
    enrollmentStep,
    enrollment,
    verificationCode,
    setVerificationCode,
    trustEnrollmentDevice,
    setTrustEnrollmentDevice,
    qrDataUrl,
    pendingEditMethod,
    setPendingEditMethod,
    setEnrollment,
    setEnrollmentStep,
    closeEnrollmentModal,
    openMethodEnrollment,
    handleEnrollmentOpenChange,
    handleEnrollmentCancel,
    handleEnrollmentBack,
  }
}
