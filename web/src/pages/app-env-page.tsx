import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { isAxiosError } from 'axios'
import { Eye, EyeOff, Loader2, Plus, Trash2 } from 'lucide-react'
import type { ReactNode } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { toast } from '@/components/ui/use-toast'
import { useAuth } from '@/contexts/auth-context'
import { type EnvValuesMap, fetchAppEnv, fetchAppEnvValue, updateAppEnv } from '@/lib/api'

const MASKED_SECRET = '********************'

type EnvRowState = {
  id: string
  key: string
  value: string
  initialValue: string
  isSecret: boolean
  revealed: boolean
  new: boolean
  fetchedSecret: boolean
}

function buildRowsFromEnv(env: EnvValuesMap): EnvRowState[] {
  return Object.entries(env)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => ({
      id: `existing-${key}`,
      key,
      value,
      initialValue: value,
      isSecret: value === MASKED_SECRET,
      revealed: value !== MASKED_SECRET,
      fetchedSecret: value !== MASKED_SECRET,
      new: false,
    }))
}

function getErrorMessage(error: unknown, fallback: string): string {
  if (isAxiosError(error)) {
    const message = error.response?.data?.error
    if (typeof message === 'string' && message.trim() !== '') {
      return message
    }
  }
  if (error instanceof Error && error.message) {
    return error.message
  }
  return fallback
}

type EnvChangesSuccess = {
  success: true
  setPayload: Record<string, string>
  finalRemove: string[]
}

type EnvChangesError = {
  success: false
  message: string
}

type EnvChangesResult = EnvChangesSuccess | EnvChangesError

function prepareEnvChanges(rows: EnvRowState[], removedKeys: string[]): EnvChangesResult {
  const setPayload: Record<string, string> = {}
  const uniqueRemovals = Array.from(new Set(removedKeys))
  const seenKeys = new Set<string>()

  for (const row of rows) {
    const trimmedKey = row.key.trim()
    const value = row.value

    if (row.new && trimmedKey === '' && value === '') {
      continue
    }

    if (trimmedKey === '') {
      return {
        success: false,
        message: 'Environment variable key is required.',
      }
    }

    if (seenKeys.has(trimmedKey)) {
      return {
        success: false,
        message: `Duplicate environment variable key: ${trimmedKey}`,
      }
    }
    seenKeys.add(trimmedKey)

    if (!row.new && value === row.initialValue) {
      continue
    }

    if (value === MASKED_SECRET && row.initialValue === MASKED_SECRET) {
      continue
    }

    setPayload[trimmedKey] = value
  }

  const finalRemove = uniqueRemovals.filter((key) => !Object.hasOwn(setPayload, key))

  return { success: true, setPayload, finalRemove }
}

type EnvRowProps = {
  row: EnvRowState
  canEdit: boolean
  isSaving: boolean
  revealPending: boolean
  onKeyChange: (rowId: string, nextKey: string) => void
  onValueChange: (rowId: string, nextValue: string) => void
  onToggleSecretVisibility: (row: EnvRowState) => void
  onDeleteRow: (row: EnvRowState) => void
}

function EnvRow({
  row,
  canEdit,
  isSaving,
  revealPending,
  onKeyChange,
  onValueChange,
  onToggleSecretVisibility,
  onDeleteRow,
}: EnvRowProps) {
  const keyDisabled = !(canEdit && row.new) || isSaving
  const valueDisabled = !canEdit || isSaving
  const toggleDisabled = !canEdit || isSaving || (!row.revealed && revealPending)

  let revealIcon: ReactNode = null
  if (revealPending) {
    revealIcon = <Loader2 className="h-4 w-4 animate-spin" />
  } else if (row.revealed) {
    revealIcon = <EyeOff className="h-4 w-4" />
  } else {
    revealIcon = <Eye className="h-4 w-4" />
  }

  const revealAriaLabel = row.revealed ? 'Hide secret' : 'Reveal secret'

  return (
    <div className="flex flex-col gap-3 p-4 md:flex-row md:items-start" data-env-key={row.key}>
      <div className="w-full md:w-1/4">
        <Input
          aria-label="Environment key"
          disabled={keyDisabled}
          onChange={(event) => onKeyChange(row.id, event.target.value)}
          placeholder="KEY"
          value={row.key}
        />
      </div>
      <div className="flex-1">
        <Input
          aria-label="Environment value"
          disabled={valueDisabled}
          onChange={(event) => onValueChange(row.id, event.target.value)}
          placeholder={row.isSecret && !row.revealed ? '********' : 'value'}
          type={row.isSecret && !row.revealed ? 'password' : 'text'}
          value={row.value}
        />
      </div>
      <div className="flex items-center gap-2 md:w-auto">
        {row.isSecret ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                aria-label={revealAriaLabel}
                disabled={toggleDisabled}
                onClick={() => onToggleSecretVisibility(row)}
                size="icon"
                variant="ghost"
              >
                {revealIcon}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{revealAriaLabel}</TooltipContent>
          </Tooltip>
        ) : null}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label="Delete env var"
              disabled={!canEdit || isSaving}
              onClick={() => onDeleteRow(row)}
              size="icon"
              variant="ghost"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Remove</TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
}

export function AppEnvPage() {
  const { app } = useParams({ from: '/apps/$app/env' }) as { app: string }
  const { user } = useAuth()
  const queryClient = useQueryClient()

  const roles = user?.roles ?? []
  const canEdit = roles.includes('admin') || roles.includes('deployer')

  const envQuery = useQuery({
    queryKey: ['app-env', app],
    queryFn: () => fetchAppEnv(app),
    refetchOnWindowFocus: false,
    staleTime: 0,
  })

  const [rows, setRows] = useState<EnvRowState[]>([])
  const [removedKeys, setRemovedKeys] = useState<string[]>([])
  const [lastReleaseId, setLastReleaseId] = useState<string | null>(null)
  const [revealingKey, setRevealingKey] = useState<string | null>(null)
  const shouldResyncRef = useRef(true)
  const lastSyncedAtRef = useRef<number | null>(null)
  const latestEnvData = envQuery.data
  const lastUpdatedAt = envQuery.dataUpdatedAt

  const hasChanges = useMemo(() => {
    if (removedKeys.length > 0) {
      return true
    }
    return rows.some((row) => {
      if (row.new) {
        return row.key.trim() !== '' || row.value !== ''
      }
      if (row.value === row.initialValue) {
        return false
      }
      if (row.value === MASKED_SECRET && row.initialValue === MASKED_SECRET) {
        return false
      }
      return true
    })
  }, [rows, removedKeys])

  useEffect(() => {
    if (!latestEnvData) {
      return
    }

    const hasNewFetch =
      typeof lastUpdatedAt === 'number' && lastSyncedAtRef.current !== lastUpdatedAt

    if (!hasChanges || shouldResyncRef.current || hasNewFetch) {
      setRows(buildRowsFromEnv(latestEnvData))
      setRemovedKeys([])
      shouldResyncRef.current = false
      if (typeof lastUpdatedAt === 'number') {
        lastSyncedAtRef.current = lastUpdatedAt
      }
    }
  }, [latestEnvData, lastUpdatedAt, hasChanges])

  const revealMutation = useMutation({
    mutationFn: (key: string) => fetchAppEnvValue(app, key, true),
    onMutate: (key) => {
      setRevealingKey(key)
    },
    onSuccess: (value, key) => {
      setRows((current) =>
        current.map((row) =>
          row.key === key
            ? {
                ...row,
                value: value ?? '',
                initialValue: value ?? '',
                revealed: true,
                fetchedSecret: true,
              }
            : row
        )
      )
    },
    onError: (err) => {
      toast.error(getErrorMessage(err, 'Failed to reveal secret value.'))
    },
    onSettled: () => {
      setRevealingKey(null)
    },
  })

  const saveMutation = useMutation({
    mutationFn: async (payload: { set: Record<string, string>; remove: string[] }) =>
      updateAppEnv(app, payload.set, payload.remove),
    onSuccess: async (data) => {
      setLastReleaseId(data.release_id ?? null)
      toast.success(
        data.release_id ? `Environment updated (release ${data.release_id})` : 'Environment updated'
      )
      setRemovedKeys([])
      shouldResyncRef.current = true
      await queryClient.invalidateQueries({ queryKey: ['app-env', app] })
    },
    onError: (err) => {
      toast.error(getErrorMessage(err, 'Failed to update environment.'))
    },
  })

  const handleAddRow = () => {
    if (!canEdit) {
      return
    }
    setRows((current) => [
      ...current,
      {
        id: `new-${Date.now()}-${Math.random()}`,
        key: '',
        value: '',
        initialValue: '',
        isSecret: false,
        revealed: true,
        new: true,
        fetchedSecret: true,
      },
    ])
  }

  const handleDeleteRow = (row: EnvRowState) => {
    if (!canEdit) {
      return
    }
    setRows((current) => current.filter((item) => item.id !== row.id))
    if (!row.new) {
      setRemovedKeys((prev) => (prev.includes(row.key) ? prev : [...prev, row.key]))
    }
  }

  const handleKeyChange = (rowId: string, nextKey: string) => {
    setRows((current) => current.map((row) => (row.id === rowId ? { ...row, key: nextKey } : row)))
  }

  const handleValueChange = (rowId: string, nextValue: string) => {
    setRows((current) =>
      current.map((row) => (row.id === rowId ? { ...row, value: nextValue } : row))
    )
  }

  const handleToggleSecretVisibility = (row: EnvRowState) => {
    if (!canEdit || isSaving) {
      return
    }
    if (row.revealed) {
      setRows((current) =>
        current.map((item) => (item.id === row.id ? { ...item, revealed: false } : item))
      )
      return
    }
    if (row.fetchedSecret) {
      setRows((current) =>
        current.map((item) => (item.id === row.id ? { ...item, revealed: true } : item))
      )
      return
    }
    if (revealMutation.isPending && revealingKey === row.key) {
      return
    }
    revealMutation.mutate(row.key)
  }

  const handleCancel = async () => {
    if (envQuery.isLoading) {
      return
    }
    setRemovedKeys([])
    setLastReleaseId(null)
    shouldResyncRef.current = true
    await envQuery.refetch()
  }

  const handleSave = async () => {
    if (!canEdit || saveMutation.isPending) {
      return
    }

    const result = prepareEnvChanges(rows, removedKeys)
    if (!result.success) {
      toast.error(result.message)
      return
    }

    const { setPayload, finalRemove } = result

    if (Object.keys(setPayload).length === 0 && finalRemove.length === 0) {
      toast.success('No changes to save.')
      return
    }

    try {
      await saveMutation.mutateAsync({ set: setPayload, remove: finalRemove })
    } catch {
      // Error handled via mutation onError toast.
    }
  }

  const isSaving = saveMutation.isPending
  const saveDisabled = !(canEdit && hasChanges) || isSaving
  const isLoading = envQuery.isLoading
  const envError = envQuery.error as Error | null

  return (
    <TooltipProvider>
      <Card className="flex h-full flex-col">
        <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <CardTitle>Environment</CardTitle>
          <div className="flex items-center gap-2">
            {!canEdit && (
              <span className="text-muted-foreground text-sm">You have read-only access.</span>
            )}
            <Button disabled={!canEdit} onClick={handleAddRow} variant="outline">
              <Plus className="mr-2 h-4 w-4" /> Add Variable
            </Button>
          </div>
        </CardHeader>
        <CardContent className="flex-1 overflow-y-auto p-0">
          {(() => {
            if (isLoading) {
              return (
                <div className="flex h-full items-center justify-center p-6 text-muted-foreground">
                  <Loader2 className="mr-2 h-5 w-5 animate-spin" /> Loading environment…
                </div>
              )
            }

            if (envError) {
              return <div className="p-6 text-destructive">{envError.message}</div>
            }

            if (rows.length === 0) {
              return (
                <div className="p-6 text-muted-foreground">No environment variables found.</div>
              )
            }

            return (
              <div className="divide-y">
                {rows.map((row) => (
                  <EnvRow
                    canEdit={canEdit}
                    isSaving={isSaving}
                    key={row.id}
                    onDeleteRow={handleDeleteRow}
                    onKeyChange={handleKeyChange}
                    onToggleSecretVisibility={handleToggleSecretVisibility}
                    onValueChange={handleValueChange}
                    revealPending={revealMutation.isPending && revealingKey === row.key}
                    row={row}
                  />
                ))}
              </div>
            )
          })()}
        </CardContent>
        <CardFooter className="sticky bottom-0 flex flex-col gap-2 border-t bg-card/95 p-4 backdrop-blur">
          {lastReleaseId && (
            <div className="text-muted-foreground text-sm">
              Last update created release {lastReleaseId}. Releases are promoted on the next deploy.
            </div>
          )}
          <div className="flex w-full items-center justify-end gap-2">
            <Button
              disabled={isSaving || (!hasChanges && removedKeys.length === 0)}
              onClick={handleCancel}
              variant="outline"
            >
              Cancel
            </Button>
            <Button disabled={saveDisabled} onClick={handleSave}>
              {isSaving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              Save Changes
            </Button>
          </div>
        </CardFooter>
      </Card>
    </TooltipProvider>
  )
}
