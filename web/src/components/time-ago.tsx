import { format, formatDistanceToNow } from 'date-fns'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'

export function TimeAgo({ date }: { date?: string | Date | null }) {
  if (!date) {
    return <span>—</span>
  }
  const d = typeof date === 'string' ? new Date(date) : date
  if (Number.isNaN(d.getTime())) {
    return <span>—</span>
  }
  const rel = formatDistanceToNow(d, { addSuffix: true })
  const exact = format(d, 'yyyy-MM-dd HH:mm:ss XXX')
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="cursor-help">{rel}</span>
        </TooltipTrigger>
        <TooltipContent>
          <span className="font-mono">{exact}</span>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
