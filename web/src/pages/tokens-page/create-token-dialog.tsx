import { Copy } from 'lucide-react'
import { Button } from '../../components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../components/ui/dialog'
import { Input } from '../../components/ui/input'
import { Label } from '../../components/ui/label'
import { TooltipProvider } from '../../components/ui/tooltip'
import { TokenPermissionsEditor } from './permission-components'
import type { TokenRoleInfo } from './types'

type CreateTokenDialogProps = {
  activeRole: string | null
  availablePermissions: string[]
  canAssignPermission: (permission: string) => boolean
  errors: { name?: string; permissions?: string }
  createdToken: string | null
  createdTokenUuid: string | null
  isCreating: boolean
  isOpen: boolean
  isPermissionLoading: boolean
  onCancel: () => void
  onCopyToken: () => void
  onCopyUuid: () => void
  onOpenChange: (open: boolean) => void
  onPermissionToggle: (permission: string) => void
  onRoleSelect: (role: TokenRoleInfo) => void
  onSubmit: () => void | Promise<void>
  onTokenNameChange: (value: string) => void
  onClose: () => void
  roleShortcuts: TokenRoleInfo[]
  selectedPermissions: string[]
  selectedPermissionsSet: Set<string>
  tokenName: string
}

export function CreateTokenDialog({
  activeRole,
  availablePermissions,
  canAssignPermission,
  errors,
  createdToken,
  createdTokenUuid,
  isCreating,
  isOpen,
  isPermissionLoading,
  onCancel,
  onCopyToken,
  onCopyUuid,
  onOpenChange,
  onPermissionToggle,
  onRoleSelect,
  onSubmit,
  onTokenNameChange,
  onClose,
  roleShortcuts,
  selectedPermissions,
  selectedPermissionsSet,
  tokenName,
}: CreateTokenDialogProps) {
  return (
    <Dialog onOpenChange={onOpenChange} open={isOpen}>
      <DialogContent className="sm:max-w-xl">
        <TooltipProvider>
          <DialogHeader>
            <DialogTitle>Create API Token</DialogTitle>
            <DialogDescription>
              {createdToken
                ? "Copy the token secret now. You won't be able to see it again."
                : 'Enter a name for the new API token'}
            </DialogDescription>
          </DialogHeader>

          {createdToken ? (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>Token UUID</Label>
                <div className="flex items-center gap-2">
                  <div className="flex-1 break-all rounded-md bg-muted p-3 font-mono text-sm">
                    {createdTokenUuid}
                  </div>
                  <Button onClick={onCopyUuid} size="icon" variant="ghost">
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>
              <div className="space-y-2">
                <Label>Token Secret</Label>
                <div className="flex items-center gap-2">
                  <div className="flex-1 break-all rounded-md bg-muted p-3 font-mono text-sm">
                    {createdToken}
                  </div>
                  <Button onClick={onCopyToken} size="icon" variant="ghost">
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="name">Token Name</Label>
                <Input
                  autoCapitalize="none"
                  autoComplete="off"
                  autoCorrect="off"
                  data-1p-ignore
                  data-bwignore="true"
                  data-lpignore="true"
                  id="name"
                  inputMode="text"
                  name="token_name"
                  onChange={(e) => onTokenNameChange(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      const result = onSubmit()
                      if (result instanceof Promise) {
                        result.catch(() => {
                          /* errors handled by caller */
                        })
                      }
                    }
                  }}
                  placeholder="e.g., CI/CD Pipeline"
                  spellCheck={false}
                  value={tokenName}
                />
                {errors.name ? <p className="text-destructive text-sm">{errors.name}</p> : null}
              </div>
              <TokenPermissionsEditor
                activeRole={activeRole}
                availablePermissions={availablePermissions}
                canAssignPermission={canAssignPermission}
                error={errors.permissions}
                isPermissionLoading={isPermissionLoading}
                onPermissionToggle={onPermissionToggle}
                onRoleSelect={onRoleSelect}
                roleShortcuts={roleShortcuts}
                selectedPermissions={selectedPermissions}
                selectedPermissionsSet={selectedPermissionsSet}
              />
            </div>
          )}

          <DialogFooter>
            {createdToken ? (
              <Button onClick={onClose}>Done</Button>
            ) : (
              <>
                <Button onClick={onCancel} variant="outline">
                  Cancel
                </Button>
                <Button
                  disabled={isCreating || isPermissionLoading}
                  onClick={() => {
                    const result = onSubmit()
                    if (result instanceof Promise) {
                      result.catch(() => {
                        /* errors handled by caller */
                      })
                    }
                  }}
                >
                  {isCreating ? 'Creating...' : 'Create Token'}
                </Button>
              </>
            )}
          </DialogFooter>
        </TooltipProvider>
      </DialogContent>
    </Dialog>
  )
}
