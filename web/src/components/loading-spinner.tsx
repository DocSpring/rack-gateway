import { cn } from '@/lib/utils'

type LoadingSpinnerVariant = 'primary' | 'white'

export function LoadingSpinner({
  className,
  variant = 'primary',
}: {
  className?: string
  variant?: LoadingSpinnerVariant
}) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'inline-flex size-5 animate-spin rounded-full border-2',
        variant === 'white'
          ? 'border-white/30 border-t-white'
          : 'border-primary/40 border-t-primary',
        className
      )}
    />
  )
}
