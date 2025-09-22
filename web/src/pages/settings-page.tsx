import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { X } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { Separator } from '../components/ui/separator'
import { useAuth } from '../contexts/auth-context'
import { api } from '../lib/api'

type RackTLSCert = {
  pem: string
  fingerprint: string
  fetched_at: string
}

type SettingsErrorPayload = {
  error?: string
}

type SettingsResponse = {
  protected_env_vars: string[]
  allow_destructive_actions: boolean
  rack_tls_cert?: RackTLSCert | null
}

function extractErrorMessage(error: unknown): string | undefined {
  if (isAxiosError<SettingsErrorPayload>(error)) {
    const payload = error.response?.data
    if (typeof payload === 'string') {
      return payload
    }
    if (payload && typeof payload.error === 'string') {
      return payload.error
    }
  }
  if (error instanceof Error) {
    return error.message
  }
  return
}

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: UI glue code is easier to follow inline.
export function SettingsPage() {
  const qc = useQueryClient()
  const { user } = useAuth()
  const isAdmin = !!user?.roles?.includes('admin')

  const { data, isLoading, error } = useQuery({
    queryKey: ['settings'],
    queryFn: async () => api.get<SettingsResponse>('/.gateway/api/admin/settings'),
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const [newVar, setNewVar] = useState('')
  const envVars = data?.protected_env_vars ?? []
  const allowDestructive = data?.allow_destructive_actions ?? false
  const cert = data?.rack_tls_cert ?? null
  const certFetchedAt = cert?.fetched_at ? new Date(cert.fetched_at).toLocaleString() : null

  const saveEnvMutation = useMutation({
    mutationFn: async (vars: string[]) =>
      api.put('/.gateway/api/admin/settings/protected_env_vars', {
        protected_env_vars: vars,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['settings'] })
      toast.success('Protected env vars updated')
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to update protected env vars')
    },
  })

  const toggleDestructiveMutation = useMutation({
    mutationFn: async (allow: boolean) =>
      api.put('/.gateway/api/admin/settings/allow_destructive_actions', {
        allow_destructive_actions: allow,
      }),
    onMutate: async (allow: boolean) => {
      await qc.cancelQueries({ queryKey: ['settings'] })
      const prev = qc.getQueryData<SettingsResponse>(['settings'])
      qc.setQueryData<SettingsResponse | undefined>(['settings'], (d) =>
        d ? { ...d, allow_destructive_actions: allow } : d
      )
      return { prev }
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(['settings'], ctx.prev)
      }
      const message = err instanceof Error ? err.message : ''
      toast.error(message || 'Failed to update setting')
    },
    onSuccess: () => {
      toast.success('Setting updated')
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const refreshCertMutation = useMutation({
    mutationFn: async () =>
      api.post<RackTLSCert>('/.gateway/api/admin/settings/rack_tls_cert/refresh'),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['settings'] })
      toast.success('Rack certificate refreshed')
    },
    onError: (err: unknown) => {
      const message = extractErrorMessage(err)
      toast.error(
        'Failed to refresh rack certificate',
        message ? { description: message } : undefined
      )
    },
  })

  const addEnvVar = () => {
    const raw = newVar.trim()
    if (!raw) {
      return
    }
    const val = raw.toUpperCase()
    if (envVars.includes(val)) {
      setNewVar('')
      return
    }
    saveEnvMutation.mutate([...envVars, val])
    setNewVar('')
  }

  const removeEnvVar = (v: string) => {
    const next = envVars.filter((x) => x !== v)
    saveEnvMutation.mutate(next)
  }

  return (
    <div className="p-8">
      <div className="mb-8">
        <h1 className="font-bold text-3xl">Settings</h1>
        <p className="mt-2 text-muted-foreground">
          Configure gateway-wide behavior and safety controls
        </p>
      </div>

      {error ? (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-destructive text-sm">
          Failed to load settings
        </div>
      ) : null}

      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Protected Environment Variables</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="mb-3 text-muted-foreground text-sm">
              Keys listed here are considered sensitive and are redacted from logs and UI unless
              explicitly revealed.
            </p>
            <div className="flex gap-2">
              <div className="flex-1">
                <Label className="mb-4" htmlFor="new-var">
                  Add Key
                </Label>
                <Input
                  disabled={!isAdmin || isLoading || saveEnvMutation.isPending}
                  id="new-var"
                  onChange={(e) => setNewVar(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      addEnvVar()
                    }
                  }}
                  placeholder="e.g. DATABASE_URL"
                  value={newVar}
                />
              </div>
              <div className="flex items-end">
                <Button
                  disabled={!isAdmin || isLoading || saveEnvMutation.isPending}
                  onClick={addEnvVar}
                >
                  Add
                </Button>
              </div>
            </div>
            <Separator className="my-4" />
            <div className="flex flex-wrap gap-2">
              {(envVars || []).length === 0 ? (
                <span className="text-muted-foreground text-sm">No protected keys set</span>
              ) : (
                envVars.map((v) => (
                  <span
                    className="inline-flex items-center gap-2 rounded-md border bg-muted px-2 py-1 text-xs"
                    key={v}
                  >
                    {v}
                    <button
                      aria-label={`Remove ${v}`}
                      className="inline-flex h-6 w-6 cursor-pointer items-center justify-center rounded hover:bg-background"
                      disabled={!isAdmin || saveEnvMutation.isPending}
                      onClick={() => removeEnvVar(v)}
                      type="button"
                    >
                      <X className="h-4 w-4" />
                    </button>
                  </span>
                ))
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Destructive Actions</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="mb-3 text-muted-foreground text-sm">
              When enabled, delete/force operations are allowed globally. Disable to protect against
              accidental destructive changes.
            </p>
            <label className="flex items-center gap-3">
              <input
                checked={!!allowDestructive}
                disabled={!isAdmin || isLoading || toggleDestructiveMutation.isPending}
                onChange={(e) => toggleDestructiveMutation.mutate(e.target.checked)}
                type="checkbox"
              />
              <span className="font-medium text-sm">Allow destructive actions</span>
            </label>
          </CardContent>
        </Card>

        <Card className="md:col-span-1">
          <CardHeader>
            <CardTitle>Rack TLS Certificate</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <p className="text-muted-foreground text-sm">
              The pinned rack certificate is used to verify all proxied connections. Refresh after
              rotating rack TLS.
            </p>
            <textarea
              className="w-full rounded-md border bg-muted/40 p-3 font-mono text-xs"
              readOnly
              rows={cert ? 10 : 4}
              value={cert?.pem ?? 'No certificate pinned yet.'}
            />
            <div className="flex flex-col gap-1 text-muted-foreground text-xs">
              <span>
                <strong>Fingerprint:</strong> {cert?.fingerprint ?? '—'}
              </span>
              <span>
                <strong>Fetched:</strong> {certFetchedAt ?? '—'}
              </span>
            </div>
            <div>
              <Button
                disabled={!isAdmin || refreshCertMutation.isPending}
                onClick={() => refreshCertMutation.mutate()}
              >
                {refreshCertMutation.isPending ? 'Refreshing…' : 'Refresh'}
              </Button>
            </div>
          </CardContent>
        </Card>
        <div className="hidden md:block" />
      </div>
    </div>
  )
}
