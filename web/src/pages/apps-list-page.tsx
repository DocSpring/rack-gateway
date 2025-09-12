import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { PageLayout } from '../components/page-layout'
import { TablePane } from '../components/table-pane'
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

type App = {
  name: string
  status: string
  generation?: string
  release?: string
}

export function AppsListPage() {
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['apps-list'],
    queryFn: async () => api.get<App[]>('/apps'),
  })
  const perPage = DEFAULT_PER_PAGE
  const total = data.length
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const [page, setPage] = useState(1)
  const start = (page - 1) * perPage
  const end = Math.min(start + perPage, total)
  const rows = data.slice(start, end)

  return (
    <PageLayout description="All apps on the rack" title="Apps">
      <TablePane
        empty={total === 0}
        emptyMessage="No apps found"
        error={error ? (error as Error).message : null}
        loading={isLoading}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Release</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((a) => (
              <TableRow key={a.name}>
                <TableCell>
                  <Link
                    className="underline hover:no-underline"
                    params={{ app: a.name }}
                    to="/apps/$app/processes"
                  >
                    {a.name}
                  </Link>
                </TableCell>
                <TableCell>{a.status}</TableCell>
                <TableCell>{a.release || '—'}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>

        {total > 0 && (
          <div className="mt-4 flex items-center justify-between">
            <div className="text-muted-foreground text-sm">
              Showing {start + 1}–{end} of {total} apps
            </div>
            <div className="flex gap-2">
              <button
                className="rounded-md border px-3 py-1 text-sm disabled:opacity-50"
                disabled={page === 1}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                type="button"
              >
                Previous
              </button>
              <button
                className="rounded-md border px-3 py-1 text-sm disabled:opacity-50"
                disabled={page === totalPages}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                type="button"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </TablePane>
    </PageLayout>
  )
}
