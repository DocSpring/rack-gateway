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

type Release = {
  id: string
  description?: string
  version?: number
  created?: string
  created_by?: string
}
export function AppReleasesPage() {
  const { app } = useParams({ from: '/apps/$app/releases' }) as { app: string }
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['app-releases', app],
    queryFn: async () => {
      const items = await api.get<Release[]>(`/api/v1/convox/apps/${app}/releases`)
      try {
        const ids = Array.from(new Set(items.map((r) => r.id))).join(',')
        if (ids) {
          const map = await api.get<Record<string, { email: string; name: string }>>(
            `/api/v1/created-by?type=release&ids=${encodeURIComponent(ids)}`
          )
          for (const r of items) {
            const m = map[r.id]
            if (m) {
              r.created_by = m.email || m.name
            }
          }
        }
      } catch (_e) {
        // ignore
      }
      return items
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
      emptyMessage="No releases found"
      error={error ? (error as Error).message : null}
      loading={isLoading}
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Description</TableHead>
            <TableHead>Version</TableHead>
            <TableHead>Created By</TableHead>
            <TableHead>Created</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((r) => (
            <TableRow key={r.id}>
              <TableCell className="font-mono text-xs">{r.id}</TableCell>
              <TableCell>{r.description || '—'}</TableCell>
              <TableCell>{r.version ?? '—'}</TableCell>
              <TableCell>{r.created_by || '—'}</TableCell>
              <TableCell>{r.created ? <TimeAgo date={r.created} /> : '—'}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {total > 0 && (
        <div className="mt-4 flex items-center justify-between">
          <div className="text-muted-foreground text-sm">
            Showing {start + 1}–{end} of {total} releases
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
