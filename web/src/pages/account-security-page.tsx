import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import QRCode from 'qrcode'
import { useEffect, useMemo, useState } from 'react'
import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog'
import { MFAInput } from '@/components/mfa-input'
import { TimeAgo } from '@/components/time-ago'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/use-toast'
import {
  type BackupCodesResponse,
  confirmTOTPEnrollment,
  deleteMFAMethod,
  getMFAStatus,
  type MFAStatusResponse,
  regenerateBackupCodes,
  revokeTrustedDevice,
  type StartTOTPEnrollmentResponse,
  startTOTPEnrollment,
  verifyMFA,
} from '@/lib/api'

type EnrollmentState = StartTOTPEnrollmentResponse & {
  qrDataUrl?: string | null
}

type PendingAction = (() => Promise<void>) | null

const STEP_UP_QUERY_KEY = ['mfaStatus'] as const
const MFA_METHOD_TYPE_LABELS: Record<string, string> = {
  totp: 'TOTP',
}

function extractErrorMessage(error: unknown): string {
  if (isAxiosError<{ error?: string }>(error)) {
    const message = error.response?.data?.error
    if (typeof message === 'string' && message.trim() !== '') {
      return message
    }
  }
  if (error instanceof Error) {
    return error.message
  }
  return 'Something went wrong'
}

function formatCodeForDownload(codes: string[]): string {
  const header = [
    'Your Convox Gateway backup codes',
    '',
    'Each code can be used once. Store them securely.',
    '',
  ]
  return [...header, ...codes].join('\n')
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: MFA management page coordinates multiple flows.
export function AccountSecurityPage() {
  const queryClient = useQueryClient()
  const [enrollment, setEnrollment] = useState<EnrollmentState | null>(null)
  const [verificationCode, setVerificationCode] = useState('')
  const [trustEnrollmentDevice, setTrustEnrollmentDevice] = useState(true)
  const [qrDataUrl, setQrDataUrl] = useState<string | null>(null)
  const [recentBackupCodes, setRecentBackupCodes] = useState<string[] | null>(null)
  const [stepUpOpen, setStepUpOpen] = useState(false)
  const [stepUpCode, setStepUpCode] = useState('')
  const [stepUpTrustDevice, setStepUpTrustDevice] = useState(false)
  const [pendingAction, setPendingAction] = useState<PendingAction>(null)
  const [disableDialogOpen, setDisableDialogOpen] = useState(false)
  const [disableAllPending, setDisableAllPending] = useState(false)

  const {
    data: status,
    isLoading,
    isFetching,
  } = useQuery<MFAStatusResponse>({
    queryKey: STEP_UP_QUERY_KEY,
    queryFn: getMFAStatus,
    refetchOnWindowFocus: true,
    staleTime: 30_000,
  })

  useEffect(() => {
    if (!enrollment?.uri) {
      setQrDataUrl(null)
      return
    }
    let cancelled = false
    QRCode.toDataURL(enrollment.uri, { margin: 1, scale: 5 })
      .then((data: string) => {
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
  }, [enrollment?.uri])

  const needsStepUp = useMemo(() => {
    if (!status?.recent_step_up_expires_at) {
      return true
    }
    const expires = new Date(status.recent_step_up_expires_at).getTime()
    return Number.isNaN(expires) || expires <= Date.now()
  }, [status?.recent_step_up_expires_at])

  const invalidateStatus = () => queryClient.invalidateQueries({ queryKey: STEP_UP_QUERY_KEY })

  const startEnrollmentMutation = useMutation({
    mutationFn: startTOTPEnrollment,
    onSuccess: (data) => {
      setEnrollment(data)
      setVerificationCode('')
      setTrustEnrollmentDevice(true)
      setRecentBackupCodes(data.backup_codes ?? null)
      toast.success('MFA enrollment started')
    },
    onError: (error) => {
      toast.error(extractErrorMessage(error))
    },
  })

  const confirmEnrollmentMutation = useMutation({
    mutationFn: confirmTOTPEnrollment,
    onSuccess: () => {
      toast.success('Multi-factor authentication enabled')
      setEnrollment(null)
      setQrDataUrl(null)
      setVerificationCode('')
      setTrustEnrollmentDevice(true)
      invalidateStatus()
    },
    onError: (error) => {
      toast.error(extractErrorMessage(error))
    },
  })

  const deleteMethodMutation = useMutation({
    mutationFn: deleteMFAMethod,
    onSuccess: () => {
      toast.success('MFA method removed')
      invalidateStatus()
    },
    onError: (error) => {
      toast.error(extractErrorMessage(error))
    },
  })

  const revokeDeviceMutation = useMutation({
    mutationFn: revokeTrustedDevice,
    onSuccess: () => {
      toast.success('Trusted device revoked')
      invalidateStatus()
    },
    onError: (error) => {
      toast.error(extractErrorMessage(error))
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
    onError: (error) => {
      toast.error(extractErrorMessage(error))
    },
  })

  const verifyMutation = useMutation({
    mutationFn: verifyMFA,
    onSuccess: async () => {
      const action = pendingAction
      toast.success('MFA verification successful')
      setStepUpOpen(false)
      setStepUpCode('')
      setStepUpTrustDevice(false)
      setPendingAction(null)
      await invalidateStatus()
      if (action) {
        try {
          await action()
        } catch (error) {
          toast.error(extractErrorMessage(error))
        }
      }
    },
    onError: (error) => {
      toast.error(extractErrorMessage(error))
    },
  })

  const runWithStepUp = (action: () => Promise<void>) => {
    if (!needsStepUp) {
      action().catch((error) => {
        toast.error(extractErrorMessage(error))
      })
      return
    }
    setPendingAction(() => action)
    setStepUpCode('')
    setStepUpTrustDevice(false)
    setStepUpOpen(true)
  }

  const handleConfirmEnrollment = () => {
    if (!enrollment?.method_id) {
      toast.error('Enrollment session expired. Start again.')
      return
    }
    confirmEnrollmentMutation.mutate({
      method_id: enrollment.method_id,
      code: verificationCode,
      trust_device: trustEnrollmentDevice,
    })
  }

  const handleDownloadCodes = (codes: string[]) => {
    const blob = new Blob([formatCodeForDownload(codes)], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'convox-gateway-backup-codes.txt'
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

  const handleDisableAllMfa = () => {
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

  const isBusy =
    isLoading ||
    isFetching ||
    startEnrollmentMutation.isPending ||
    confirmEnrollmentMutation.isPending

  const methods = status?.methods ?? []
  const trustedDevices = status?.trusted_devices ?? []
  const backupSummary = status?.backup_codes
  const showBackupCard = Boolean(status?.enrolled)

  return (
    <div className="space-y-8 p-8">
      <div className="space-y-2">
        <h1 className="font-bold text-3xl">Account Security</h1>
        <p className="text-muted-foreground">
          Manage multi-factor authentication and trusted devices.
        </p>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between gap-3">
            <CardTitle>Multi-Factor Authentication</CardTitle>
            <Badge variant={status?.enrolled ? 'success' : 'outline'}>
              {status?.enrolled ? 'Enabled' : 'Disabled'}
            </Badge>
          </CardHeader>
          <CardContent className="flex-1 space-y-7">
            {status?.required && !status.enrolled ? (
              <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-destructive text-sm">
                MFA is required. Enable it before accessing sensitive operations.
              </div>
            ) : null}
            {status?.enrolled ? (
              <p className="text-muted-foreground text-sm">
                Multi-factor authentication is active for your account.
              </p>
            ) : (
              <p className="text-muted-foreground text-sm">
                Protect your account by requiring a TOTP code in addition to your password for
                sensitive actions.
              </p>
            )}
          </CardContent>
          <CardFooter className="mt-auto flex flex-wrap gap-3">
            {status?.enrolled ? (
              <Button
                disabled={
                  methods.length === 0 || deleteMethodMutation.isPending || disableAllPending
                }
                onClick={() => setDisableDialogOpen(true)}
                variant="destructive"
              >
                Disable MFA
              </Button>
            ) : (
              <Button
                disabled={startEnrollmentMutation.isPending || !!enrollment}
                onClick={() => startEnrollmentMutation.mutate()}
              >
                {enrollment ? 'Enrollment In Progress' : 'Enable MFA'}
              </Button>
            )}
          </CardFooter>
        </Card>

        {showBackupCard ? (
          <Card>
            <CardHeader>
              <CardTitle>Backup Codes</CardTitle>
            </CardHeader>
            <CardContent className="flex-1 space-y-6 pb-2">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1">
                  <p className="text-muted-foreground text-xs uppercase tracking-wide">
                    Unused codes
                  </p>
                  <p className="font-semibold text-2xl">{backupSummary?.unused ?? 0}</p>
                </div>
                {backupSummary?.last_generated_at ? (
                  <div className="space-y-1">
                    <p className="text-muted-foreground text-xs uppercase tracking-wide">
                      Last generated
                    </p>
                    <p className="text-sm">
                      <TimeAgo date={backupSummary.last_generated_at} />
                    </p>
                  </div>
                ) : null}
              </div>
            </CardContent>
            <CardFooter className="mt-auto flex flex-wrap gap-2">
              <Button
                disabled={regenerateCodesMutation.isPending}
                onClick={() =>
                  runWithStepUp(async () => {
                    await regenerateCodesMutation.mutateAsync()
                  })
                }
              >
                Regenerate backup codes
              </Button>
              {recentBackupCodes && recentBackupCodes.length > 0 ? (
                <Button
                  onClick={() => handleDownloadCodes(recentBackupCodes)}
                  size="sm"
                  variant="outline"
                >
                  Download latest codes
                </Button>
              ) : null}
            </CardFooter>
          </Card>
        ) : null}
      </div>

      {enrollment ? (
        <Card>
          <CardHeader>
            <CardTitle>Finish MFA Enrollment</CardTitle>
          </CardHeader>
          <CardContent className="space-y-6 pb-2">
            <div className="grid gap-6 md:grid-cols-2">
              <div className="space-y-3">
                <h3 className="font-semibold text-base">1. Scan the QR code</h3>
                {qrDataUrl ? (
                  /* biome-ignore lint/performance/noImgElement: Using inline QR code image for authenticator setup. */
                  <img
                    alt="Authenticator QR code"
                    className="h-48 w-48 rounded border"
                    height={192}
                    src={qrDataUrl}
                    width={192}
                  />
                ) : (
                  <p className="text-muted-foreground text-sm">
                    Unable to render QR code. Use the secret key instead.
                  </p>
                )}
              </div>
              <div className="space-y-4">
                <div>
                  <h3 className="font-semibold text-base">Secret key</h3>
                  <p className="break-all rounded border bg-muted px-3 py-2 font-mono text-sm">
                    {enrollment.secret}
                  </p>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <Button
                      onClick={() => handleCopy(enrollment.secret ?? '')}
                      size="sm"
                      variant="secondary"
                    >
                      Copy secret
                    </Button>
                  </div>
                </div>
                {recentBackupCodes && recentBackupCodes.length > 0 && (
                  <div>
                    <h3 className="font-semibold text-base">Backup codes</h3>
                    <p className="text-muted-foreground text-sm">
                      Store these codes somewhere safe. Each code can be used once if you lose
                      access to your authenticator.
                    </p>
                    <ul className="mt-3 grid grid-cols-1 gap-1 font-mono text-sm md:grid-cols-2">
                      {recentBackupCodes.map((code) => (
                        <li className="rounded border bg-muted px-2 py-1" key={code}>
                          {code}
                        </li>
                      ))}
                    </ul>
                    <div className="mt-2 flex flex-wrap gap-2">
                      <Button
                        onClick={() => handleCopyCodes(recentBackupCodes)}
                        size="sm"
                        variant="secondary"
                      >
                        Copy codes
                      </Button>
                      <Button
                        onClick={() => handleDownloadCodes(recentBackupCodes)}
                        size="sm"
                        variant="secondary"
                      >
                        Download
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            </div>
            <div className="space-y-3">
              <Label htmlFor="verification-code">2. Enter the 6-digit code to confirm</Label>
              <MFAInput
                id="verification-code"
                maxLength={6}
                onChange={(event) => setVerificationCode(event.target.value.trim())}
                placeholder="123456"
                value={verificationCode}
              />
              <label className="flex items-center gap-2 py-5 text-sm">
                <input
                  checked={trustEnrollmentDevice}
                  onChange={(event) => setTrustEnrollmentDevice(event.target.checked)}
                  type="checkbox"
                />
                Trust this browser for 30 days
              </label>
              <div className="flex gap-2">
                <Button
                  disabled={verificationCode.length === 0 || confirmEnrollmentMutation.isPending}
                  onClick={handleConfirmEnrollment}
                >
                  Confirm
                </Button>
                <Button
                  onClick={() => {
                    setEnrollment(null)
                    setQrDataUrl(null)
                    setVerificationCode('')
                  }}
                  variant="outline"
                >
                  Cancel
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Registered MFA Methods</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {methods.length === 0 ? (
            <p className="text-muted-foreground text-sm">No MFA methods configured.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[320px] text-left text-sm">
                <thead className="border-b text-muted-foreground text-xs uppercase">
                  <tr>
                    <th className="py-2">Type</th>
                    <th className="py-2">Label</th>
                    <th className="py-2">Added</th>
                    <th className="py-2">Last used</th>
                    <th className="py-2 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {methods.map((method) => (
                    <tr className="border-b last:border-0" key={method.id}>
                      <td className="py-2 font-medium">
                        {MFA_METHOD_TYPE_LABELS[(method.type ?? '').toLowerCase()] ??
                          (method.type ? method.type.toUpperCase() : 'MFA')}
                      </td>
                      <td className="py-2">{method.label ?? 'Authenticator app'}</td>
                      <td className="py-2">
                        <TimeAgo date={method.created_at ?? null} />
                      </td>
                      <td className="py-2">
                        {method.last_used_at ? <TimeAgo date={method.last_used_at} /> : 'Never'}
                      </td>
                      <td className="py-2 text-right">
                        <Button
                          onClick={() => {
                            if (!method.id) {
                              toast.error('Unable to determine method identifier')
                              return
                            }
                            runWithStepUp(async () => {
                              await deleteMethodMutation.mutateAsync(method.id as number)
                            })
                          }}
                          variant="destructive"
                        >
                          Remove
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Trusted Devices</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {trustedDevices.length === 0 ? (
            <p className="text-muted-foreground text-sm">No trusted devices on file.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[320px] text-left text-sm">
                <thead className="border-b text-muted-foreground text-xs uppercase">
                  <tr>
                    <th className="py-2">Device</th>
                    <th className="py-2">Last used</th>
                    <th className="py-2">IP</th>
                    <th className="py-2">Expires</th>
                    <th className="py-2 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {trustedDevices.map((device) => (
                    <tr className="border-b last:border-0" key={device.id}>
                      <td className="py-2">
                        <div className="max-w-[220px] break-words text-xs md:text-sm">
                          {device.label}
                        </div>
                      </td>
                      <td className="py-2">
                        <TimeAgo date={device.last_used_at ?? null} />
                      </td>
                      <td className="py-2 font-mono text-xs md:text-sm">
                        {device.ip_address ?? '—'}
                      </td>
                      <td className="py-2">
                        <TimeAgo date={device.expires_at ?? null} />
                      </td>
                      <td className="py-2 text-right">
                        <Button
                          onClick={() => {
                            if (!device.id) {
                              toast.error('Unable to determine device identifier')
                              return
                            }
                            runWithStepUp(async () => {
                              await revokeDeviceMutation.mutateAsync(device.id as number)
                            })
                          }}
                          variant="destructive"
                        >
                          Revoke
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      <ConfirmDeleteDialog
        busy={disableAllPending || deleteMethodMutation.isPending}
        confirmButtonText="Disable MFA"
        confirmText="DISABLE"
        description={<>Type DISABLE to remove all registered authenticators and turn off MFA for your account.</>}
        inputId="confirm-disable-mfa"
        onConfirm={handleDisableAllMfa}
        onOpenChange={setDisableDialogOpen}
        open={disableDialogOpen}
        title="Disable MFA"
      />

      <Dialog
        onOpenChange={(open) => {
          setStepUpOpen(open)
          if (!open) {
            setStepUpCode('')
            setStepUpTrustDevice(false)
            if (!verifyMutation.isPending) {
              setPendingAction(null)
            }
          }
        }}
        open={stepUpOpen}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>MFA verification required</DialogTitle>
            <DialogDescription>
              Enter a code from your authenticator or an unused backup code to continue.
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault()
              verifyMutation.mutate({ code: stepUpCode, trust_device: stepUpTrustDevice })
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="step-up-code">Verification code</Label>
              <MFAInput
                autoFocus
                id="step-up-code"
                maxLength={6}
                onChange={(event) => setStepUpCode(event.target.value.trim())}
                placeholder="123456"
                required
                value={stepUpCode}
              />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input
                checked={stepUpTrustDevice}
                onChange={(event) => setStepUpTrustDevice(event.target.checked)}
                type="checkbox"
              />
              Trust this browser for 30 days
            </label>
            <div className="flex justify-end gap-2">
              <Button
                onClick={() => {
                  setStepUpOpen(false)
                  setPendingAction(null)
                }}
                type="button"
                variant="outline"
              >
                Cancel
              </Button>
              <Button disabled={verifyMutation.isPending || stepUpCode.length === 0} type="submit">
                Verify
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      {isBusy ? (
        <p className="text-muted-foreground text-sm">Loading latest security information…</p>
      ) : null}
    </div>
  )
}
