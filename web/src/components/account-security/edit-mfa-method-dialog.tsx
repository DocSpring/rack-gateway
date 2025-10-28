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
  onLabelChange: (value: string) => void
  onCancel: () => void
  onSubmit: () => void
  isSubmitting: boolean
}

export function EditMfaMethodDialog({
  open,
  label,
  onLabelChange,
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
