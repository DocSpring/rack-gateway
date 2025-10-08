import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { CodeBlockCopy } from './code-block-copy'

interface CliSetupDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  rackAlias: string
  gatewayOrigin: string
}

export function CliSetupDialog({
  open,
  onOpenChange,
  rackAlias,
  gatewayOrigin,
}: CliSetupDialogProps) {
  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Configure CLI</DialogTitle>
          <DialogDescription>
            Follow these steps to install and authenticate the Rack gateway CLI.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 text-sm">
          <p>Clone the repository and install the CLI:</p>
          <CodeBlockCopy
            code={
              'git clone git@github.com:DocSpring/rack-gateway.git\ncd rack-gateway\n./scripts/install.sh'
            }
          >
            <div className="rounded-md border bg-muted p-3 font-mono text-xs">
              <div>git clone git@github.com:DocSpring/rack-gateway.git</div>
              <div className="mt-1">cd rack-gateway</div>
              <div className="mt-1">./scripts/install.sh</div>
            </div>
          </CodeBlockCopy>

          <p className="pt-1">Authenticate the CLI against this gateway:</p>
          <CodeBlockCopy code={`rack-gateway login ${rackAlias.toLowerCase()} ${gatewayOrigin}`}>
            <div className="rounded-md border bg-muted p-3 font-mono text-xs">
              <div>
                rack-gateway login {rackAlias.toLowerCase()} {gatewayOrigin}
              </div>
            </div>
          </CodeBlockCopy>
          <p className="text-muted-foreground">
            After logging in, you can run Convox commands via the gateway using{' '}
            <span className="font-mono">rack-gateway convox …</span>
          </p>
          <p className="text-muted-foreground">
            See the{' '}
            <a
              className="underline hover:no-underline"
              href="https://github.com/DocSpring/rack-gateway/blob/main/README.md"
              rel="noreferrer noopener"
              target="_blank"
            >
              README
            </a>{' '}
            for more information.
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
