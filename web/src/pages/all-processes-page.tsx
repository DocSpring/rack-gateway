import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'

type App = { name: string }
type Proc = { id: string; service: string; status: string; release: string; app?: string }

export function AllProcessesPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['all-procs'],
    queryFn: async () => {
      const apps = await api.get<App[]>('/.gateway/api/apps')
      const lists = await Promise.all(
        apps.map(async (a) => {
          const ps = await api.get<Proc[]>(`/.gateway/api/apps/${a.name}/processes`)
          return ps.map((p) => ({ ...p, app: a.name }))
        })
      )
      return lists.flat()
    },
  })
  return (
    <div className="mx-auto max-w-5xl p-6">
      <h2 className="mb-4 font-semibold text-2xl">All Processes</h2>
      {isLoading && <div>Loading processes…</div>}
      {error && (
        <div className="text-destructive">Failed to load processes: {(error as Error).message}</div>
      )}
      {data && (
        <div className="overflow-x-auto">
          <table className="w-full border text-left text-sm">
            <thead>
              <tr className="bg-muted">
                <th className="border px-3 py-2">App</th>
                <th className="border px-3 py-2">ID</th>
                <th className="border px-3 py-2">Service</th>
                <th className="border px-3 py-2">Status</th>
                <th className="border px-3 py-2">Release</th>
              </tr>
            </thead>
            <tbody>
              {data.map((p) => (
                <tr key={`${p.app}/${p.id}`}>
                  <td className="border px-3 py-2">{p.app}</td>
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
