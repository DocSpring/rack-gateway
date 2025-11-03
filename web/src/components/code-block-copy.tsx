import { Copy } from 'lucide-react'
import type { ReactNode } from 'react'
import { toast } from '@/components/ui/use-toast'
import { Button } from './ui/button'

type CodeBlockCopyProps = {
  children: ReactNode
  code: string
}

export function CodeBlockCopy({ children, code }: CodeBlockCopyProps) {
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      toast.success('Copied to clipboard')
    } catch {
      toast.error('Failed to copy to clipboard')
    }
  }

  return (
    <div className="group relative">
      {children}
      <Button
        className="absolute top-[5px] right-1 size-8 opacity-0 transition-opacity hover:opacity-100 focus-visible:opacity-100 group-hover:opacity-100"
        onClick={handleCopy}
        size="sm"
        type="button"
        variant="secondary"
      >
        <Copy className="size-4" />
      </Button>
    </div>
  )
}
