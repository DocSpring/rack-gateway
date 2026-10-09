import type { MutableRefObject, ReactNode } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { MFAInput } from '@/components/mfa-input'
import { Button } from '@/components/ui/button'
import { BackupCodeForm } from './backup-code-form'

const DIGITS_ONLY_REGEX = /^\d+$/
const SIX_DIGITS_REGEX = /^\d{6}$/

export type FormView = 'loading' | 'totp' | 'webauthn' | 'backup'

export type RenderProps = {
  showTrustDevice: boolean
  trustDevice: boolean
  setTrustDevice: (value: boolean) => void
  isVerifying: boolean
  handleVerifyWebAuthn: () => void
  allowMethodSwitch: boolean
  hasTOTP: boolean
  hasWebAuthn: boolean
  hasBackupCodes: boolean
  useWebAuthn: boolean
  setUseWebAuthn: (value: boolean) => void
  setUseBackupCode: (value: boolean) => void
  handleVerifyBackupCode: (code: string) => void
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
  props: Pick<RenderProps, 'showTrustDevice' | 'trustDevice' | 'setTrustDevice'>
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
        Trust this device for 30 days
      </label>
    </div>
  )
}

function BackupCodeLink(
  props: Pick<RenderProps, 'hasBackupCodes' | 'isVerifying' | 'setUseBackupCode'>
) {
  if (!props.hasBackupCodes) {
    return null
  }
  return (
    <div className="flex justify-center">
      <Button
        className="h-auto p-0 text-sm"
        disabled={props.isVerifying}
        onClick={() => props.setUseBackupCode(true)}
        type="button"
        variant="link"
      >
        Use a backup code
      </Button>
    </div>
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
            Trust this device for 30 days
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
      <BackupCodeLink {...props} />
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
      <BackupCodeLink {...props} />
      <CancelButton {...props} />
    </div>
  )
}

function renderBackupCodeForm(props: RenderProps) {
  return (
    <BackupCodeForm
      backLabel={props.useWebAuthn ? 'Back to security key' : 'Back to authenticator app'}
      focusOnMount={props.focusOnMount}
      isVerifying={props.isVerifying}
      key={props.inputVersion}
      onBack={() => {
        props.setError(null)
        props.setUseBackupCode(false)
      }}
      onSubmit={props.handleVerifyBackupCode}
      renderCancelButton={props.renderCancelButton}
    />
  )
}

export function renderFormContent(view: FormView, props: RenderProps) {
  switch (view) {
    case 'loading':
      return renderLoadingState(props)
    case 'backup':
      return renderBackupCodeForm(props)
    case 'webauthn':
      return renderWebAuthnForm(props)
    default:
      return renderTOTPForm(props)
  }
}
