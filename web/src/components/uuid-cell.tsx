import { Copy } from 'lucide-react'
import { Button } from './ui/button'
import { toast } from '@/components/ui/use-toast'

type UuidCellProps = {
  uuid: string
  label: string
}

export function UuidCell({ uuid, label }: UuidCellProps) {
  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (!uuid) return
    navigator.clipboard
      .writeText(uuid)
      .then(() => toast.success(`${label} copied to clipboard`))
      .catch(() => toast.error('Failed to copy to clipboard'))
  }

  return (
    <div className="flex items-center gap-2">
      <span className="font-mono text-xs">{uuid}</span>
      <Button aria-label={`Copy ${label}`} onClick={handleCopy} size="icon" variant="ghost">
        <Copy className="h-4 w-4" />
      </Button>
    </div>
  )
}
