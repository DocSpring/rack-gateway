import { format, formatDistanceToNow, isPast } from 'date-fns'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'

type ExpiryTimeProps = {
  date?: string | Date | null
}

export function ExpiryTime({ date }: ExpiryTimeProps) {
  if (!date) {
    return <span>—</span>
  }
  const dateObject = typeof date === 'string' ? new Date(date) : date
  if (Number.isNaN(dateObject.getTime())) {
    return <span>—</span>
  }

  const exact = format(dateObject, 'yyyy-MM-dd HH:mm:ss XXX')
  const isExpired = isPast(dateObject)
  const rel = formatDistanceToNow(dateObject, { addSuffix: true })

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className={`cursor-help ${isExpired ? 'text-destructive' : ''}`}>{rel}</span>
        </TooltipTrigger>
        <TooltipContent>
          <span className="font-mono">{exact}</span>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
