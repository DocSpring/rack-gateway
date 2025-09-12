import { format, formatDistanceToNow } from 'date-fns'

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
  return <span title={exact}>{rel}</span>
}
