import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { api } from '../lib/api'

type App = {
  name: string
  status: string
  generation?: string
  release?: string
}

export function AppsListPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['apps-list'],
    queryFn: async () => api.get<App[]>('/.gateway/api/apps'),
  })

  return (
    <div className="mx-auto max-w-5xl p-6">
      <h2 className="mb-4 font-semibold text-2xl">Apps</h2>
      {isLoading && <div>Loading apps…</div>}
      {error && (
        <div className="text-destructive">Failed to load apps: {(error as Error).message}</div>
      )}
      {data && data.length === 0 && <div>No apps found</div>}
      {data && data.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full border text-left text-sm">
            <thead>
              <tr className="bg-muted">
                <th className="border px-3 py-2">Name</th>
                <th className="border px-3 py-2">Status</th>
                <th className="border px-3 py-2">Release</th>
              </tr>
            </thead>
            <tbody>
              {data.map((a) => (
                <tr key={a.name}>
                  <td className="border px-3 py-2">
                    <Link
                      className="underline hover:no-underline"
                      params={{ app: a.name }}
                      to="/apps/$app/processes"
                    >
                      {a.name}
                    </Link>
                  </td>
                  <td className="border px-3 py-2">{a.status}</td>
                  <td className="border px-3 py-2">{a.release || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
