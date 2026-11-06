import { FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import Sparkline from '../Sparkline'
import {
  fetchHost,
  fetchHostMetrics,
  fetchQuotaUsages,
  formatBytes,
  formatPercent,
  HostDetail,
  MetricPoint,
  patchHostGroup,
  ProcessInfo,
  QuotaUsage,
} from '../api'
import DiskScanPanel from './DiskScanPanel'

function ProcessTable({ title, rows, showParent }: { title: string; rows: ProcessInfo[]; showParent?: boolean }) {
  if (!rows.length) {
    return (
      <>
        <h3>{title}</h3>
        <p className="muted">暂无数据。</p>
      </>
    )
  }
  return (
    <>
      <h3>{title}</h3>
      <table>
        <thead>
          <tr>
            <th>PID</th>
            {showParent && <th>PPID</th>}
            <th>命令</th>
            <th>用户</th>
            {showParent && <th>父进程</th>}
            <th>CPU</th>
            <th>RSS</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((p) => (
            <tr key={`${title}-${p.pid}`}>
              <td>{p.pid}</td>
              {showParent && <td className="muted">{p.ppid ?? '—'}</td>}
              <td>{p.comm}</td>
              <td className="muted">{p.user || '—'}</td>
              {showParent && <td className="muted">{p.ppid_comm || '—'}</td>}
              <td>{formatPercent(p.cpu_percent || 0)}</td>
              <td>{formatBytes(p.rss_bytes || 0)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}

export default function HostDetailPage() {
  const { id } = useParams()
  const [data, setData] = useState<HostDetail | null>(null)
  const [points, setPoints] = useState<MetricPoint[]>([])
  const [hours, setHours] = useState(1)
  const [groupInput, setGroupInput] = useState('')
  const [error, setError] = useState('')
  const [usages, setUsages] = useState<QuotaUsage[]>([])
  const groupDirty = useRef(false)

  useEffect(() => {
    groupDirty.current = false
    setGroupInput('')
    setData(null)
    setPoints([])
    setUsages([])
    setError('')
  }, [id])

  useEffect(() => {
    if (!id) return
    let alive = true
    const load = async () => {
      try {
        const [d, m, u] = await Promise.all([
          fetchHost(id),
          fetchHostMetrics(id, hours, [
            'cpu.usage_percent',
            'memory.used_percent',
            'disk.max_used_percent',
            'gpu.max_util_percent',
            'gpu.max_mem_percent',
          ]),
          fetchQuotaUsages(id).catch(() => [] as QuotaUsage[]),
        ])
        if (!alive) return
        setData(d)
        if (!groupDirty.current) setGroupInput(d.host.group || '')
        setPoints(m.points || [])
        setUsages(u)
      } catch (e) {
        if (alive) setError((e as Error).message)
      }
    }
    load()
    const t = setInterval(load, 10000)
    return () => { alive = false; clearInterval(t) }
  }, [id, hours])

  const series = useMemo(() => {
    const by: Record<string, MetricPoint[]> = {}
    for (const p of points) {
      (by[p.metric] ||= []).push(p)
    }
    return by
  }, [points])

  if (error) return <p className="error">{error}</p>
  if (!id || !data) return <p className="muted">加载中…</p>

  const snap = data.snapshot
  const procs = snap?.processes
  const gpus = snap?.gpus

  const saveGroup = async (e: FormEvent) => {
    e.preventDefault()
    if (!id) return
    try {
      const host = await patchHostGroup(id, groupInput.trim())
      groupDirty.current = false
      setGroupInput(host.group || '')
      setData({ ...data, host: { ...data.host, ...host } })
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <div className="card">
      <p><Link to="/">← 返回总览</Link></p>
      <h1>{data.host.name}</h1>
      <p className="muted">
        {data.host.hostname} · <span className={`badge ${data.host.status}`}>{data.host.status}</span>
        {data.host.group ? ` · 分组 ${data.host.group}` : ''}
      </p>
      <form onSubmit={saveGroup} className="muted" style={{ marginBottom: 12 }}>
        分组：
        <input
          value={groupInput}
          onChange={(e) => {
            groupDirty.current = true
            setGroupInput(e.target.value)
          }}
          placeholder="例如 gpu / training"
        />
        {' '}<button type="submit">保存</button>
      </form>

      {!snap ? (
        <p className="muted">尚无指标快照。</p>
      ) : (
        <>
          <p className="muted">采集时间：{new Date(snap.collected_at).toLocaleString()} · 采集耗时 {snap.collect_duration_ms}ms（磁盘 {snap.disk_collect_duration_ms}ms）</p>
          <div className="grid">
            <div className="stat"><div className="label">CPU</div><div className="value">{formatPercent(snap.cpu.usage_percent)}</div></div>
            <div className="stat"><div className="label">负载 1/5/15</div><div className="value" style={{ fontSize: '1rem' }}>{snap.cpu.load1.toFixed(2)} / {snap.cpu.load5.toFixed(2)} / {snap.cpu.load15.toFixed(2)}</div></div>
            <div className="stat"><div className="label">内存</div><div className="value">{formatPercent(snap.memory.used_percent)}</div></div>
            <div className="stat"><div className="label">GPU</div><div className="value">{gpus?.available ? `${gpus.devices.length} 卡` : '无'}</div></div>
            <div className="stat"><div className="label">进程数</div><div className="value">{procs?.summary.total ?? '—'}</div></div>
            <div className="stat"><div className="label">僵尸</div><div className="value">{procs?.summary.zombie ?? 0}</div></div>
          </div>

          <h2>历史曲线</h2>
          <p className="muted">
            窗口：
            {[1, 6, 24].map((h) => (
              <button key={h} type="button" onClick={() => setHours(h)} style={{ marginRight: 8, fontWeight: hours === h ? 700 : 400 }}>
                {h}h
              </button>
            ))}
            （只读已存样本，默认保留 7 天）
          </p>
          <div className="charts">
            <Sparkline title="CPU %" points={series['cpu.usage_percent'] || []} />
            <Sparkline title="内存 %" points={series['memory.used_percent'] || []} color="#0d9488" />
            <Sparkline title="最满磁盘 %" points={series['disk.max_used_percent'] || []} color="#d97706" />
            {(series['gpu.max_util_percent']?.length || series['gpu.max_mem_percent']?.length) ? (
              <>
                <Sparkline title="GPU 利用率 %" points={series['gpu.max_util_percent'] || []} color="#7c3aed" />
                <Sparkline title="GPU 显存 %" points={series['gpu.max_mem_percent'] || []} color="#db2777" />
              </>
            ) : null}
          </div>

          {gpus?.available ? (
            <>
              <h2>GPU（{gpus.method || 'nvidia'}）</h2>
              <table>
                <thead>
                  <tr>
                    <th>#</th>
                    <th>名称</th>
                    <th>利用率</th>
                    <th>显存</th>
                    <th>温度</th>
                    <th>功耗</th>
                  </tr>
                </thead>
                <tbody>
                  {gpus.devices.map((g) => (
                    <tr key={g.uuid || g.index}>
                      <td>{g.index}</td>
                      <td>{g.name}</td>
                      <td>{formatPercent(g.util_percent)}</td>
                      <td>{formatBytes(g.memory_used_bytes)} / {formatBytes(g.memory_total_bytes)} ({formatPercent(g.memory_used_percent)})</td>
                      <td>{g.temperature_c ? `${g.temperature_c.toFixed(0)}°C` : '—'}</td>
                      <td>{g.power_watts ? `${g.power_watts.toFixed(0)} W` : '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          ) : (
            <p className="muted">本机无 NVIDIA GPU，或 nvidia-smi 不可用。</p>
          )}

          <h2>磁盘挂载（statfs，快路径）</h2>
          <table>
            <thead>
              <tr>
                <th>挂载点</th>
                <th>设备</th>
                <th>类型</th>
                <th>已用</th>
                <th>总量</th>
                <th>使用率</th>
              </tr>
            </thead>
            <tbody>
              {snap.disks.map((d) => (
                <tr key={d.mountpoint}>
                  <td>{d.mountpoint}</td>
                  <td className="muted">{d.device}</td>
                  <td className="muted">{d.fstype}</td>
                  <td>{formatBytes(d.used_bytes)}</td>
                  <td>{formatBytes(d.total_bytes)}</td>
                  <td>{formatPercent(d.used_percent)}</td>
                </tr>
              ))}
            </tbody>
          </table>

          <DiskScanPanel key={id} hostId={id} mountpoints={snap.disks.map((d) => d.mountpoint)} />

          <h2>软配额用量</h2>
          <p className="muted"><Link to="/quota">管理策略</Link> · 超限只告警，不拦写入。用量带采集时刻，不是实时 du。</p>
          {usages.some((u) => u.partial_scan) ? (
            <p className="muted">最近一次扫描不完整（超时或目标过多）。未扫到的路径不会自动消失，已删除目录需等一次完整扫描才会从列表和告警中清除。</p>
          ) : null}
          {usages.length === 0 ? (
            <p className="muted">此机器暂无配额扫描结果。</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>策略</th>
                  <th>路径</th>
                  <th>已用</th>
                  <th>限额</th>
                  <th>利用率</th>
                  <th>as_of</th>
                </tr>
              </thead>
              <tbody>
                {usages.map((u) => (
                  <tr key={`${u.policy_id}-${u.path}`}>
                    <td>{u.policy_name}</td>
                    <td>{u.path}</td>
                    <td>{formatBytes(u.used_bytes)}</td>
                    <td>{formatBytes(u.limit_bytes)}</td>
                    <td className={u.used_percent >= 100 ? 'error' : u.used_percent >= 80 ? 'warn-text' : ''}>
                      {formatPercent(u.used_percent)}
                    </td>
                    <td className="muted">{u.as_of ? new Date(u.as_of).toLocaleString() : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          <h2>进程</h2>
          {!procs ? (
            <p className="muted">尚无进程快照（等待 agent 中路径采集，默认约 30s）。</p>
          ) : (
            <>
              <p className="muted">
                进程采集：{new Date(procs.collected_at).toLocaleString()} · {procs.summary.collect_duration_ms}ms
                {procs.summary.partial ? ' · 部分扫描（超时）' : ''}
              </p>
              <div className="grid">
                <div className="stat"><div className="label">Running</div><div className="value">{procs.summary.running}</div></div>
                <div className="stat"><div className="label">Sleeping</div><div className="value">{procs.summary.sleeping}</div></div>
                <div className="stat"><div className="label">D</div><div className="value">{procs.summary.uninterruptible}</div></div>
                <div className="stat"><div className="label">Stopped</div><div className="value">{procs.summary.stopped}</div></div>
              </div>
              {procs.summary.zombie > 0 && (
                <ProcessTable title={`僵尸进程（${procs.summary.zombie}）`} rows={procs.zombies} showParent />
              )}
              <ProcessTable title="Top CPU" rows={procs.top_cpu} />
              <ProcessTable title="Top RSS" rows={procs.top_rss} />
            </>
          )}
        </>
      )}
    </div>
  )
}
