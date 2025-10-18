import { useMemo } from 'react'
import { Badge } from '../../components/ui/badge'
import { Button } from '../../components/ui/button'
import { Label } from '../../components/ui/label'
import { Tooltip, TooltipContent, TooltipTrigger } from '../../components/ui/tooltip'
import { buildPermissionGroups, normalizePermissions, permissionsEqual } from './permission-utils'
import type { PermissionOption, TokenRoleInfo } from './types'

function RoleShortcutButtons({
  roleShortcuts,
  activeRole,
  selectedPermissions,
  onRoleSelect,
  canAssignPermission,
}: {
  roleShortcuts: TokenRoleInfo[]
  activeRole: string | null
  selectedPermissions: string[]
  onRoleSelect: (role: TokenRoleInfo) => void
  canAssignPermission: (permission: string) => boolean
}) {
  return (
    <div className="space-y-2">
      <Label>Role Shortcuts</Label>
      <p className="text-muted-foreground text-sm">
        Choose a baseline permission set and optionally fine-tune the list below.
      </p>
      <div className="flex flex-wrap gap-2">
        {roleShortcuts.length === 0 ? (
          <Badge variant="outline">No predefined roles</Badge>
        ) : (
          roleShortcuts.map((role) => {
            const rolePermissions = normalizePermissions(role.permissions)
            const isRoleActive =
              activeRole === role.name && permissionsEqual(selectedPermissions, rolePermissions)
            const roleAllowed = rolePermissions.every(canAssignPermission)
            const button = (
              <Button
                disabled={!roleAllowed}
                key={role.name}
                onClick={() => onRoleSelect(role)}
                size="sm"
                variant={isRoleActive ? 'default' : 'outline'}
              >
                {role.label}
              </Button>
            )
            if (roleAllowed) {
              return button
            }
            return (
              <Tooltip delayDuration={150} key={role.name}>
                <TooltipTrigger asChild>{button}</TooltipTrigger>
                <TooltipContent align="start">
                  You don't have permission to assign this role.
                </TooltipContent>
              </Tooltip>
            )
          })
        )}
      </div>
    </div>
  )
}

function PermissionCheckboxGrid({
  availablePermissions,
  selectedPermissionsSet,
  onPermissionToggle,
  canAssignPermission,
  isLoading,
}: {
  availablePermissions: string[]
  selectedPermissionsSet: Set<string>
  onPermissionToggle: (permission: string) => void
  canAssignPermission: (permission: string) => boolean
  isLoading: boolean
}) {
  const groupedPermissions = useMemo(
    () => buildPermissionGroups(availablePermissions),
    [availablePermissions]
  )

  const topLevelOptions = useMemo(
    () => groupedPermissions.filter((group) => group.key === '*').flatMap((group) => group.options),
    [groupedPermissions]
  )

  const nestedGroups = useMemo(
    () => groupedPermissions.filter((group) => group.key !== '*'),
    [groupedPermissions]
  )

  const renderOption = (option: PermissionOption) => {
    const isSelected = selectedPermissionsSet.has(option.value)
    const assignable = canAssignPermission(option.value)

    if (assignable) {
      return (
        <label
          className="flex cursor-pointer items-start gap-3 rounded-md px-2 py-2 text-sm leading-5 transition-colors hover:bg-muted"
          key={option.value}
        >
          <input
            checked={isSelected}
            className="mt-1 h-4 w-4"
            onChange={() => onPermissionToggle(option.value)}
            type="checkbox"
          />
          <span className="font-normal">
            <span className="block font-medium capitalize">{option.title}</span>
            <span className="block text-muted-foreground text-xs">{option.description}</span>
          </span>
        </label>
      )
    }

    return (
      <Tooltip delayDuration={150} key={option.value}>
        <TooltipTrigger asChild>
          <label className="flex cursor-not-allowed items-start gap-3 rounded-md px-2 py-2 text-sm leading-5 opacity-60">
            <input
              aria-disabled={true}
              checked={isSelected}
              className="mt-1 h-4 w-4"
              disabled
              onChange={() => onPermissionToggle(option.value)}
              type="checkbox"
            />
            <span className="font-normal">
              <span className="block font-medium capitalize">{option.title}</span>
              <span className="block text-muted-foreground text-xs">{option.description}</span>
            </span>
          </label>
        </TooltipTrigger>
        <TooltipContent align="start">
          You don't have permission to perform that action.
        </TooltipContent>
      </Tooltip>
    )
  }

  return (
    <div className="space-y-2">
      <Label>Permissions</Label>
      {isLoading ? (
        <p className="text-muted-foreground text-sm">Loading permissions…</p>
      ) : (
        <div className="max-h-60 overflow-y-auto rounded-md border p-3">
          {groupedPermissions.length === 0 ? (
            <p className="text-muted-foreground text-sm">No permissions available.</p>
          ) : (
            <div className="space-y-4">
              {topLevelOptions.length > 0 && (
                <div className="space-y-1" key="__top-level-permissions">
                  {topLevelOptions.map((option) => renderOption(option))}
                </div>
              )}

              {nestedGroups.map((group) => (
                <div className="space-y-2" key={group.key}>
                  <p className="font-semibold text-foreground text-sm">{group.label}</p>
                  <div className="space-y-1">
                    {group.options.map((option) => renderOption(option))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export function TokenPermissionsEditor({
  availablePermissions,
  roleShortcuts,
  activeRole,
  selectedPermissions,
  selectedPermissionsSet,
  onRoleSelect,
  onPermissionToggle,
  canAssignPermission,
  isPermissionLoading,
  error,
}: {
  availablePermissions: string[]
  roleShortcuts: TokenRoleInfo[]
  activeRole: string | null
  selectedPermissions: string[]
  selectedPermissionsSet: Set<string>
  onRoleSelect: (role: TokenRoleInfo) => void
  onPermissionToggle: (permission: string) => void
  canAssignPermission: (permission: string) => boolean
  isPermissionLoading: boolean
  error?: string
}) {
  return (
    <div className="space-y-4">
      <RoleShortcutButtons
        activeRole={activeRole}
        canAssignPermission={canAssignPermission}
        onRoleSelect={onRoleSelect}
        roleShortcuts={roleShortcuts}
        selectedPermissions={selectedPermissions}
      />
      <PermissionCheckboxGrid
        availablePermissions={availablePermissions}
        canAssignPermission={canAssignPermission}
        isLoading={isPermissionLoading}
        onPermissionToggle={onPermissionToggle}
        selectedPermissionsSet={selectedPermissionsSet}
      />
      {error ? <p className="text-destructive text-sm">{error}</p> : null}
    </div>
  )
}
