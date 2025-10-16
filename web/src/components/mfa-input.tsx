import type { ComponentPropsWithoutRef } from 'react'
import { forwardRef, useState } from 'react'

import { OTPInput } from '@/components/otp-input'

type OTPInputProps = ComponentPropsWithoutRef<typeof OTPInput>

type MFAInputProps = Omit<OTPInputProps, 'onChange' | 'value' | 'length'> & {
  value?: string
  onChange?: (event: React.ChangeEvent<HTMLInputElement>) => void
  onComplete?: (code: string) => void
  maxLength?: number
  // Legacy props that are ignored (for backward compatibility)
  placeholder?: string
  id?: string
  required?: boolean
}

/**
 * MFA Input component that wraps OTPInput to maintain backward compatibility.
 * Converts the new OTPInput API (value/onChange with string) to the old API
 * (onChange with event).
 */
export const MFAInput = forwardRef<HTMLFieldSetElement, MFAInputProps>(
  (
    {
      value: externalValue,
      onChange,
      onComplete,
      maxLength = 6,
      autoFocus,
      placeholder,
      id,
      required,
      ...rest
    },
    ref
  ) => {
    const [internalValue, setInternalValue] = useState('')
    const value = externalValue ?? internalValue

    const handleChange = (newValue: string) => {
      // Update internal state if not controlled
      if (externalValue === undefined) {
        setInternalValue(newValue)
      }

      // Call onChange with synthetic event for backward compatibility
      if (onChange) {
        const syntheticEvent = {
          target: { value: newValue },
          currentTarget: { value: newValue },
        } as React.ChangeEvent<HTMLInputElement>
        onChange(syntheticEvent)
      }
    }

    return (
      <OTPInput
        {...rest}
        autoFocus={autoFocus}
        id={id}
        length={maxLength}
        onChange={handleChange}
        onComplete={onComplete}
        ref={ref}
        value={value}
      />
    )
  }
)

MFAInput.displayName = 'MFAInput'

export default MFAInput
