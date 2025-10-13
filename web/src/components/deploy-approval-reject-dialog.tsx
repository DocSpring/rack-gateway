import { Loader2, X } from 'lucide-react';
import { useState } from 'react';
import { Button } from './ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Label } from './ui/label';
import { Textarea } from './ui/textarea';

type DeployApprovalRejectDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (notes: string) => void;
  pending: boolean;
  requestId: string;
};

export function DeployApprovalRejectDialog({
  open,
  onOpenChange,
  onSubmit,
  pending,
  requestId,
}: DeployApprovalRejectDialogProps) {
  const [notes, setNotes] = useState('');

  const handleOpenChange = (isOpen: boolean) => {
    if (!isOpen) {
      setNotes('');
    }
    onOpenChange(isOpen);
  };

  const handleSubmit = () => {
    onSubmit(notes);
  };

  return (
    <Dialog onOpenChange={handleOpenChange} open={open}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reject deploy approval request</DialogTitle>
          <DialogDescription>
            Provide an optional reason for rejecting request {requestId}.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Label htmlFor="reject-notes">Reason (optional)</Label>
          <Textarea
            id="reject-notes"
            onChange={(event) => setNotes(event.target.value)}
            placeholder="Provide additional context for the requester"
            rows={4}
            value={notes}
          />
        </div>
        <DialogFooter>
          <Button onClick={() => handleOpenChange(false)} variant="outline">
            Cancel
          </Button>
          <Button
            disabled={pending}
            onClick={handleSubmit}
            variant="destructive"
          >
            {pending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <X className="h-4 w-4" />
            )}
            Reject request
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
