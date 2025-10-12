import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation } from '@tanstack/react-router'
import { MoreVertical, Pencil, ShieldAlert, Trash2 } from 'lucide-react'
import QRCode from 'qrcode'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog'
import { MFAInput } from '@/components/mfa-input'
import { TimeAgo } from '@/components/time-ago'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { toast } from '@/components/ui/use-toast'
import { useAuth } from '@/contexts/auth-context'
import { useStepUp } from '@/contexts/step-up-context'
import {
  type BackupCodesResponse,
  confirmTOTPEnrollment,
  confirmWebAuthnEnrollment,
  deleteMFAMethod,
  getMFAStatus,
  type MFAStatusResponse,
  regenerateBackupCodes,
  revokeTrustedDevice,
  type StartTOTPEnrollmentResponse,
  type StartWebAuthnEnrollmentResponse,
  startTOTPEnrollment,
  startWebAuthnEnrollment,
  updateMFAMethod,
  updatePreferredMFAMethod,
} from '@/lib/api'
import { getErrorMessage } from '@/lib/error-utils'
import { normalizeRedirectPath } from '@/lib/navigation'
import { WebRoute } from '@/lib/routes'
import {
  createCredential,
  prepareCreationOptions,
  serializeRegistrationCredential,
} from '@/lib/webauthn-utils'

type EnrollmentState = (StartTOTPEnrollmentResponse | StartWebAuthnEnrollmentResponse) & {
  qrDataUrl?: string | null
  enrollmentType?: 'totp' | 'webauthn'
}

type MFAMethodType = 'totp' | 'webauthn'

const STEP_UP_QUERY_KEY = ['mfaStatus'] as const
const MFA_METHOD_TYPE_LABELS: Record<string, string> = {
  totp: 'TOTP',
  webauthn: 'WebAuthn',
}
export const DEFAULT_MFA_LABEL = 'Security Key'

function formatCodeForDownload(codes: string[]): string {
  const header = [
    'Your Rack Gateway backup codes',
    '',
    'Each code can be used once. Store them securely.',
    '',
  ]
  return [...header, ...codes].join('\n')
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: MFA management page coordinates multiple flows.
export function AccountSecurityPage() {
  const queryClient = useQueryClient()
  const location = useLocation()
  const { refresh: refreshUser, user } = useAuth()
  const { openStepUp, requireStepUp, isVerifying: isGlobalStepUpVerifying } = useStepUp()
  const [enrollmentModalOpen, setEnrollmentModalOpen] = useState(false)
  const [enrollmentStep, setEnrollmentStep] = useState<'method-selection' | 'totp-setup'>(
    'method-selection'
  )
  const [enrollment, setEnrollment] = useState<EnrollmentState | null>(null)
  const [verificationCode, setVerificationCode] = useState('')
  const [trustEnrollmentDevice, setTrustEnrollmentDevice] = useState(true)
  const [qrDataUrl, setQrDataUrl] = useState<string | null>(null)
  const [recentBackupCodes, setRecentBackupCodes] = useState<string[] | null>(null)
  const [enrollmentLabel, setEnrollmentLabel] = useState(DEFAULT_MFA_LABEL)
  const [disableDialogOpen, setDisableDialogOpen] = useState(false)
  const [disableAllPending, setDisableAllPending] = useState(false)
  const [autoPrompted, setAutoPrompted] = useState(false)
  const [editingMethod, setEditingMethod] = useState<{ id: number; label: string } | null>(null)
  const [editLabel, setEditLabel] = useState('')

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
    queryKey: STEP_UP_QUERY_KEY,
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
  }, [enrollment])

  const needsStepUp = useMemo(() => {
    if (!status?.recent_step_up_expires_at) {
      return true
    }
    const expires = new Date(status.recent_step_up_expires_at).getTime()
    return Number.isNaN(expires) || expires <= Date.now()
  }, [status?.recent_step_up_expires_at])

  const invalidateStatus = useCallback(
    () => queryClient.invalidateQueries({ queryKey: STEP_UP_QUERY_KEY }),
    [queryClient]
  )

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
    onError: (error) => {
      toast.error(getErrorMessage(error))
    },
  })

  const startWebAuthnMutation = useMutation({
    mutationFn: startWebAuthnEnrollment,
    onSuccess: async (data) => {
      // Immediately trigger the WebAuthn browser prompt
      try {
        if (!data.public_key_options) {
          throw new Error('No public key options received from server')
        }

        // Convert server options to browser-compatible format
        const webAuthnOptions = prepareCreationOptions(data.public_key_options)

        // Call the browser WebAuthn API to create a credential
        const credential = await createCredential({
          publicKey: webAuthnOptions,
        })

        if (!credential) {
          throw new Error('No credential created')
        }

        // Serialize the credential for the backend
        const credentialForBackend = serializeRegistrationCredential(
          credential as PublicKeyCredential
        )

        const normalizedLabel = enrollmentLabel.trim() || 'Security Key'
        await confirmWebAuthnEnrollment({
          method_id: data.method_id ?? 0,
          credential: credentialForBackend,
          label: normalizedLabel,
        })

        // Success!
        setRecentBackupCodes(data.backup_codes ?? null)
        toast.success('WebAuthn enrollment completed')
        // Close modal - onOpenChange will clean up state after animation
        setEnrollmentModalOpen(false)
        await invalidateStatus()
        refreshUser().catch(() => {
          /* noop */
        })
      } catch (error) {
        const msg = getErrorMessage(error)
        toast.error(`WebAuthn enrollment failed: ${msg}`)
        // Reset state on error
        setEnrollment(null)
      }
    },
    onError: (error) => {
      const msg = getErrorMessage(error)
      // If WebAuthn not configured, silently fall back to TOTP instead of showing error
      if (msg.includes('not configured') || msg.includes('WebAuthn')) {
        startTOTPMutation.mutate()
      } else {
        toast.error(msg)
      }
    },
  })

  const handleStartEnrollment = (method: MFAMethodType) => {
    switch (method) {
      case 'totp':
        startTOTPMutation.mutate()
        break
      case 'webauthn':
        startWebAuthnMutation.mutate()
        break
      default:
        toast.error('Invalid MFA method')
        break
    }
  }

  const confirmEnrollmentMutation = useMutation({
    mutationFn: confirmTOTPEnrollment,
    onSuccess: async () => {
      toast.success('Multi-factor authentication enabled')
      // Close modal first - onOpenChange will clean up state after animation
      setEnrollmentModalOpen(false)
      await invalidateStatus()
      refreshUser().catch(() => {
        /* noop */
      })
    },
    onError: (error) => {
      toast.error(getErrorMessage(error))
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
    onError: (error) => {
      toast.error(getErrorMessage(error))
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
    onError: (error) => {
      toast.error(getErrorMessage(error))
    },
  })

  const revokeDeviceMutation = useMutation({
    mutationFn: revokeTrustedDevice,
    onSuccess: () => {
      toast.success('Trusted device revoked')
      invalidateStatus()
    },
    onError: (error) => {
      toast.error(getErrorMessage(error))
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
      toast.error(getErrorMessage(error))
    },
  })

  const updatePreferredMethodMutation = useMutation({
    mutationFn: updatePreferredMFAMethod,
    onSuccess: async () => {
      await refreshUser().catch(() => {
        /* noop */
      })
      toast.success('Preferred MFA method updated')
    },
    onError: (error) => {
      toast.error(getErrorMessage(error))
    },
  })

  const runWithStepUp = (action: () => Promise<void>) => {
    if (!needsStepUp) {
      action().catch((error) => {
        toast.error(getErrorMessage(error))
      })
      return
    }
    requireStepUp(async () => {
      await invalidateStatus()
      await action()
    })
  }

  const handleConfirmEnrollment = () => {
    if (!enrollment?.method_id) {
      toast.error('Enrollment session expired. Start again.')
      return
    }
    const normalizedLabel = enrollmentLabel.trim() || DEFAULT_MFA_LABEL
    confirmEnrollmentMutation.mutate({
      method_id: enrollment.method_id,
      code: verificationCode,
      trust_device: trustEnrollmentDevice,
      label: normalizedLabel,
    })
  }

  const handleDownloadCodes = (codes: string[]) => {
    const blob = new Blob([formatCodeForDownload(codes)], { type: 'text/plain;charset=utf-8' })
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
            const targetPath = redirectTarget.startsWith('/')
              ? redirectTarget.slice(1)
              : redirectTarget
            const absolute = WebRoute(targetPath)
            if (typeof window !== 'undefined') {
              window.location.assign(absolute)
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
    startTOTPMutation.isPending ||
    startWebAuthnMutation.isPending ||
    confirmEnrollmentMutation.isPending

  const methods = status?.methods ?? []
  const trustedDevices = status?.trusted_devices ?? []
  const backupSummary = status?.backup_codes
  const showBackupCard = Boolean(status?.enrolled)

  // Check if user has both TOTP and WebAuthn methods enrolled
  const hasTOTP = methods.some((m) => m.type === 'totp')
  const hasWebAuthn = methods.some((m) => m.type === 'webauthn')
  const hasBothMethods = hasTOTP && hasWebAuthn

  const handlePreferredMethodChange = (value: string) => {
    const method: string | undefined = value === 'none' ? undefined : value
    updatePreferredMethodMutation.mutate({
      preferred_method: method,
    })
  }

  return (
    <div className="space-y-8 p-8">
      <div className="space-y-2">
        <h1 className="font-bold text-3xl">Account Security</h1>
        <p className="text-muted-foreground">
          Manage multi-factor authentication and trusted devices.
        </p>
      </div>

      {enrollmentRequiredFlag && enrollmentChannel === 'cli' && !status?.enrolled ? (
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
        <Card>
          <CardHeader className="flex flex-row items-center justify-between gap-3">
            <CardTitle>Multi-Factor Authentication</CardTitle>
            <Badge variant={status?.enrolled ? 'success' : 'outline'}>
              {status?.enrolled ? 'Enabled' : 'Disabled'}
            </Badge>
          </CardHeader>
          <CardContent className="flex-1 space-y-7">
            {status?.required && !status.enrolled ? (
              <div className="rounded-md border border-destructive bg-destructive/10 p-4 font-medium text-destructive-foreground text-sm">
                Multi-factor authentication is required for all gateway users. Enable MFA now to
                restore access to the CLI and the rest of the web console.
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

            {status?.enrolled && hasBothMethods ? (
              <div className="space-y-5">
                <Label>Preferred sign-in method</Label>
                <RadioGroup
                  disabled={updatePreferredMethodMutation.isPending}
                  onValueChange={handlePreferredMethodChange}
                  value={user?.preferred_mfa_method || 'totp'}
                >
                  <div className="flex items-center space-x-2">
                    <RadioGroupItem id="totp" value="totp" />
                    <Label className="cursor-pointer font-normal" htmlFor="totp">
                      TOTP Authenticator
                    </Label>
                  </div>
                  <div className="flex items-center space-x-2">
                    <RadioGroupItem id="webauthn" value="webauthn" />
                    <Label className="cursor-pointer font-normal" htmlFor="webauthn">
                      WebAuthn / Security Key
                    </Label>
                  </div>
                </RadioGroup>
                <p className="text-muted-foreground text-xs">
                  This method will be used by default when signing in. You can always use the other
                  method if needed.
                </p>
              </div>
            ) : null}
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
                disabled={
                  startTOTPMutation.isPending || startWebAuthnMutation.isPending || !!enrollment
                }
                onClick={() => {
                  setEnrollmentModalOpen(true)
                  setEnrollmentStep('method-selection')
                  setEnrollmentLabel(DEFAULT_MFA_LABEL)
                }}
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
                <Button onClick={() => handleDownloadCodes(recentBackupCodes)} variant="outline">
                  Download latest codes
                </Button>
              ) : null}
            </CardFooter>
          </Card>
        ) : null}
      </div>

      {methods.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>Registered MFA Methods</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
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
                      <td className="py-2">{method.label ?? DEFAULT_MFA_LABEL}</td>
                      <td className="py-2">
                        <TimeAgo date={method.created_at ?? null} />
                      </td>
                      <td className="py-2">
                        {method.last_used_at ? <TimeAgo date={method.last_used_at} /> : 'Never'}
                      </td>
                      <td className="py-2 text-right">
                        <div className="flex justify-end">
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button
                                aria-label={`Actions for ${method.label ?? DEFAULT_MFA_LABEL}`}
                                size="sm"
                                variant="ghost"
                              >
                                <MoreVertical className="h-4 w-4" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem
                                onClick={() => {
                                  if (!method.id) {
                                    toast.error('Unable to determine method identifier')
                                    return
                                  }
                                  setEditingMethod({
                                    id: method.id as number,
                                    label: method.label ?? DEFAULT_MFA_LABEL,
                                  })
                                  setEditLabel(method.label ?? DEFAULT_MFA_LABEL)
                                }}
                              >
                                <Pencil className="h-4 w-4" />
                                Edit Label
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
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
                                <Trash2 className="h-4 w-4" />
                                Remove Method
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
          <CardFooter>
            <Button
              disabled={
                startTOTPMutation.isPending || startWebAuthnMutation.isPending || !!enrollment
              }
              onClick={() => {
                setEnrollmentModalOpen(true)
                setEnrollmentStep('method-selection')
                setEnrollmentLabel(DEFAULT_MFA_LABEL)
              }}
              variant="outline"
            >
              Add Method
            </Button>
          </CardFooter>
        </Card>
      ) : null}

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

      <Dialog
        onOpenChange={(open) => {
          if (!open) {
            setEnrollmentModalOpen(false)
            setEnrollmentStep('method-selection')
            setEnrollment(null)
            setEnrollmentLabel(DEFAULT_MFA_LABEL)
            setQrDataUrl(null)
            setVerificationCode('')
          }
        }}
        open={enrollmentModalOpen}
      >
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Enable Multi-Factor Authentication</DialogTitle>
            <DialogDescription>
              Set up an additional verification method to secure your account.
            </DialogDescription>
          </DialogHeader>

          {enrollmentStep === 'method-selection' ? (
            <div className="space-y-6">
              <div className="space-y-2">
                <Label htmlFor="mfa-method-label">Device name (optional)</Label>
                <Input
                  id="mfa-method-label"
                  maxLength={150}
                  onChange={(event) => setEnrollmentLabel(event.target.value)}
                  placeholder={DEFAULT_MFA_LABEL}
                  value={enrollmentLabel}
                />
                <p className="text-muted-foreground text-xs">
                  Give this authenticator a memorable name
                </p>
              </div>

              <div className="space-y-3">
                <Label>Choose authentication method</Label>
                <div className="grid gap-4 md:grid-cols-2">
                  {status?.webauthn_available && (
                    <button
                      aria-label="Passkey or security key"
                      className="flex cursor-pointer flex-col gap-2 rounded-lg border bg-card p-4 text-left transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
                      disabled={startWebAuthnMutation.isPending}
                      onClick={() => handleStartEnrollment('webauthn')}
                      type="button"
                    >
                      <h3 className="font-semibold">Passkey or security key</h3>
                      <p className="text-muted-foreground text-sm">
                        Use security keys, Touch ID, or Windows Hello
                      </p>
                    </button>
                  )}
                  <button
                    aria-label="Authenticator app"
                    className="flex cursor-pointer flex-col gap-2 rounded-lg border bg-card p-4 text-left transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={startTOTPMutation.isPending}
                    onClick={() => handleStartEnrollment('totp')}
                    type="button"
                  >
                    <h3 className="font-semibold">Authenticator app</h3>
                    <p className="text-muted-foreground text-sm">
                      Use Google Authenticator, Authy, or similar apps
                    </p>
                  </button>
                </div>
              </div>

              <DialogFooter>
                <Button
                  onClick={() => {
                    setEnrollmentModalOpen(false)
                    setEnrollmentStep('method-selection')
                    setEnrollmentLabel(DEFAULT_MFA_LABEL)
                  }}
                  variant="outline"
                >
                  Cancel
                </Button>
              </DialogFooter>
            </div>
          ) : null}

          {enrollmentStep === 'totp-setup' && enrollment ? (
            <div className="space-y-6">
              <div className="grid gap-6 md:grid-cols-2">
                <div className="space-y-3">
                  <h3 className="font-semibold text-base">Scan the QR code</h3>
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
                  <div className="flex flex-col gap-2">
                    <Button
                      className="w-fit"
                      onClick={() =>
                        handleCopy('secret' in enrollment ? (enrollment.secret ?? '') : '')
                      }
                      size="sm"
                      variant="secondary"
                    >
                      Copy secret for manual entry
                    </Button>
                    <p className="text-muted-foreground text-xs">
                      Use this if your authenticator app cannot scan the QR code.
                    </p>
                  </div>
                </div>
                <div className="space-y-4">
                  {recentBackupCodes && recentBackupCodes.length > 0 && (
                    <div>
                      <h3 className="font-semibold text-base">Backup codes</h3>
                      <p className="text-muted-foreground text-sm">
                        Store these codes somewhere safe. Each code can be used once if you lose
                        access to your authenticator.
                      </p>
                      <ul className="mt-3 grid grid-cols-1 gap-1 font-mono text-sm">
                        {recentBackupCodes.slice(0, 6).map((code) => (
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
                <Label htmlFor="verification-code">Enter the 6-digit code to confirm</Label>
                <MFAInput
                  autoFocus
                  className="max-w-64"
                  id="verification-code"
                  maxLength={6}
                  onChange={(event) => setVerificationCode(event.target.value.trim())}
                  placeholder="123456"
                  value={verificationCode}
                />
                <label className="flex items-center gap-2 text-sm">
                  <input
                    checked={trustEnrollmentDevice}
                    onChange={(event) => setTrustEnrollmentDevice(event.target.checked)}
                    type="checkbox"
                  />
                  Trust this browser for 30 days
                </label>
              </div>
              <DialogFooter>
                <Button
                  onClick={() => {
                    setEnrollmentStep('method-selection')
                    setEnrollment(null)
                    setQrDataUrl(null)
                    setVerificationCode('')
                  }}
                  variant="outline"
                >
                  Back
                </Button>
                <Button
                  disabled={verificationCode.length === 0 || confirmEnrollmentMutation.isPending}
                  onClick={handleConfirmEnrollment}
                >
                  {confirmEnrollmentMutation.isPending ? 'Confirming...' : 'Confirm'}
                </Button>
              </DialogFooter>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <ConfirmDeleteDialog
        busy={disableAllPending || deleteMethodMutation.isPending}
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

      <Dialog
        onOpenChange={(open) => {
          if (!open) {
            setEditingMethod(null)
            setEditLabel('')
          }
        }}
        open={!!editingMethod}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit MFA Method Label</DialogTitle>
            <DialogDescription>Update the label for this MFA method.</DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(event) => {
              event.preventDefault()
              if (editingMethod) {
                updateMethodMutation.mutate({
                  methodId: editingMethod.id,
                  label: editLabel.trim(),
                })
              }
            }}
          >
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="edit-label">Label</Label>
                <Input
                  autoFocus
                  id="edit-label"
                  maxLength={150}
                  onChange={(event) => setEditLabel(event.target.value)}
                  placeholder="My Security Key"
                  required
                  value={editLabel}
                />
              </div>
            </div>
            <DialogFooter className="mt-4">
              <Button
                onClick={() => {
                  setEditingMethod(null)
                  setEditLabel('')
                }}
                type="button"
                variant="outline"
              >
                Cancel
              </Button>
              <Button disabled={updateMethodMutation.isPending || !editLabel.trim()} type="submit">
                {updateMethodMutation.isPending ? 'Saving...' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {isBusy ? (
        <p className="text-muted-foreground text-sm">Loading latest security information…</p>
      ) : null}
    </div>
  )
}
