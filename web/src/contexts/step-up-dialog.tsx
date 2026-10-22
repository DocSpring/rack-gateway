import { MFAVerificationForm } from '@/components/mfa-verification-form'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { StepUpVerification } from '@/contexts/step-up-helpers'

interface StepUpDialogProps {
  isOpen: boolean
  onOpenChange: (open: boolean) => void
  onVerify: (params: StepUpVerification) => Promise<void>
  onCancel: () => void
  showTrustDevice: boolean
}

export function StepUpDialog({
  isOpen,
  onOpenChange,
  onVerify,
  onCancel,
  showTrustDevice,
}: StepUpDialogProps) {
  return (
    <Dialog onOpenChange={onOpenChange} open={isOpen}>
      <DialogContent
        className="focus-visible:outline-none"
        onOpenAutoFocus={(event) => {
          event.preventDefault()
          if (document.activeElement instanceof HTMLElement) {
            document.activeElement.blur()
          }
        }}
      >
        <DialogHeader className="text-center sm:text-center">
          <DialogTitle className="text-center">Multi-Factor Authentication Required</DialogTitle>
        </DialogHeader>
        <DialogDescription className="text-center" />
        <MFAVerificationForm
          mode="step-up"
          onVerify={onVerify}
          renderCancelButton={() => (
            <Button onClick={onCancel} type="button" variant="outline">
              Cancel
            </Button>
          )}
          showTrustDevice={showTrustDevice}
        />
      </DialogContent>
    </Dialog>
  )
}
