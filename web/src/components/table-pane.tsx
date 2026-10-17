import { RefreshCw } from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'

interface TablePaneProps {
  title?: string
  description?: string
  headerRight?: React.ReactNode
  loading?: boolean
  error?: string | Error | null
  empty?: boolean
  emptyMessage: string
  children: React.ReactNode
}

export function TablePane({
  title,
  description,
  headerRight,
  loading = false,
  error,
  empty = false,
  emptyMessage,
  children,
}: TablePaneProps) {
  const errorMessage = typeof error === 'string' ? error : (error as Error)?.message

  return (
    <Card>
      {(title || description || headerRight) && (
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              {title ? <CardTitle>{title}</CardTitle> : null}
              {description ? <CardDescription>{description}</CardDescription> : null}
            </div>
            {headerRight ? <div>{headerRight}</div> : null}
          </div>
        </CardHeader>
      )}
      <CardContent>
        {errorMessage ? (
          <div className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-destructive text-sm">
            {errorMessage}
          </div>
        ) : null}
        {(() => {
          if (loading) {
            return (
              <div className="flex h-64 items-center justify-center">
                <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
              </div>
            )
          }
          if (empty) {
            return <div className="py-8 text-center text-muted-foreground">{emptyMessage}</div>
          }
          return children
        })()}
      </CardContent>
    </Card>
  )
}
