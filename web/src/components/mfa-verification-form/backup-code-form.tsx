import { type FormEvent, type ReactNode, useEffect, useRef, useState } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { isValidBackupCode, normalizeBackupCode } from './backup-code'

type BackupCodeFormProps = {
  focusOnMount: boolean
  isVerifying: boolean
  backLabel: string
  onBack: () => void
  onSubmit: (code: string) => void
  renderCancelButton?: () => ReactNode
}

/**
 * Lets a user who has lost their authenticator or security key sign in with one of the single-use
 * backup codes issued at enrollment.
 */
export function BackupCodeForm({
  focusOnMount,
  isVerifying,
  backLabel,
  onBack,
  onSubmit,
  renderCancelButton,
}: BackupCodeFormProps) {
  const [value, setValue] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const isValid = isValidBackupCode(value)

  // Focus manually (like OTPInput) so a dialog's onOpenAutoFocus can blur other elements first
  useEffect(() => {
    if (!focusOnMount) {
      return
    }
    const timeoutId = setTimeout(() => inputRef.current?.focus(), 0)
    return () => clearTimeout(timeoutId)
  }, [focusOnMount])

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (isValid && !isVerifying) {
      onSubmit(normalizeBackupCode(value))
    }
  }

  return (
    <form className="space-y-6" onSubmit={handleSubmit}>
      <div className="space-y-2">
        <Label htmlFor="mfa-backup-code">Backup code</Label>
        <Input
          autoCapitalize="characters"
          autoComplete="one-time-code"
          autoCorrect="off"
          className="text-center font-mono uppercase tracking-widest"
          disabled={isVerifying}
          id="mfa-backup-code"
          onChange={(event) => setValue(event.target.value)}
          placeholder="XXXXXX-XXXXXX"
          ref={inputRef}
          spellCheck={false}
          value={value}
        />
      </div>
      <Button className="w-full" disabled={!isValid || isVerifying} type="submit">
        {isVerifying ? <LoadingSpinner className="size-4" variant="white" /> : 'Verify backup code'}
      </Button>
      <div className="border-t" />
      <Button
        className="w-full"
        disabled={isVerifying}
        onClick={onBack}
        type="button"
        variant="outline"
      >
        {backLabel}
      </Button>
      {renderCancelButton && <div className="flex justify-end">{renderCancelButton()}</div>}
    </form>
  )
}
