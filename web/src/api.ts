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

export type DiskEntry = {
  path: string
  bytes: number
  owner?: string
  entry_type: string
}

export type DiskScanReport = {
  root: string
  as_of: string
  duration_ms: number
  partial: boolean
  partial_reason?: string
  files_visited: number
  scan_errors: number
  root_bytes: number
  entries: DiskEntry[]
  others_bytes: number
  by_owner: { owner: string; bytes: number }[]
  large_files: DiskEntry[]
}

export type SlowJob = {
  id: string
  host_id: string
  kind: string
  status: 'queued' | 'running' | 'done' | 'failed'
  params?: { root?: string; path_pattern?: string }
  report?: DiskScanReport
  error?: string
  cached?: boolean
  created_at: string
  finished_at?: string
}

export type QuotaPolicy = {
  id: string
  name: string
  enabled: boolean
  host_group?: string
  path_pattern: string
  match_mode: string
  limit_bytes: number
  warn_percent: number
  critical_percent: number
  scan_interval_seconds: number
  exclude?: string[]
}

export type QuotaUsage = {
  host_id: string
  host_name: string
  policy_id: string
  policy_name: string
  path: string
  owner?: string
  used_bytes: number
  limit_bytes: number
  used_percent: number
  as_of: string
  partial_scan?: boolean
}

async function readError(res: Response, fallback: string): Promise<string> {
  try {
    const body = await res.json() as { error?: string }
    if (body.error) return body.error
  } catch {
    /* ignore */
  }
  return fallback
}

export async function startDiskScan(hostId: string, root: string, force = false): Promise<SlowJob> {
  const res = await fetch(`${API}/hosts/${hostId}/disk-scans`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ root, force }),
  })
  if (!res.ok) throw new Error(await readError(res, 'failed to start disk scan'))
  return res.json()
}

export async function fetchDiskScan(hostId: string, jobId: string): Promise<SlowJob> {
  const res = await fetch(`${API}/hosts/${hostId}/disk-scans/${jobId}`)
  if (!res.ok) throw new Error(await readError(res, 'failed to load disk scan'))
  return res.json()
}

export async function fetchQuotaPolicies(): Promise<QuotaPolicy[]> {
  const res = await fetch(`${API}/quota-policies`)
  if (!res.ok) throw new Error('failed to load quota policies')
  return res.json()
}

export async function createQuotaPolicy(p: Partial<QuotaPolicy>): Promise<QuotaPolicy> {
  const res = await fetch(`${API}/quota-policies`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(p),
  })
  if (!res.ok) throw new Error(await readError(res, 'failed to create policy'))
  return res.json()
}

export async function patchQuotaPolicy(id: string, p: Partial<QuotaPolicy>): Promise<QuotaPolicy> {
  const res = await fetch(`${API}/quota-policies/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(p),
  })
  if (!res.ok) throw new Error(await readError(res, 'failed to update policy'))
  return res.json()
}

export async function deleteQuotaPolicy(id: string): Promise<void> {
  const res = await fetch(`${API}/quota-policies/${id}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(await readError(res, 'failed to delete policy'))
}

export async function triggerQuotaScan(id: string, hostId?: string): Promise<void> {
  const res = await fetch(`${API}/quota-policies/${id}/scan`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ host_id: hostId || '', force: true }),
  })
  if (!res.ok) throw new Error(await readError(res, 'failed to start quota scan'))
}

export async function fetchQuotaUsages(hostId = '', policyId = ''): Promise<QuotaUsage[]> {
  const params = new URLSearchParams()
  if (hostId) params.set('host_id', hostId)
  if (policyId) params.set('policy_id', policyId)
  const q = params.toString()
  const res = await fetch(`${API}/quota-usages${q ? `?${q}` : ''}`)
  if (!res.ok) throw new Error('failed to load quota usages')
  return res.json()
}

const KiB = 1024
const MiB = 1024 ** 2
const GiB = 1024 ** 3
const TiB = 1024 ** 4

export function formatBytes(n: number): string {
  if (n >= TiB) return (n / TiB).toFixed(1) + ' TiB'
  if (n >= GiB) return (n / GiB).toFixed(1) + ' GiB'
  if (n >= MiB) return (n / MiB).toFixed(1) + ' MiB'
  if (n >= KiB) return (n / KiB).toFixed(1) + ' KiB'
  return n + ' B'
}

export function formatPercent(n: number): string {
  return n.toFixed(1) + '%'
}
