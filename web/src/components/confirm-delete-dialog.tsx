import type { ReactNode } from 'react';
import { useEffect, useMemo, useState } from 'react';
import { Button } from './ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Input } from './ui/input';
import { Label } from './ui/label';

export type ConfirmDeleteDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: ReactNode;
  onConfirm: () => void | Promise<void>;
  busy?: boolean;
  confirmText?: string;
  confirmButtonText?: string;
  busyText?: string;
  inputLabel?: string;
  inputPlaceholder?: string;
  inputId?: string;
};

const normalize = (value: string) => value.trim().toUpperCase();

export function ConfirmDeleteDialog({
  open,
  onOpenChange,
  title,
  description,
  onConfirm,
  busy = false,
  confirmText = 'DELETE',
  confirmButtonText = 'Delete',
  busyText,
  inputLabel = 'Confirmation',
  inputPlaceholder,
  inputId = 'confirm-delete-input',
}: ConfirmDeleteDialogProps) {
  const [value, setValue] = useState('');
  const normalizedConfirm = useMemo(
    () => normalize(confirmText),
    [confirmText],
  );

  useEffect(() => {
    if (!open) {
      setValue('');
    }
  }, [open]);

  const placeholder = useMemo(() => {
    if (inputPlaceholder) {
      return inputPlaceholder;
    }
    return `Type ${confirmText.toUpperCase()} to confirm`;
  }, [confirmText, inputPlaceholder]);

  const disabled = busy || normalize(value) !== normalizedConfirm;
  const confirmBusyLabel = busyText ?? `${confirmButtonText}...`;

  const handleConfirm = () => {
    if (disabled) {
      return;
    }
    onConfirm();
  };

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Label htmlFor={inputId}>{inputLabel}</Label>
          <Input
            autoCapitalize="none"
            autoComplete="off"
            autoCorrect="off"
            data-1p-ignore
            data-bwignore="true"
            data-lpignore="true"
            id={inputId}
            onChange={(event) => setValue(event.target.value)}
            placeholder={placeholder}
            value={value}
          />
        </div>
        <DialogFooter>
          <Button
            disabled={busy}
            onClick={() => onOpenChange(false)}
            variant="outline"
          >
            Cancel
          </Button>
          <Button
            disabled={disabled}
            onClick={handleConfirm}
            variant="destructive"
          >
            {busy ? confirmBusyLabel : confirmButtonText}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
