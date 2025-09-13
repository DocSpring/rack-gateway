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
import { formatElapsed } from '../lib/time'

type Build = {
  id: string
  description: string
  status: string
  release: string
  started?: string
  ended?: string
}

export function AppBuildsPage() {
  const { app } = useParams({ from: '/apps/$app/builds' }) as { app: string }
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['app-builds', app],
    queryFn: async () => api.get<Build[]>(`/apps/${app}/builds`),
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
      emptyMessage="No builds found"
      error={error ? (error as Error).message : null}
      loading={isLoading}
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Description</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Release</TableHead>
            <TableHead>Started</TableHead>
            <TableHead>Elapsed</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((b) => (
            <TableRow key={b.id}>
              <TableCell className="font-mono text-xs">{b.id}</TableCell>
              <TableCell className="max-w-[420px] whitespace-normal break-words">
                {b.description}
              </TableCell>
              <TableCell>{b.status}</TableCell>
              <TableCell>{b.release}</TableCell>
              <TableCell>{b.started ? <TimeAgo date={b.started} /> : '—'}</TableCell>
              <TableCell>{formatElapsed(b.started, b.ended)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {total > 0 && (
        <div className="mt-4 flex items-center justify-between">
          <div className="text-muted-foreground text-sm">
            Showing {start + 1}–{end} of {total} builds
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
