import { CheckCircle2 } from 'lucide-react'
import type { ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

function IconCircle({ children }: { children: ReactNode }) {
  return (
    <div
      aria-hidden="true"
      className={cn(
        'flex size-12 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-500',
        'sm:size-14'
      )}
    >
      {children}
    </div>
  )
}

export function CLIAuthSuccessContent() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-6 py-12">
      <Card className="w-full max-w-lg text-center">
        <CardHeader className="items-center justify-items-center gap-4">
          <IconCircle>
            <CheckCircle2 className="size-7 sm:size-8" />
          </IconCircle>
          <CardTitle className="text-2xl">Authentication Complete</CardTitle>
          <p className="max-w-md text-muted-foreground text-sm">
            Your CLI login is approved. Return to the terminal window that prompted you and continue
            with your workflow.
          </p>
        </CardHeader>
        <CardContent className="flex flex-col items-center gap-4 p-2">
          <Button asChild className="w-full sm:w-auto">
            <a href="/.gateway/web/">Open Web UI</a>
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}

export default CLIAuthSuccessContent
