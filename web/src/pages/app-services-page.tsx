import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { Loader2, Pencil, Save } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { TablePane } from '../components/table-pane'
import { Button } from '../components/ui/button'
import { Input } from '../components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import { toast } from '../components/ui/use-toast'
import { useAuth } from '../contexts/auth-context'
import { useMutation } from '../hooks/use-mutation'
import { api } from '../lib/api'
import { countProcessesByService, fetchAppProcesses, fetchAppServices } from '../lib/app-runtime'
import { QUERY_KEYS } from '../lib/query-keys'

export function AppServicesPage() {
  const { app } = useParams({ from: '/apps/$app/services' }) as {
    app: string
  }
  const { user } = useAuth()
  const queryClient = useQueryClient()
  const [editingService, setEditingService] = useState<string | null>(null)
  const [draftCount, setDraftCount] = useState('')
  const [pendingService, setPendingService] = useState<string | null>(null)

  const roles = user?.roles ?? []
  const canScale = roles.includes('admin') || roles.includes('deployer')

  const servicesQuery = useQuery({
    queryKey: [...QUERY_KEYS.APP_SERVICES, app],
    queryFn: () => fetchAppServices(app),
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const processesQuery = useQuery({
    queryKey: [...QUERY_KEYS.APP_PROCESSES, app],
    queryFn: () => fetchAppProcesses(app),
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })

  const scaleMutation = useMutation({
    mutationFn: async ({ serviceName, count }: { serviceName: string; count: number }) =>
      api.put(`/api/v1/convox/apps/${app}/services/${encodeURIComponent(serviceName)}`, undefined, {
        params: { count },
      }),
    onSuccess: async (_data, variables) => {
      toast.success(`Scaled ${variables.serviceName} to ${variables.count}`)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.APP_SERVICES, app] }),
        queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.APP_PROCESSES, app] }),
      ])
      setEditingService(null)
      setDraftCount('')
    },
    onSettled: () => {
      setPendingService(null)
    },
  })

  const services = servicesQuery.data ?? []
  const processCounts = countProcessesByService(processesQuery.data ?? [])
  const error = servicesQuery.error ?? processesQuery.error
  const loading = servicesQuery.isLoading || processesQuery.isLoading
  const draftValue = Number(draftCount.trim())
  const draftIsValid = draftCount.trim() !== '' && Number.isInteger(draftValue) && draftValue >= 0

  const startEditing = (serviceName: string, count: number) => {
    setEditingService(serviceName)
    setDraftCount(String(count))
  }

  const handleScaleSubmit = (event: FormEvent<HTMLFormElement>, serviceName: string) => {
    event.preventDefault()
    if (!draftIsValid) {
      toast.error('Scale must be a whole number greater than or equal to 0')
      return
    }

    setPendingService(serviceName)
    scaleMutation.mutate({
      serviceName,
      count: draftValue,
    })
  }

  return (
    <TablePane
      description="Compare desired scale with currently running processes."
      empty={services.length === 0}
      emptyMessage="No services found"
      error={error ? (error as Error).message : null}
      loading={loading}
      title="Services"
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Service</TableHead>
            <TableHead>Processes</TableHead>
            <TableHead>Scale</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {services.map((service) => {
            const isEditing = editingService === service.name && canScale
            const isSaving = pendingService === service.name && scaleMutation.isPending

            return (
              <TableRow data-testid={`service-row-${service.name}`} key={service.name}>
                <TableCell className="font-medium">{service.name}</TableCell>
                <TableCell className="tabular-nums">{processCounts[service.name] ?? 0}</TableCell>
                <TableCell>
                  {isEditing ? (
                    <form
                      className="flex items-center gap-2"
                      onSubmit={(event) => handleScaleSubmit(event, service.name)}
                    >
                      <Input
                        aria-label={`Scale for ${service.name}`}
                        className="h-8 w-20"
                        inputMode="numeric"
                        min={0}
                        onChange={(event) => setDraftCount(event.target.value)}
                        type="number"
                        value={draftCount}
                      />
                      <Button
                        aria-label={`Save scale for ${service.name}`}
                        data-testid={`service-save-${service.name}`}
                        disabled={!draftIsValid || isSaving}
                        size="icon"
                        type="submit"
                        variant="success"
                      >
                        {isSaving ? <Loader2 className="animate-spin" /> : <Save />}
                      </Button>
                    </form>
                  ) : (
                    <div className="flex items-center gap-2">
                      <span className="tabular-nums">{service.count}</span>
                      {canScale ? (
                        <Button
                          aria-label={`Edit scale for ${service.name}`}
                          data-testid={`service-edit-${service.name}`}
                          onClick={() => startEditing(service.name, service.count)}
                          size="icon"
                          type="button"
                          variant="ghost"
                        >
                          <Pencil />
                        </Button>
                      ) : null}
                    </div>
                  )}
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </TablePane>
  )
}
