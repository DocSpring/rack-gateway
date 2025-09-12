import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { PageLayout } from '../components/page-layout'
import { TablePane } from '../components/table-pane'
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

type App = { name: string }
type Build = { id: string; description?: string; status: string; release: string; app?: string }

export function AllBuildsPage() {
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['all-builds'],
    queryFn: async () => {
      const apps = await api.get<App[]>('/apps')
      const lists = await Promise.all(
        apps.map(async (a) => {
          const bs = await api.get<Build[]>(`/apps/${a.name}/builds`)
          return bs.map((b) => ({ ...b, app: a.name }))
        })
      )
      return lists.flat()
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
    <PageLayout description="Builds across all apps" title="Builds">
      <TablePane
        empty={total === 0}
        emptyMessage="No builds found"
        error={error ? (error as Error).message : null}
        loading={isLoading}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>App</TableHead>
              <TableHead>ID</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Release</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((b) => (
              <TableRow key={`${b.app}/${b.id}`}>
                <TableCell>{b.app}</TableCell>
                <TableCell className="font-mono text-xs">{b.id}</TableCell>
                <TableCell>{b.description || '—'}</TableCell>
                <TableCell>{b.status}</TableCell>
                <TableCell>{b.release}</TableCell>
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
    </PageLayout>
  )
}
