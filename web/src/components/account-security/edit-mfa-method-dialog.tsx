import type { FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

type EditMfaMethodDialogProps = {
  open: boolean
  label: string
  methodType: string
  cliCapable: boolean
  onLabelChange: (value: string) => void
  onCliCapableChange: (value: boolean) => void
  onCancel: () => void
  onSubmit: () => void
  isSubmitting: boolean
}

export function EditMfaMethodDialog({
  open,
  label,
  methodType,
  cliCapable,
  onLabelChange,
  onCliCapableChange,
  onCancel,
  onSubmit,
  isSubmitting,
}: EditMfaMethodDialogProps) {
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    onSubmit()
  }

  return (
    <Dialog onOpenChange={(next) => !next && onCancel()} open={open}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit MFA Method Label</DialogTitle>
          <DialogDescription>Update the label for this MFA method.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="edit-label">Label</Label>
              <Input
                autoFocus
                id="edit-label"
                maxLength={150}
                onChange={(event) => onLabelChange(event.target.value)}
                placeholder="My Security Key"
                required
                value={label}
              />
            </div>
            {methodType === 'webauthn' && (
              <div className="rounded-md border border-border p-4">
                <label className="flex items-start gap-3">
                  <input
                    checked={cliCapable}
                    className="mt-1"
                    id="cli-capable"
                    onChange={(event) => onCliCapableChange(event.target.checked)}
                    type="checkbox"
                  />
                  <div className="grid gap-1.5 leading-none">
                    <span className="font-medium text-sm">CLI Compatible</span>
                    <p className="text-muted-foreground text-sm">
                      Enable this for hardware security keys (YubiKey, etc). Disable for
                      browser-only authenticators like 1Password.
                    </p>
                  </div>
                </label>
              </div>
            )}
          </div>
          <DialogFooter className="mt-4">
            <Button onClick={onCancel} type="button" variant="outline">
              Cancel
            </Button>
            <Button disabled={isSubmitting || !label.trim()} type="submit">
              {isSubmitting ? 'Saving...' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
