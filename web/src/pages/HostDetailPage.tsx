import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { fetchHost, formatBytes, formatPercent, HostDetail, ProcessInfo } from '../api'

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
  const [error, setError] = useState('')

  useEffect(() => {
    if (!id) return
    let alive = true
    const load = async () => {
      try {
        const d = await fetchHost(id)
        if (alive) setData(d)
      } catch (e) {
        if (alive) setError((e as Error).message)
      }
    }
    load()
    const t = setInterval(load, 10000)
    return () => { alive = false; clearInterval(t) }
  }, [id])

  if (error) return <p className="error">{error}</p>
  if (!data) return <p className="muted">加载中…</p>

  const snap = data.snapshot
  const procs = snap?.processes
  return (
    <div className="card">
      <p><Link to="/">← 返回总览</Link></p>
      <h1>{data.host.name}</h1>
      <p className="muted">{data.host.hostname} · <span className={`badge ${data.host.status}`}>{data.host.status}</span></p>

      {!snap ? (
        <p className="muted">尚无指标快照。</p>
      ) : (
        <>
          <p className="muted">采集时间：{new Date(snap.collected_at).toLocaleString()} · 采集耗时 {snap.collect_duration_ms}ms（磁盘 {snap.disk_collect_duration_ms}ms）</p>
          <div className="grid">
            <div className="stat"><div className="label">CPU</div><div className="value">{formatPercent(snap.cpu.usage_percent)}</div></div>
            <div className="stat"><div className="label">负载 1/5/15</div><div className="value" style={{ fontSize: '1rem' }}>{snap.cpu.load1.toFixed(2)} / {snap.cpu.load5.toFixed(2)} / {snap.cpu.load15.toFixed(2)}</div></div>
            <div className="stat"><div className="label">内存</div><div className="value">{formatPercent(snap.memory.used_percent)}</div></div>
            <div className="stat"><div className="label">核心数</div><div className="value">{snap.cpu.cores}</div></div>
            <div className="stat"><div className="label">进程数</div><div className="value">{procs?.summary.total ?? '—'}</div></div>
            <div className="stat"><div className="label">僵尸</div><div className="value">{procs?.summary.zombie ?? 0}</div></div>
          </div>

          <h2>磁盘挂载（statfs）</h2>
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
