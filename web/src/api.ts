export type HostSummary = {
  id: string
  name: string
  hostname: string
  group?: string
  agent_version: string
  status: 'online' | 'offline'
  last_seen_at?: string
  cpu_usage_percent: number
  memory_used_percent: number
  max_disk_used_percent: number
  max_disk_mountpoint?: string
  process_total: number
  zombie_count: number
  gpu_count: number
  max_gpu_util_percent: number
  max_gpu_mem_percent: number
}

export type HostDetail = {
  host: HostSummary
  snapshot: MetricSnapshot | null
}

export type ProcessInfo = {
  pid: number
  ppid?: number
  ppid_comm?: string
  comm: string
  user?: string
  state?: string
  cpu_percent: number
  rss_bytes: number
}

export type ProcessStats = {
  collected_at: string
  summary: {
    total: number
    running: number
    sleeping: number
    uninterruptible: number
    zombie: number
    stopped: number
    unknown: number
    collect_duration_ms: number
    partial: boolean
    scanned_pids: number
  }
  top_cpu: ProcessInfo[]
  top_rss: ProcessInfo[]
  zombies: ProcessInfo[]
}

export type GPUDevice = {
  index: number
  name: string
  uuid?: string
  util_percent: number
  memory_used_bytes: number
  memory_total_bytes: number
  memory_used_percent: number
  temperature_c: number
  power_watts: number
}

export type GPUStats = {
  available: boolean
  status: string
  method?: string
  collect_duration_ms: number
  devices: GPUDevice[]
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
  gpus?: GPUStats
  processes?: ProcessStats
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

export type MetricPoint = {
  metric: string
  ts: string
  value: number
}

export type MetricsSeries = {
  from: string
  to: string
  points: MetricPoint[]
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
export const PROCESS_COUNT_WARN = 2000

export async function fetchHosts(group = ''): Promise<HostSummary[]> {
  const q = group ? `?group=${encodeURIComponent(group)}` : ''
  const res = await fetch(`${API}/hosts${q}`)
  if (!res.ok) throw new Error('failed to load hosts')
  return res.json()
}

export async function fetchHost(id: string): Promise<HostDetail> {
  const res = await fetch(`${API}/hosts/${id}`)
  if (!res.ok) throw new Error('failed to load host')
  return res.json()
}

export async function patchHostGroup(id: string, group: string): Promise<HostSummary> {
  const res = await fetch(`${API}/hosts/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ group }),
  })
  if (!res.ok) throw new Error('failed to update group')
  return res.json()
}

export async function fetchHostMetrics(id: string, hours = 1, names?: string[]): Promise<MetricsSeries> {
  const params = new URLSearchParams({ hours: String(hours) })
  if (names?.length) params.set('names', names.join(','))
  const res = await fetch(`${API}/hosts/${id}/metrics?${params}`)
  if (!res.ok) throw new Error('failed to load metrics')
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
