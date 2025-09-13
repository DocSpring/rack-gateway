export function formatElapsed(start?: string | null, end?: string | null): string {
  if (!start) {
    return '—'
  }
  const s = new Date(start)
  const e = end ? new Date(end) : new Date()
  if (Number.isNaN(s.getTime()) || Number.isNaN(e.getTime())) {
    return '—'
  }
  let seconds = Math.max(0, Math.floor((e.getTime() - s.getTime()) / 1000))
  const hours = Math.floor(seconds / 3600)
  seconds -= hours * 3600
  const minutes = Math.floor(seconds / 60)
  seconds -= minutes * 60

  if (hours > 0) {
    return `${hours}h${minutes ? `${minutes}m` : ''}`
  }
  if (minutes > 0) {
    return `${minutes}m${seconds ? `${seconds}s` : ''}`
  }
  return `${seconds}s`
}
