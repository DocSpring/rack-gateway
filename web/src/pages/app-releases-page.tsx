import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { api } from '../lib/api'

type Release = {
  id: string
  description?: string
  version?: number
  created?: string
}
export function AppReleasesPage() {
  const { app } = useParams({ from: '/apps/$app/releases' }) as { app: string }
  const { data, isLoading, error } = useQuery({
    queryKey: ['app-releases', app],
    queryFn: async () => api.get<Release[]>(`/.gateway/api/apps/${app}/releases`),
  })
  return (
    <div>
      {isLoading && <div>Loading releases…</div>}
      {error && (
        <div className="text-destructive">Failed to load releases: {(error as Error).message}</div>
      )}
      {data && (
        <div className="overflow-x-auto">
          <table className="w-full border text-left text-sm">
            <thead>
              <tr className="bg-muted">
                <th className="border px-3 py-2">ID</th>
                <th className="border px-3 py-2">Description</th>
                <th className="border px-3 py-2">Version</th>
              </tr>
            </thead>
            <tbody>
              {data.map((r) => (
                <tr key={r.id}>
                  <td className="border px-3 py-2 font-mono text-xs">{r.id}</td>
                  <td className="border px-3 py-2">{r.description || '—'}</td>
                  <td className="border px-3 py-2">{r.version ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
