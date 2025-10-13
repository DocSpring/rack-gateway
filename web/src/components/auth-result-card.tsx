import { AlertTriangle, CheckCircle2 } from 'lucide-react'
import type { ReactNode } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

const STATUS_STYLES = {
  success: {
    iconClass: 'bg-emerald-500/10 text-emerald-500',
    defaultIcon: <CheckCircle2 className="size-7 sm:size-8" />,
  },
  error: {
    iconClass: 'bg-destructive/10 text-destructive',
    defaultIcon: <AlertTriangle className="size-7 sm:size-8" />,
  },
}

type AuthResultCardProps = {
  status: 'success' | 'error'
  title: string
  description?: ReactNode
  icon?: ReactNode
  children?: ReactNode
  contentClassName?: string
}

export function AuthResultCard({
  status,
  title,
  description,
  icon,
  children,
  contentClassName,
}: AuthResultCardProps) {
  const styles = STATUS_STYLES[status]

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-6 py-12">
      <Card className="w-full max-w-lg text-center">
        <CardHeader className="items-center justify-items-center gap-4">
          <div
            aria-hidden="true"
            className={cn(
              'flex size-12 items-center justify-center rounded-full sm:size-14',
              styles.iconClass
            )}
          >
            {icon ?? styles.defaultIcon}
          </div>
          <CardTitle aria-level={2} className="text-2xl" role="heading">
            {title}
          </CardTitle>
          {description ? (
            <p className="max-w-md text-muted-foreground text-sm">{description}</p>
          ) : null}
        </CardHeader>
        {children ? (
          <CardContent className={cn('flex flex-col items-center gap-4 p-4', contentClassName)}>
            {children}
          </CardContent>
        ) : null}
      </Card>
    </div>
  )
}

export default AuthResultCard
