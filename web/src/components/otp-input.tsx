import { type ComponentPropsWithoutRef, forwardRef, useRef } from 'react'
import { cn } from '@/lib/utils'

const DIGIT_REGEX = /^\d+$/

type OTPInputProps = Omit<ComponentPropsWithoutRef<'div'>, 'onChange'> & {
  value: string
  onChange: (value: string) => void
  onComplete?: (code: string) => void
  length?: number
  autoFocus?: boolean
  disabled?: boolean
}

/**
 * OTP Input component that renders separate input boxes for each digit.
 * Features:
 * - Auto-focus next input on entry
 * - Auto-focus previous input on backspace
 * - Paste support - distributes digits across inputs
 * - Auto-submit when all digits entered
 * - Keyboard navigation with arrow keys
 */
export const OTPInput = forwardRef<HTMLDivElement, OTPInputProps>(
  (
    {
      value = '',
      onChange,
      onComplete,
      length = 6,
      autoFocus = false,
      disabled = false,
      className,
      ...rest
    },
    ref
  ) => {
    const inputRefs = useRef<(HTMLInputElement | null)[]>([])

    // Ensure value is padded to length
    const paddedValue = value.padEnd(length, '')

    const handleInputChange = (index: number, inputValue: string) => {
      // Only allow digits
      const digit = inputValue.replace(/\D/g, '').slice(-1)

      // Build new value
      const newValue = paddedValue.split('')
      newValue[index] = digit
      const finalValue = newValue.join('').slice(0, length)

      onChange(finalValue)

      // Auto-focus next input if digit was entered
      if (digit && index < length - 1) {
        inputRefs.current[index + 1]?.focus()
      }

      // Check if complete
      if (finalValue.length === length && DIGIT_REGEX.test(finalValue)) {
        onComplete?.(finalValue)
      }
    }

    const handleKeyDown = (index: number, event: React.KeyboardEvent<HTMLInputElement>) => {
      // Handle backspace
      if (event.key === 'Backspace') {
        if (!paddedValue[index] && index > 0) {
          // If current input is empty, focus previous and clear it
          inputRefs.current[index - 1]?.focus()
          const newValue = paddedValue.split('')
          newValue[index - 1] = ''
          onChange(newValue.join('').slice(0, length))
          event.preventDefault()
        } else if (paddedValue[index]) {
          // Clear current digit
          const newValue = paddedValue.split('')
          newValue[index] = ''
          onChange(newValue.join('').slice(0, length))
          event.preventDefault()
        }
      }

      // Handle left arrow
      if (event.key === 'ArrowLeft' && index > 0) {
        inputRefs.current[index - 1]?.focus()
        event.preventDefault()
      }

      // Handle right arrow
      if (event.key === 'ArrowRight' && index < length - 1) {
        inputRefs.current[index + 1]?.focus()
        event.preventDefault()
      }
    }

    const handlePaste = (event: React.ClipboardEvent<HTMLInputElement>) => {
      event.preventDefault()
      const pastedText = event.clipboardData.getData('text').replace(/\D/g, '').slice(0, length)

      if (pastedText) {
        onChange(pastedText)

        // Focus the next empty input or the last input
        const nextEmptyIndex = pastedText.length < length ? pastedText.length : length - 1
        inputRefs.current[nextEmptyIndex]?.focus()

        // Check if complete
        if (pastedText.length === length) {
          onComplete?.(pastedText)
        }
      }
    }

    return (
      <fieldset
        aria-label="Verification code"
        className={cn('flex justify-center gap-2', className)}
        ref={ref}
        {...rest}
      >
        {Array.from({ length }, (_, index) => (
          <input
            autoCapitalize="none"
            autoComplete={index === 0 ? 'one-time-code' : 'off'}
            autoCorrect="off"
            autoFocus={autoFocus && index === 0}
            className={cn(
              'h-14 w-12 rounded-lg border-2 border-input bg-background text-center font-mono text-2xl transition-all',
              'focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/20',
              'disabled:cursor-not-allowed disabled:opacity-50',
              paddedValue[index] && 'border-primary'
            )}
            data-1p-ignore="true"
            data-lpignore="true"
            disabled={disabled}
            inputMode="numeric"
            // biome-ignore lint/suspicious/noArrayIndexKey: static list that never reorders
            key={index}
            maxLength={1}
            name={`otp-${index}`}
            onChange={(e) => handleInputChange(index, e.target.value)}
            onKeyDown={(e) => handleKeyDown(index, e)}
            onPaste={handlePaste}
            pattern="[0-9]"
            ref={(el) => {
              inputRefs.current[index] = el
            }}
            type="text"
            value={paddedValue[index] || ''}
          />
        ))}
      </fieldset>
    )
  }
)

OTPInput.displayName = 'OTPInput'

export default OTPInput
