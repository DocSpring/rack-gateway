import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'

type App = { name: string }
type Build = { id: string; description?: string; status: string; release: string; app?: string }

export function AllBuildsPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['all-builds'],
    queryFn: async () => {
      const apps = await api.get<App[]>('/.gateway/api/apps')
      const lists = await Promise.all(
        apps.map(async (a) => {
          const bs = await api.get<Build[]>(`/.gateway/api/apps/${a.name}/builds`)
          return bs.map((b) => ({ ...b, app: a.name }))
        })
      )
      return lists.flat()
    },
  })
  return (
    <div className="mx-auto max-w-5xl p-6">
      <h2 className="mb-4 font-semibold text-2xl">All Builds</h2>
      {isLoading && <div>Loading builds…</div>}
      {error && (
        <div className="text-destructive">Failed to load builds: {(error as Error).message}</div>
      )}
      {data && (
        <div className="overflow-x-auto">
          <table className="w-full border text-left text-sm">
            <thead>
              <tr className="bg-muted">
                <th className="border px-3 py-2">App</th>
                <th className="border px-3 py-2">ID</th>
                <th className="border px-3 py-2">Description</th>
                <th className="border px-3 py-2">Status</th>
                <th className="border px-3 py-2">Release</th>
              </tr>
            </thead>
            <tbody>
              {data.map((b) => (
                <tr key={`${b.app}/${b.id}`}>
                  <td className="border px-3 py-2">{b.app}</td>
                  <td className="border px-3 py-2 font-mono text-xs">{b.id}</td>
                  <td className="border px-3 py-2">{b.description || '—'}</td>
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
