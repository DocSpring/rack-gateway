import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { PageLayout } from '../components/page-layout'
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

type App = { name: string }
type Build = {
  id: string
  description?: string
  status: string
  release: string
  started?: string
  ended?: string
  app: string
  created_by?: string
}

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
      const items = lists.flat()
      // Fetch created-by mapping
      try {
        const ids = Array.from(new Set(items.map((b) => b.id))).join(',')
        if (ids) {
          const map = await api.get<Record<string, { email: string; name: string }>>(
            `/.gateway/api/created-by?type=build&ids=${encodeURIComponent(ids)}`
          )
          for (const b of items) {
            const m = map[b.id]
            if (m) {
              b.created_by = m.email || m.name
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
              <TableHead>Created By</TableHead>
              <TableHead>Started</TableHead>
              <TableHead>Elapsed</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((b) => (
              <TableRow key={`${b.app}/${b.id}`}>
                <TableCell>
                  <Link
                    className="underline hover:no-underline"
                    params={{ app: b.app! as string }}
                    to="/apps/$app/builds"
                  >
                    {b.app}
                  </Link>
                </TableCell>
                <TableCell className="font-mono text-xs">{b.id}</TableCell>
                <TableCell className="max-w-[420px] whitespace-normal break-words">
                  {b.description || '—'}
                </TableCell>
                <TableCell>{b.status}</TableCell>
                <TableCell>{b.release}</TableCell>
                <TableCell>{b.created_by || '—'}</TableCell>
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
    </PageLayout>
  )
}
