import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'

type App = { name: string }
type Release = { id: string; description?: string; version?: number; app?: string }

export function AllReleasesPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['all-releases'],
    queryFn: async () => {
      const apps = await api.get<App[]>('/.gateway/api/apps')
      const lists = await Promise.all(
        apps.map(async (a) => {
          const rs = await api.get<Release[]>(`/.gateway/api/apps/${a.name}/releases`)
          return rs.map((r) => ({ ...r, app: a.name }))
        })
      )
      return lists.flat()
    },
  })
  return (
    <div className="mx-auto max-w-5xl p-6">
      <h2 className="mb-4 font-semibold text-2xl">All Releases</h2>
      {isLoading && <div>Loading releases…</div>}
      {error && (
        <div className="text-destructive">Failed to load releases: {(error as Error).message}</div>
      )}
      {data && (
        <div className="overflow-x-auto">
          <table className="w-full border text-left text-sm">
            <thead>
              <tr className="bg-muted">
                <th className="border px-3 py-2">App</th>
                <th className="border px-3 py-2">ID</th>
                <th className="border px-3 py-2">Description</th>
                <th className="border px-3 py-2">Version</th>
              </tr>
            </thead>
            <tbody>
              {data.map((r) => (
                <tr key={`${r.app}/${r.id}`}>
                  <td className="border px-3 py-2">{r.app}</td>
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
