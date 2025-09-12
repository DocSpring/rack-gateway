import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { api } from '../lib/api'

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
  const { data, isLoading, error } = useQuery({
    queryKey: ['app-builds', app],
    queryFn: async () => api.get<Build[]>(`/.gateway/api/apps/${app}/builds`),
  })
  return (
    <div>
      {isLoading && <div>Loading builds…</div>}
      {error && (
        <div className="text-destructive">Failed to load builds: {(error as Error).message}</div>
      )}
      {data && (
        <div className="overflow-x-auto">
          <table className="w-full border text-left text-sm">
            <thead>
              <tr className="bg-muted">
                <th className="border px-3 py-2">ID</th>
                <th className="border px-3 py-2">Description</th>
                <th className="border px-3 py-2">Status</th>
                <th className="border px-3 py-2">Release</th>
              </tr>
            </thead>
            <tbody>
              {data.map((b) => (
                <tr key={b.id}>
                  <td className="border px-3 py-2 font-mono text-xs">{b.id}</td>
                  <td className="border px-3 py-2">{b.description}</td>
                  <td className="border px-3 py-2">{b.status}</td>
                  <td className="border px-3 py-2">{b.release}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
