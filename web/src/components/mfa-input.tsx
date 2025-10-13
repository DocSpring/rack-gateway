import type { ComponentPropsWithoutRef } from 'react';
import { forwardRef } from 'react';

import { Input } from '@/components/ui/input';

const SIX_DIGIT_REGEX = /^\d{6}$/;

type InputProps = ComponentPropsWithoutRef<typeof Input>;

type MFAInputProps = Omit<InputProps, 'onChange'> & {
  onChange?: (event: React.ChangeEvent<HTMLInputElement>) => void;
  onComplete?: (code: string) => void;
};

export const MFAInput = forwardRef<HTMLInputElement, MFAInputProps>(
  (
    {
      autoCapitalize,
      autoComplete,
      autoCorrect,
      inputMode,
      pattern,
      type,
      name,
      onChange,
      onComplete,
      maxLength,
      ...rest
    },
    ref,
  ) => {
    const handleChange = (event: React.ChangeEvent<HTMLInputElement>) => {
      onChange?.(event);
      const value = event.target.value.trim();
      // Auto-submit when 6 digits are entered
      if (onComplete && value.length === 6 && SIX_DIGIT_REGEX.test(value)) {
        onComplete(value);
      }
    };

    const handlePaste = (event: React.ClipboardEvent<HTMLInputElement>) => {
      const pastedText = event.clipboardData.getData('text').trim();
      // Auto-submit when pasting 6 digits
      if (
        onComplete &&
        pastedText.length === 6 &&
        SIX_DIGIT_REGEX.test(pastedText)
      ) {
        // Let the paste happen first, then trigger completion
        setTimeout(() => onComplete(pastedText), 0);
      }
    };

    return (
      <Input
        {...rest}
        autoCapitalize={autoCapitalize ?? 'none'}
        autoComplete={autoComplete ?? 'one-time-code'}
        autoCorrect={autoCorrect ?? 'off'}
        data-1p-ignore="true"
        data-lpignore="true"
        inputMode={inputMode ?? 'numeric'}
        maxLength={maxLength ?? 6}
        name={name ?? 'otp_entry'}
        onChange={handleChange}
        onPaste={handlePaste}
        pattern={pattern ?? '[0-9]*'}
        ref={ref}
        type={type ?? 'text'}
      />
    );
  },
);

MFAInput.displayName = 'MFAInput';

export default MFAInput;
