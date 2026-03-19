import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { TablePane } from '../components/table-pane'
import { TimeAgo } from '../components/time-ago'
import { Button } from '../components/ui/button'
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
import { fetchAppProcesses } from '../lib/app-runtime'
import { DEFAULT_PER_PAGE } from '../lib/constants'
import { QUERY_KEYS } from '../lib/query-keys'

export function AppProcessesPage() {
  const { app } = useParams({ from: '/apps/$app/processes' }) as {
    app: string
  }
  const { user } = useAuth()
  const queryClient = useQueryClient()
  const roles = user?.roles ?? []
  const canStopProcesses =
    roles.includes('admin') || roles.includes('deployer') || roles.includes('ops')
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: [...QUERY_KEYS.APP_PROCESSES, app],
    queryFn: () => fetchAppProcesses(app),
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })
  const stopMutation = useMutation({
    mutationFn: async (processId: string) =>
      api.delete(`/api/v1/convox/apps/${app}/processes/${encodeURIComponent(processId)}`),
    onSuccess: async (_data, processId) => {
      toast.success(`Stopped process ${processId}`)
      await queryClient.invalidateQueries({ queryKey: [...QUERY_KEYS.APP_PROCESSES, app] })
    },
  })
  const perPage = DEFAULT_PER_PAGE
  const total = data.length
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const [page, setPage] = useState(1)
  const start = (page - 1) * perPage
  const end = Math.min(start + perPage, total)
  const rows = data.slice(start, end)

  return (
    <TablePane
      empty={data.length === 0}
      emptyMessage="No processes found"
      error={error ? (error as Error).message : null}
      loading={isLoading}
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Service</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Release</TableHead>
            <TableHead>Started</TableHead>
            {canStopProcesses ? <TableHead className="text-right">Actions</TableHead> : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((p) => (
            <TableRow key={p.id}>
              <TableCell className="font-mono text-xs">{p.id}</TableCell>
              <TableCell>{p.service}</TableCell>
              <TableCell>{p.status}</TableCell>
              <TableCell>{p.release}</TableCell>
              <TableCell>{p.started ? <TimeAgo date={p.started} /> : '—'}</TableCell>
              {canStopProcesses ? (
                <TableCell className="text-right">
                  <Button
                    aria-label={`Stop process ${p.id}`}
                    data-testid={`stop-process-${p.id}`}
                    disabled={stopMutation.isPending}
                    onClick={() => stopMutation.mutate(p.id)}
                    size="sm"
                    variant="destructive"
                  >
                    {stopMutation.isPending && stopMutation.variables === p.id ? (
                      <Loader2 className="animate-spin" />
                    ) : null}
                    Stop
                  </Button>
                </TableCell>
              ) : null}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {total > 0 && (
        <div className="mt-4 flex items-center justify-between">
          <div className="text-muted-foreground text-sm">
            Showing {start + 1}–{end} of {total} processes
          </div>
          <div className="flex gap-2">
            <Button
              disabled={page === 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              variant="outline"
            >
              Previous
            </Button>
            <Button
              disabled={page === totalPages}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              variant="outline"
            >
              Next
            </Button>
          </div>
        </div>
      )}
    </TablePane>
  )
}
