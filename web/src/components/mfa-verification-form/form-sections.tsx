import type { MutableRefObject, ReactNode } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { MFAInput } from '@/components/mfa-input'
import { Button } from '@/components/ui/button'
import { trustLabel } from '@/lib/time'
import { isValidBackupCode, normalizeBackupCode } from './backup-code'
import { CodeEntryForm } from './code-entry-form'
import { isValidYubiKeyOTP, normalizeYubiKeyOTP } from './yubikey-otp'

const DIGITS_ONLY_REGEX = /^\d+$/
const SIX_DIGITS_REGEX = /^\d{6}$/

export type FormView = 'loading' | 'totp' | 'webauthn' | 'backup' | 'yubikey'

export interface RenderProps {
  showTrustDevice: boolean
  // How long a trusted device skips the login MFA challenge, once the MFA status has loaded.
  trustDeviceDays?: number
  trustDevice: boolean
  setTrustDevice: (value: boolean) => void
  isVerifying: boolean
  handleVerifyWebAuthn: () => void
  allowMethodSwitch: boolean
  hasTOTP: boolean
  hasWebAuthn: boolean
  hasBackupCodes: boolean
  hasYubiKeyOTP: boolean
  useWebAuthn: boolean
  useYubiKeyOTP: boolean
  setUseWebAuthn: (value: boolean) => void
  setUseBackupCode: (value: boolean) => void
  setUseYubiKeyOTP: (value: boolean) => void
  handleVerifyBackupCode: (code: string) => void
  handleVerifyYubiKeyOTP: (otp: string) => void
  renderCancelButton?: () => ReactNode
  focusOnMount: boolean
  inputVersion: number
  code: string
  setError: (error: string | null) => void
  setCode: (code: string) => void
  pendingCodeRef: MutableRefObject<string | null>
  trySubmitCode: () => void
}

function TrustDeviceOption(
  props: Pick<RenderProps, 'showTrustDevice' | 'trustDevice' | 'setTrustDevice' | 'trustDeviceDays'>
) {
  if (!props.showTrustDevice) {
    return null
  }
  return (
    <div className="flex justify-center">
      <label className="flex items-center gap-2 text-sm">
        <input
          checked={props.trustDevice}
          onChange={(event) => props.setTrustDevice(event.target.checked)}
          type="checkbox"
        />
        {trustLabel('device', props.trustDeviceDays)}
      </label>
    </div>
  )
}

function MethodLink({
  show,
  isVerifying,
  onClick,
  label,
}: {
  show: boolean
  isVerifying: boolean
  onClick: () => void
  label: string
}) {
  if (!show) {
    return null
  }
  return (
    <div className="flex justify-center">
      <Button
        className="h-auto p-0 text-sm"
        disabled={isVerifying}
        onClick={onClick}
        type="button"
        variant="link"
      >
        {label}
      </Button>
    </div>
  )
}

// OtherMethodLinks offers a YubiKey OTP (outside its own view) and a backup code.
function OtherMethodLinks(props: RenderProps) {
  return (
    <>
      <MethodLink
        isVerifying={props.isVerifying}
        label="Use a YubiKey OTP"
        onClick={() => props.setUseYubiKeyOTP(true)}
        show={props.hasYubiKeyOTP && !props.useYubiKeyOTP}
      />
      <MethodLink
        isVerifying={props.isVerifying}
        label="Use a backup code"
        onClick={() => props.setUseBackupCode(true)}
        show={props.hasBackupCodes}
      />
    </>
  )
}

function CancelButton(props: Pick<RenderProps, 'renderCancelButton'>) {
  if (!props.renderCancelButton) {
    return null
  }
  return <div className="flex justify-end">{props.renderCancelButton()}</div>
}

function renderLoadingState(props: Pick<RenderProps, 'showTrustDevice'>) {
  return (
    <div className="space-y-6">
      <div className="flex justify-center py-[20px]">
        <LoadingSpinner className="size-9" />
      </div>
      {props.showTrustDevice && (
        <div className="flex justify-center">
          <label className="invisible flex items-center gap-2 text-muted-foreground text-sm">
            <input disabled type="checkbox" />
            {trustLabel('device')}
          </label>
        </div>
      )}
    </div>
  )
}

function renderWebAuthnForm(props: RenderProps) {
  return (
    <div className="space-y-6">
      <div className="space-y-6">
        <div className="py-[10px]">
          <Button
            className="w-full"
            disabled={props.isVerifying}
            onClick={() => {
              props.handleVerifyWebAuthn()
            }}
          >
            {props.isVerifying ? (
              <LoadingSpinner className="size-4" variant="white" />
            ) : (
              'Authenticate with Security Key'
            )}
          </Button>
        </div>
        <TrustDeviceOption {...props} />
      </div>
      {props.allowMethodSwitch && props.hasTOTP && props.hasWebAuthn && (
        <div className="space-y-6">
          <div className="border-t" />
          <Button
            className="w-full"
            onClick={() => props.setUseWebAuthn(false)}
            type="button"
            variant="outline"
          >
            Use authenticator app instead
          </Button>
        </div>
      )}
      <OtherMethodLinks {...props} />
      <CancelButton {...props} />
    </div>
  )
}

function handleTOTPChange(props: RenderProps, value: string) {
  if (props.isVerifying) {
    return
  }
  const normalized = value.trim()
  ;(globalThis as { __lastOnChange?: string }).__lastOnChange = normalized
  props.setError(null)
  props.setCode(normalized)
  if (normalized.length === 6 && DIGITS_ONLY_REGEX.test(normalized)) {
    props.pendingCodeRef.current = normalized
    props.trySubmitCode()
  } else {
    props.pendingCodeRef.current = null
  }
}

function handleTOTPComplete(props: RenderProps, completedCode: string) {
  if (props.isVerifying) {
    return
  }
  props.setCode(completedCode)
  if (SIX_DIGITS_REGEX.test(completedCode)) {
    props.pendingCodeRef.current = completedCode
    props.trySubmitCode()
  }
}

function renderTOTPForm(props: RenderProps) {
  return (
    <div className="space-y-6">
      <div className="space-y-6">
        <div className="flex flex-col items-center">
          <MFAInput
            disabled={props.isVerifying}
            focusOnMount={props.focusOnMount}
            key={props.inputVersion}
            maxLength={6}
            onChange={(event) => handleTOTPChange(props, event.target.value)}
            onComplete={(completedCode) => handleTOTPComplete(props, completedCode)}
            value={props.code}
          />
        </div>
        <TrustDeviceOption {...props} />
      </div>
      {props.allowMethodSwitch && props.hasTOTP && props.hasWebAuthn && (
        <div className="space-y-6">
          <div className="border-t" />
          <Button
            className="w-full"
            disabled={props.isVerifying}
            onClick={() => {
              props.handleVerifyWebAuthn()
            }}
            type="button"
            variant="outline"
          >
            {props.isVerifying ? <LoadingSpinner className="size-4" /> : 'Use security key instead'}
          </Button>
        </div>
      )}
      <OtherMethodLinks {...props} />
      <CancelButton {...props} />
    </div>
  )
}

function backupCodeBackLabel(props: RenderProps): string {
  if (props.useYubiKeyOTP) {
    return 'Back to YubiKey OTP'
  }
  return props.useWebAuthn ? 'Back to security key' : 'Back to authenticator app'
}

function renderBackupCodeForm(props: RenderProps) {
  return (
    <CodeEntryForm
      backLabel={backupCodeBackLabel(props)}
      focusOnMount={props.focusOnMount}
      id="mfa-backup-code"
      isValid={isValidBackupCode}
      isVerifying={props.isVerifying}
      key={props.inputVersion}
      label="Backup code"
      normalize={normalizeBackupCode}
      onBack={() => {
        props.setError(null)
        props.setUseBackupCode(false)
      }}
      onSubmit={props.handleVerifyBackupCode}
      placeholder="XXXXXX-XXXXXX"
      renderCancelButton={props.renderCancelButton}
      submitLabel="Verify backup code"
      uppercase
    />
  )
}

// yubiKeyBack is the way back from the YubiKey OTP view to the user's other factor, if they have one.
function yubiKeyBack(props: RenderProps): { label: string; onBack: () => void } | undefined {
  if (!(props.hasTOTP || props.hasWebAuthn)) {
    return
  }
  const toWebAuthn = props.hasWebAuthn && (props.useWebAuthn || !props.hasTOTP)
  return {
    label: toWebAuthn ? 'Back to security key' : 'Back to authenticator app',
    onBack: () => {
      props.setError(null)
      props.setUseWebAuthn(toWebAuthn)
      props.setUseYubiKeyOTP(false)
    },
  }
}

function renderYubiKeyForm(props: RenderProps) {
  const back = yubiKeyBack(props)
  return (
    <CodeEntryForm
      backLabel={back?.label}
      focusOnMount={props.focusOnMount}
      id="mfa-yubikey-otp"
      isValid={isValidYubiKeyOTP}
      isVerifying={props.isVerifying}
      key={props.inputVersion}
      label="YubiKey OTP"
      normalize={normalizeYubiKeyOTP}
      onBack={back?.onBack}
      onSubmit={props.handleVerifyYubiKeyOTP}
      placeholder="Touch your YubiKey"
      renderCancelButton={props.renderCancelButton}
      submitLabel="Verify YubiKey OTP"
      uppercase={false}
    >
      <TrustDeviceOption {...props} />
      <OtherMethodLinks {...props} />
    </CodeEntryForm>
  )
}

export function renderFormContent(view: FormView, props: RenderProps) {
  switch (view) {
    case 'loading':
      return renderLoadingState(props)
    case 'backup':
      return renderBackupCodeForm(props)
    case 'yubikey':
      return renderYubiKeyForm(props)
    case 'webauthn':
      return renderWebAuthnForm(props)
    default:
      return renderTOTPForm(props)
  }
}
