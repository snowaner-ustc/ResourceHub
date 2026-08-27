import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { fetchHost, formatBytes, formatPercent, HostDetail } from '../api'

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
        </>
      )}
    </div>
  )
}
