import { api } from './api'

type RawProcess = {
  id: string
  service?: string
  name?: string
  status: string
  release: string
  started?: string
}

type RawService = {
  name: string
  count?: number
  cpu?: number
  memory?: number
}

type AppProcess = {
  id: string
  service: string
  status: string
  release: string
  started?: string
}

type AppService = {
  name: string
  count: number
  cpu: number
  memory: number
}

export async function fetchAppProcesses(app: string): Promise<AppProcess[]> {
  const processes = await api.get<RawProcess[]>(`/api/v1/convox/apps/${app}/processes`)
  return processes.map((process) => ({
    id: process.id,
    service: process.service ?? process.name ?? '',
    status: process.status,
    release: process.release,
    started: process.started,
  }))
}

export async function fetchAppServices(app: string): Promise<AppService[]> {
  const services = await api.get<RawService[]>(`/api/v1/convox/apps/${app}/services`)
  return services
    .map((service) => ({
      name: service.name,
      count: service.count ?? 0,
      cpu: service.cpu ?? 0,
      memory: service.memory ?? 0,
    }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

export function countProcessesByService(processes: AppProcess[]): Record<string, number> {
  return processes.reduce<Record<string, number>>((counts, process) => {
    counts[process.service] = (counts[process.service] ?? 0) + 1
    return counts
  }, {})
}
