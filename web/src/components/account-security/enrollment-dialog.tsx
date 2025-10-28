import { MFAInput } from '@/components/mfa-input'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import type { MFAStatusResponse } from '@/lib/api'
import type { EnrollmentState, MFAMethodType } from './types'

type EnrollmentDialogProps = {
  open: boolean
  enrollmentStep: 'method-selection' | 'totp-setup'
  onOpenChange: (open: boolean) => void
  onSelectMethod: (type: MFAMethodType) => void
  startTotpPending: boolean
  startWebAuthnPending: boolean
  enrollment: EnrollmentState | null
  qrDataUrl: string | null
  verificationCode: string
  onVerificationCodeChange: (value: string) => void
  trustEnrollmentDevice: boolean
  onTrustEnrollmentDeviceChange: (checked: boolean) => void
  recentBackupCodes: string[] | null
  confirmEnrollmentPending: boolean
  onConfirmEnrollment: () => void
  onBackToSelection: () => void
  onCancel: () => void
  onCopySecret: (value: string) => void
  onCopyCodes: (codes: string[]) => void
  onDownloadCodes: (codes: string[]) => void
  status?: MFAStatusResponse
}

export function EnrollmentDialog({
  open,
  enrollmentStep,
  onOpenChange,
  onSelectMethod,
  startTotpPending,
  startWebAuthnPending,
  enrollment,
  qrDataUrl,
  verificationCode,
  onVerificationCodeChange,
  trustEnrollmentDevice,
  onTrustEnrollmentDeviceChange,
  recentBackupCodes,
  confirmEnrollmentPending,
  onConfirmEnrollment,
  onBackToSelection,
  onCancel,
  onCopySecret,
  onCopyCodes,
  onDownloadCodes,
  status,
}: EnrollmentDialogProps) {
  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Enable Multi-Factor Authentication</DialogTitle>
          <DialogDescription>
            Set up an additional verification method to secure your account.
          </DialogDescription>
        </DialogHeader>

        {enrollmentStep === 'method-selection' ? (
          <div className="mt-6 space-y-6">
            <div className="space-y-3">
              <Label>Choose authentication method</Label>
              <div className="flex flex-col gap-4">
                {status?.webauthn_available ? (
                  <button
                    aria-label="Passkey or security key"
                    className="flex cursor-pointer flex-col gap-2 rounded-lg border bg-card p-4 text-left transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={startWebAuthnPending}
                    onClick={() => onSelectMethod('webauthn')}
                    type="button"
                  >
                    <h3 className="font-semibold">Passkey or security key</h3>
                    <p className="text-muted-foreground text-sm">
                      Use security keys, Touch ID, or Windows Hello
                    </p>
                  </button>
                ) : null}
                <button
                  aria-label="Authenticator app"
                  className="flex cursor-pointer flex-col gap-2 rounded-lg border bg-card p-4 text-left transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
                  disabled={startTotpPending}
                  onClick={() => onSelectMethod('totp')}
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
              <Button onClick={onCancel} variant="outline">
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
                      onCopySecret('secret' in enrollment ? (enrollment.secret ?? '') : '')
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
                {recentBackupCodes && recentBackupCodes.length > 0 ? (
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
                        onClick={() => onCopyCodes(recentBackupCodes)}
                        size="sm"
                        variant="secondary"
                      >
                        Copy codes
                      </Button>
                      <Button
                        onClick={() => onDownloadCodes(recentBackupCodes)}
                        size="sm"
                        variant="secondary"
                      >
                        Download
                      </Button>
                    </div>
                  </div>
                ) : null}
              </div>
            </div>
            <div className="space-y-3">
              <Label htmlFor="verification-code">Enter the 6-digit code to confirm</Label>
              <MFAInput
                autoFocus
                className="max-w-64"
                id="verification-code"
                maxLength={6}
                onChange={(event) => onVerificationCodeChange(event.target.value.trim())}
                placeholder="123456"
                value={verificationCode}
              />
              <label className="flex items-center gap-2 text-sm">
                <input
                  checked={trustEnrollmentDevice}
                  onChange={(event) => onTrustEnrollmentDeviceChange(event.target.checked)}
                  type="checkbox"
                />
                Trust this browser for 30 days
              </label>
            </div>
            <DialogFooter>
              <Button onClick={onBackToSelection} variant="outline">
                Back
              </Button>
              <Button
                disabled={verificationCode.length === 0 || confirmEnrollmentPending}
                onClick={onConfirmEnrollment}
              >
                {confirmEnrollmentPending ? 'Confirming...' : 'Confirm'}
              </Button>
            </DialogFooter>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
