import { Copy } from 'lucide-react'
import { type ReactNode, useState } from 'react'
import { Button } from './ui/button'
import { toast } from './ui/use-toast'

type CodeBlockCopyProps = {
  children: ReactNode
  code: string
}

export function CodeBlockCopy({ children, code }: CodeBlockCopyProps) {
  const [isHovered, setIsHovered] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      toast.success('Copied to clipboard')
    } catch {
      toast.error('Failed to copy to clipboard')
    }
  }

  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: Hover detection for showing copy button
    <div
      className="group relative"
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      role="presentation"
    >
      {children}
      {isHovered && (
        <Button
          className="absolute top-[5px] right-1 size-8 opacity-70 hover:opacity-100"
          onClick={handleCopy}
          size="sm"
          type="button"
          variant="secondary"
        >
          <Copy className="size-4" />
        </Button>
      )}
    </div>
  )
}
