import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'

type RackInfo = {
  name?: string
  domain?: string
  provider?: string
  region?: string
  status?: string
  type?: string
  version?: string
  count?: number
  'rack-domain'?: string
  outputs?: Record<string, string>
  parameters?: Record<string, string>
}

export function RackPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['rack-info'],
    queryFn: async () => {
      const res = await api.get<RackInfo>('/.gateway/api/rack')
      return res
    },
  })

  return (
    <div className="mx-auto max-w-5xl p-6">
      <h2 className="mb-4 font-semibold text-2xl">Rack Information</h2>
      {isLoading && <div>Loading rack info…</div>}
      {error && (
        <div className="text-destructive">Failed to load rack info: {(error as Error).message}</div>
      )}
      {data && (
        <div className="space-y-8">
          <section>
            <h3 className="mb-2 font-medium text-lg">Overview</h3>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              <InfoRow label="Name" value={data.name} />
              <InfoRow label="Domain" value={data.domain} />
              <InfoRow label="Rack Domain" value={data['rack-domain']} />
              <InfoRow label="Provider" value={data.provider} />
              <InfoRow label="Region" value={data.region} />
              <InfoRow label="Type" value={data.type} />
              <InfoRow label="Version" value={data.version} />
              <InfoRow label="Count" value={String(data.count ?? '')} />
              <InfoRow label="Status" value={data.status} />
            </div>
          </section>

          {data.parameters && Object.keys(data.parameters).length > 0 && (
            <section>
              <h3 className="mb-2 font-medium text-lg">Parameters</h3>
              <KVTable obj={data.parameters} />
            </section>
          )}

          {data.outputs && Object.keys(data.outputs).length > 0 && (
            <section>
              <h3 className="mb-2 font-medium text-lg">Outputs</h3>
              <KVTable obj={data.outputs} />
            </section>
          )}
        </div>
      )}
    </div>
  )
}

function InfoRow({ label, value }: { label: string; value?: string }) {
  return (
    <div className="flex items-center justify-between rounded-md border px-3 py-2 text-sm">
      <div className="text-muted-foreground">{label}</div>
      <div className="ml-4 truncate font-medium">{value || '—'}</div>
    </div>
  )
}

function KVTable({ obj }: { obj: Record<string, string> }) {
  const entries = Object.entries(obj).sort((a, b) => a[0].localeCompare(b[0]))
  return (
    <div className="overflow-x-auto">
      <table className="w-full border text-left text-sm">
        <thead>
          <tr className="bg-muted">
            <th className="border px-3 py-2">Key</th>
            <th className="border px-3 py-2">Value</th>
          </tr>
        </thead>
        <tbody>
          {entries.map(([k, v]) => (
            <tr key={k}>
              <td className="border px-3 py-1 font-mono text-xs">{k}</td>
              <td className="truncate border px-3 py-1 font-mono text-xs">{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
