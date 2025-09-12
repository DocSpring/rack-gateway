import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { api } from '../lib/api'

type Process = {
  id: string
  service: string
  status: string
  release: string
  command?: string
}

export function AppProcessesPage() {
  const { app } = useParams({ from: '/apps/$app/processes' }) as { app: string }
  const { data, isLoading, error } = useQuery({
    queryKey: ['app-processes', app],
    queryFn: async () => api.get<Process[]>(`/.gateway/api/apps/${app}/processes`),
  })
  return (
    <div>
      {isLoading && <div>Loading processes…</div>}
      {error && (
        <div className="text-destructive">Failed to load processes: {(error as Error).message}</div>
      )}
      {data && (
        <div className="overflow-x-auto">
          <table className="w-full border text-left text-sm">
            <thead>
              <tr className="bg-muted">
                <th className="border px-3 py-2">ID</th>
                <th className="border px-3 py-2">Service</th>
                <th className="border px-3 py-2">Status</th>
                <th className="border px-3 py-2">Release</th>
              </tr>
            </thead>
            <tbody>
              {data.map((p) => (
                <tr key={p.id}>
                  <td className="border px-3 py-2 font-mono text-xs">{p.id}</td>
                  <td className="border px-3 py-2">{p.service}</td>
                  <td className="border px-3 py-2">{p.status}</td>
                  <td className="border px-3 py-2">{p.release}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
