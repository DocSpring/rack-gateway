import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import type { MFAStatusResponse } from '@/lib/api'

type MfaStatusCardProps = {
  status?: MFAStatusResponse
  hasBothMethods: boolean
  preferredMethod?: string | null
  onPreferredMethodChange: (value: string) => void
  preferredMethodPending: boolean
  disableButtonDisabled: boolean
  enableButtonDisabled: boolean
  onDisableClick: () => void
  onEnableClick: () => void
  enrollmentInProgress: boolean
}

export function MfaStatusCard({
  status,
  hasBothMethods,
  preferredMethod,
  onPreferredMethodChange,
  preferredMethodPending,
  disableButtonDisabled,
  enableButtonDisabled,
  onDisableClick,
  onEnableClick,
  enrollmentInProgress,
}: MfaStatusCardProps) {
  const enrolled = Boolean(status?.enrolled)
  const required = Boolean(status?.required)
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3">
        <CardTitle>Multi-Factor Authentication</CardTitle>
        <Badge variant={enrolled ? 'success' : 'outline'}>
          {enrolled ? 'Enabled' : 'Disabled'}
        </Badge>
      </CardHeader>
      <CardContent className="flex-1 space-y-7">
        {required && !enrolled ? (
          <div className="rounded-md border border-destructive bg-destructive/10 p-4 font-medium text-destructive-foreground text-sm">
            Multi-factor authentication is required for all gateway users. Enable MFA now to restore
            access to the CLI and the rest of the web console.
          </div>
        ) : null}
        {enrolled ? (
          <p className="text-muted-foreground text-sm">
            Multi-factor authentication is active for your account.
          </p>
        ) : (
          <p className="text-muted-foreground text-sm">
            Protect your account by requiring a TOTP code in addition to your password for sensitive
            actions.
          </p>
        )}

        {enrolled && hasBothMethods ? (
          <div className="space-y-5">
            <div className="space-y-3">
              <p className="text-muted-foreground text-xs uppercase tracking-wide">
                Preferred sign-in method
              </p>
              <RadioGroup
                disabled={preferredMethodPending}
                onValueChange={onPreferredMethodChange}
                value={preferredMethod || 'totp'}
              >
                <label className="flex cursor-pointer items-center space-x-2" htmlFor="totp">
                  <RadioGroupItem id="totp" value="totp" />
                  <span className="font-normal">TOTP Authenticator</span>
                </label>
                <label className="flex cursor-pointer items-center space-x-2" htmlFor="webauthn">
                  <RadioGroupItem id="webauthn" value="webauthn" />
                  <span className="font-normal">WebAuthn / Security Key</span>
                </label>
              </RadioGroup>
            </div>
            <p className="text-muted-foreground text-xs">
              This method will be used by default when signing in. You can always use the other
              method if needed.
            </p>
          </div>
        ) : null}
      </CardContent>
      <CardFooter className="mt-auto flex flex-wrap gap-3">
        {enrolled ? (
          <Button disabled={disableButtonDisabled} onClick={onDisableClick} variant="destructive">
            Disable MFA
          </Button>
        ) : (
          <Button disabled={enableButtonDisabled} onClick={onEnableClick}>
            {enrollmentInProgress ? 'Enrollment In Progress' : 'Enable MFA'}
          </Button>
        )}
      </CardFooter>
    </Card>
  )
}
