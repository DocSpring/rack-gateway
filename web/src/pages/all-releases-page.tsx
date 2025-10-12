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

type App = { name: string }
type Release = {
  id: string
  description?: string
  version?: number
  app: string
  created?: string
  created_by?: string
}

export function AllReleasesPage() {
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['all-releases'],
    queryFn: async () => {
      const apps = await api.get<App[]>('/api/v1/convox/apps')
      const lists = await Promise.all(
        apps.map(async (a) => {
          const rs = await api.get<Release[]>(`/api/v1/convox/apps/${a.name}/releases`)
          return rs.map((r) => ({ ...r, app: a.name }))
        })
      )
      const items = lists.flat().sort((a, b) => {
        const at = a.created ? new Date(a.created).getTime() : 0
        const bt = b.created ? new Date(b.created).getTime() : 0
        return bt - at
      })
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
    <PageLayout description="Releases across all apps" title="Releases">
      <TablePane
        empty={total === 0}
        emptyMessage="No releases found"
        error={error ? (error as Error).message : null}
        loading={isLoading}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>App</TableHead>
              <TableHead>ID</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Created By</TableHead>
              <TableHead>Created</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={`${r.app}/${r.id}`}>
                <TableCell>
                  <Link
                    className="underline hover:no-underline"
                    params={{ app: r.app! as string }}
                    to="/apps/$app/releases"
                  >
                    {r.app}
                  </Link>
                </TableCell>
                <TableCell className="font-mono text-xs">{r.id}</TableCell>
                <TableCell className="max-w-[420px] whitespace-normal break-words">
                  {r.description || '—'}
                </TableCell>
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
    </PageLayout>
  )
}
