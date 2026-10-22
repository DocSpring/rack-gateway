import { type FormEvent, type ReactNode, useEffect, useRef, useState } from 'react'
import { LoadingSpinner } from '@/components/loading-spinner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface CodeEntryFormProps {
  id: string
  label: string
  placeholder: string
  submitLabel: string
  // uppercase for backup codes; YubiKey OTPs are lowercase modhex
  uppercase: boolean
  isValid: (value: string) => boolean
  normalize: (value: string) => string
  focusOnMount: boolean
  isVerifying: boolean
  // backLabel and onBack offer a way back to another method, when there is one
  backLabel?: string
  onBack?: () => void
  onSubmit: (code: string) => void
  renderCancelButton?: () => ReactNode
  // children go below the submit button (a trust-device option, a link to another method)
  children?: ReactNode
}

/**
 * A code typed (or, for a YubiKey, entered by the key) into a text field: a backup code issued at enrollment, or a
 * YubiKey's one-time password.
 */
export function CodeEntryForm({
  id,
  label,
  placeholder,
  submitLabel,
  uppercase,
  isValid,
  normalize,
  focusOnMount,
  isVerifying,
  backLabel,
  onBack,
  onSubmit,
  renderCancelButton,
  children,
}: CodeEntryFormProps) {
  const [value, setValue] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const valid = isValid(value)

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
    if (valid && !isVerifying) {
      onSubmit(normalize(value))
    }
  }

  return (
    <form className="space-y-6" onSubmit={handleSubmit}>
      <div className="space-y-2">
        <Label htmlFor={id}>{label}</Label>
        <Input
          autoCapitalize={uppercase ? 'characters' : 'none'}
          autoComplete="one-time-code"
          autoCorrect="off"
          className={`text-center font-mono tracking-widest ${uppercase ? 'uppercase' : ''}`}
          disabled={isVerifying}
          id={id}
          onChange={(event) => setValue(event.target.value)}
          placeholder={placeholder}
          ref={inputRef}
          spellCheck={false}
          value={value}
        />
      </div>
      <Button className="w-full" disabled={!valid || isVerifying} type="submit">
        {isVerifying ? <LoadingSpinner className="size-4" variant="white" /> : submitLabel}
      </Button>
      {children}
      {onBack && backLabel && (
        <>
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
        </>
      )}
      {renderCancelButton && <div className="flex justify-end">{renderCancelButton()}</div>}
    </form>
  )
}
