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
type Proc = {
  id: string
  service: string
  name?: string
  status: string
  release: string
  app: string
  started?: string
}

export function AllProcessesPage() {
  const {
    data = [],
    isLoading,
    error,
  } = useQuery({
    queryKey: ['all-procs'],
    queryFn: async () => {
      const apps = await api.get<App[]>('/apps')
      const lists = await Promise.all(
        apps.map(async (a) => {
          const ps = await api.get<
            {
              id: string
              service?: string
              name?: string
              status: string
              release: string
              started?: string
            }[]
          >(`/apps/${a.name}/processes`)
          return ps.map((p) => ({
            ...p,
            app: a.name,
            service: p.service ?? p.name ?? '',
          })) as Proc[]
        })
      )
      // Also include system processes
      let systemProcs: Proc[] = []
      try {
        const sys =
          await api.get<
            {
              id: string
              service?: string
              name?: string
              status: string
              release: string
              app?: string
              started?: string
            }[]
          >('/system/processes')
        systemProcs = (sys || []).map((p) => ({
          ...p,
          app: p.app || 'system',
          service: p.service ?? p.name ?? '',
        })) as Proc[]
      } catch (_e) {
        // ignore if rack doesn't provide system processes
      }
      return lists.flat().concat(systemProcs)
    },
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    staleTime: 0,
  })
  // Simple client-side pagination like Audit page
  const perPage = DEFAULT_PER_PAGE
  const total = data.length
  const totalPages = Math.max(1, Math.ceil(total / perPage))
  const [page, setPage] = useState(1)
  const start = (page - 1) * perPage
  const end = Math.min(start + perPage, total)
  const rows = data.slice(start, end)

  return (
    <PageLayout description="Aggregated across all apps" title="Processes">
      <TablePane
        empty={total === 0}
        emptyMessage="No processes found"
        error={error ? (error as Error).message : null}
        loading={isLoading}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>App</TableHead>
              <TableHead>ID</TableHead>
              <TableHead>Service</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Release</TableHead>
              <TableHead>Started</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((p) => (
              <TableRow key={`${p.app}/${p.id}`}>
                <TableCell>
                  <Link
                    className="underline hover:no-underline"
                    params={{ app: p.app }}
                    to="/apps/$app/processes"
                  >
                    {p.app}
                  </Link>
                </TableCell>
                <TableCell className="font-mono text-xs">{p.id}</TableCell>
                <TableCell>{p.service ?? p.name ?? '—'}</TableCell>
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
    </PageLayout>
  )
}
