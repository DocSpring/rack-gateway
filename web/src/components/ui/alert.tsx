import type { HTMLAttributes } from 'react';
import { forwardRef } from 'react';
import { cn } from '@/lib/utils';

const alertVariants: Record<'default' | 'destructive', string> = {
  default: 'bg-background text-foreground',
  destructive:
    'border-destructive/50 text-destructive dark:border-destructive [&>svg]:text-destructive',
};

const Alert = forwardRef<
  HTMLDivElement,
  HTMLAttributes<HTMLDivElement> & { variant?: keyof typeof alertVariants }
>(({ className, variant = 'default', ...props }, ref) => (
  <div
    className={cn(
      'relative w-full rounded-lg border border-border/60 px-4 py-3 text-sm [&>svg]:absolute [&>svg]:top-3 [&>svg]:left-4 [&>svg]:text-foreground',
      alertVariants[variant],
      className,
    )}
    ref={ref}
    role="alert"
    {...props}
  />
));
Alert.displayName = 'Alert';

const AlertDescription = forwardRef<
  HTMLParagraphElement,
  HTMLAttributes<HTMLParagraphElement>
>(({ className, ...props }, ref) => (
  <p
    className={cn('pl-7 text-sm leading-relaxed', className)}
    ref={ref}
    {...props}
  />
));
AlertDescription.displayName = 'AlertDescription';

export { Alert, AlertDescription };
