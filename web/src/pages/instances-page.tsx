import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'

type Instance = {
  id: string
  status: string
  private_ip?: string
  public_ip?: string
  instance_type?: string
}

export function InstancesPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['instances'],
    queryFn: async () => api.get<Instance[]>('/.gateway/api/instances'),
  })
  return (
    <div className="mx-auto max-w-5xl p-6">
      <h2 className="mb-4 font-semibold text-2xl">Instances</h2>
      {isLoading && <div>Loading instances…</div>}
      {error && (
        <div className="text-destructive">Failed to load instances: {(error as Error).message}</div>
      )}
      {data && (
        <div className="overflow-x-auto">
          <table className="w-full border text-left text-sm">
            <thead>
              <tr className="bg-muted">
                <th className="border px-3 py-2">ID</th>
                <th className="border px-3 py-2">Status</th>
                <th className="border px-3 py-2">Private IP</th>
                <th className="border px-3 py-2">Public IP</th>
                <th className="border px-3 py-2">Type</th>
              </tr>
            </thead>
            <tbody>
              {data.map((i) => (
                <tr key={i.id}>
                  <td className="border px-3 py-2 font-mono text-xs">{i.id}</td>
                  <td className="border px-3 py-2">{i.status}</td>
                  <td className="border px-3 py-2">{i.private_ip || '—'}</td>
                  <td className="border px-3 py-2">{i.public_ip || '—'}</td>
                  <td className="border px-3 py-2">{i.instance_type || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
