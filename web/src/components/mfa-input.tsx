import type { ComponentPropsWithoutRef } from 'react'
import { forwardRef } from 'react'

import { Input } from '@/components/ui/input'

type InputProps = ComponentPropsWithoutRef<typeof Input>

type MFAInputProps = InputProps

export const MFAInput = forwardRef<HTMLInputElement, MFAInputProps>(
  ({ autoCapitalize, autoComplete, autoCorrect, inputMode, pattern, type, name, ...rest }, ref) => (
    <Input
      {...rest}
      autoCapitalize={autoCapitalize ?? 'none'}
      autoComplete={autoComplete ?? 'one-time-code'}
      autoCorrect={autoCorrect ?? 'off'}
      data-1p-ignore="true"
      data-lpignore="true"
      inputMode={inputMode ?? 'numeric'}
      name={name ?? 'otp_entry'}
      pattern={pattern ?? '[0-9]*'}
      ref={ref}
      type={type ?? 'text'}
    />
  )
)

MFAInput.displayName = 'MFAInput'

export default MFAInput
