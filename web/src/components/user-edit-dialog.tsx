import { useEffect, useMemo, useState } from 'react';
import type { RoleName } from '../lib/api';
import { AVAILABLE_ROLES } from '../lib/api';
import { ROLE_PRIORITY } from '../lib/user-roles';
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

export type UserEditDialogMode = 'create' | 'edit';

export type UserEditDialogValues = {
  email: string;
  name: string;
  role: RoleName;
};

type UserEditDialogProps = {
  open: boolean;
  mode: UserEditDialogMode;
  initialEmail: string;
  initialName: string;
  initialRole: RoleName;
  busy?: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (values: UserEditDialogValues) => Promise<void>;
};

const ROLE_ENTRIES = ROLE_PRIORITY.map((role) => [
  role,
  AVAILABLE_ROLES[role],
]) as [RoleName, (typeof AVAILABLE_ROLES)[RoleName]][];

const formatRoleName = (value: string) =>
  value.replace(/\b\w/g, (char) => char.toUpperCase());

export function UserEditDialog({
  open,
  mode,
  initialEmail,
  initialName,
  initialRole,
  busy = false,
  onOpenChange,
  onSubmit,
}: UserEditDialogProps) {
  const [email, setEmail] = useState(initialEmail);
  const [name, setName] = useState(initialName);
  const [role, setRole] = useState<RoleName>(initialRole);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) {
      return;
    }
    setEmail(initialEmail);
    setName(initialName);
    setRole(initialRole);
  }, [open, initialEmail, initialName, initialRole]);

  const dialogTitle = useMemo(
    () => (mode === 'create' ? 'Add User' : 'Edit User'),
    [mode],
  );

  const dialogDescription = useMemo(
    () =>
      mode === 'create'
        ? 'Add a new user to the gateway and assign their initial role.'
        : 'Update account details and permissions for this user.',
    [mode],
  );

  const submitLabel = mode === 'create' ? 'Add User' : 'Save Changes';
  const isBusy = submitting || busy;

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (isBusy) {
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit({ email: email.trim(), name: name.trim(), role });
      onOpenChange(false);
    } catch (_error) {
      // Parent is responsible for surfacing the error; keep dialog open.
    } finally {
      setSubmitting(false);
    }
  };

  const handleCancel = () => {
    if (isBusy) {
      return;
    }
    onOpenChange(false);
  };

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{dialogTitle}</DialogTitle>
          <DialogDescription>{dialogDescription}</DialogDescription>
        </DialogHeader>

        <form className="space-y-4" onSubmit={handleSubmit}>
          <div className="space-y-2">
            <Label htmlFor="user-edit-email">Email</Label>
            <Input
              autoCapitalize="none"
              autoComplete="email"
              autoCorrect="off"
              data-1p-ignore
              data-bwignore="true"
              data-lpignore="true"
              disabled={isBusy}
              id="user-edit-email"
              inputMode="email"
              onChange={(event) => setEmail(event.target.value)}
              required
              spellCheck={false}
              type="email"
              value={email}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="user-edit-name">Name</Label>
            <Input
              autoCapitalize="words"
              autoComplete="name"
              autoCorrect="off"
              disabled={isBusy}
              id="user-edit-name"
              onChange={(event) => setName(event.target.value)}
              required
              spellCheck={false}
              value={name}
            />
          </div>

          <div className="space-y-2">
            <Label>Role</Label>
            <div className="space-y-2">
              {ROLE_ENTRIES.map(([roleKey, meta]) => (
                <label
                  className={`flex cursor-pointer items-center justify-between rounded-lg border p-3 transition-colors ${
                    role === roleKey
                      ? 'border-primary bg-primary/10'
                      : 'hover:bg-accent'
                  }`}
                  key={roleKey}
                >
                  <div className="flex items-start gap-3">
                    <input
                      checked={role === roleKey}
                      className="mt-1 h-4 w-4"
                      disabled={isBusy}
                      name="user-role"
                      onChange={() => setRole(roleKey)}
                      type="radio"
                      value={roleKey}
                    />
                    <div>
                      <div className="font-medium">
                        {formatRoleName(meta.name)}
                      </div>
                      <div className="text-muted-foreground text-sm">
                        {meta.description}
                      </div>
                    </div>
                  </div>
                </label>
              ))}
            </div>
          </div>

          <DialogFooter>
            <Button onClick={handleCancel} type="button" variant="outline">
              Cancel
            </Button>
            <Button disabled={isBusy} type="submit">
              {isBusy ? 'Saving...' : submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
