import { format, formatDistanceToNow, isPast } from 'date-fns'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'

type ExpiryTimeProps = {
  date?: string | Date | null
}

export function ExpiryTime({ date }: ExpiryTimeProps) {
  if (!date) {
    return <span>—</span>
  }
  const d = typeof date === 'string' ? new Date(date) : date
  if (Number.isNaN(d.getTime())) {
    return <span>—</span>
  }

  const exact = format(d, 'yyyy-MM-dd HH:mm:ss XXX')
  const isExpired = isPast(d)
  const rel = formatDistanceToNow(d, { addSuffix: true })

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className={`cursor-help ${isExpired ? 'text-destructive' : ''}`}>{exact}</span>
        </TooltipTrigger>
        <TooltipContent>
          <span className="font-mono">{rel}</span>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
