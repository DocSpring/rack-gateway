import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
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
import { api } from '../lib/api'
import { DEFAULT_PER_PAGE } from '../lib/constants'

type Process = {
  id: string
  service: string
  name?: string
  status: string
  release: string
  command?: string
  started?: string
}

export function AppProcessesPage() {
  const { app } = useParams({ from: '/apps/$app/processes' }) as {
    app: string
  }
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['app-processes', app],
    queryFn: async () => {
      const ps = await api.get<
        {
          id: string
          service?: string
          name?: string
          status: string
          release: string
          command?: string
          started?: string
        }[]
      >(`/apps/${app}/processes`)
      return ps.map((p) => ({
        ...p,
        service: p.service ?? p.name ?? '',
      })) as Process[]
    },
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
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
