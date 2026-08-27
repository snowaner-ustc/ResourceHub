export type HostSummary = {
  id: string
  name: string
  hostname: string
  agent_version: string
  status: 'online' | 'offline'
  last_seen_at?: string
  cpu_usage_percent: number
  memory_used_percent: number
  max_disk_used_percent: number
  max_disk_mountpoint?: string
}

export type HostDetail = {
  host: HostSummary
  snapshot: MetricSnapshot | null
}

export type MetricSnapshot = {
  host_id: string
  collected_at: string
  cpu: {
    usage_percent: number
    load1: number
    load5: number
    load15: number
    cores: number
  }
  memory: {
    total_bytes: number
    used_bytes: number
    available_bytes: number
    used_percent: number
  }
  disks: DiskStats[]
  collect_duration_ms: number
  disk_collect_duration_ms: number
}

export type DiskStats = {
  mountpoint: string
  device: string
  fstype: string
  total_bytes: number
  used_bytes: number
  avail_bytes: number
  used_percent: number
  collect_method: string
  collect_duration_ms: number
}

export type Alert = {
  id: string
  host_id: string
  host_name: string
  rule_type: string
  severity: 'warning' | 'critical'
  status: 'firing' | 'resolved'
  message: string
  fired_at: string
  resolved_at?: string
}

const API = '/api/v1'

export async function fetchHosts(): Promise<HostSummary[]> {
  const res = await fetch(`${API}/hosts`)
  if (!res.ok) throw new Error('failed to load hosts')
  return res.json()
}

export async function fetchHost(id: string): Promise<HostDetail> {
  const res = await fetch(`${API}/hosts/${id}`)
  if (!res.ok) throw new Error('failed to load host')
  return res.json()
}

export async function fetchAlerts(status = 'firing'): Promise<Alert[]> {
  const q = status ? `?status=${encodeURIComponent(status)}` : ''
  const res = await fetch(`${API}/alerts${q}`)
  if (!res.ok) throw new Error('failed to load alerts')
  return res.json()
}

export function formatBytes(n: number): string {
  if (n >= 1 << 40) return (n / (1 << 40)).toFixed(1) + ' TiB'
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + ' GiB'
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MiB'
  return n + ' B'
}

export function formatPercent(n: number): string {
  return n.toFixed(1) + '%'
}
