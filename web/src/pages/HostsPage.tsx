import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { fetchHosts, formatPercent, HostSummary } from '../api'

function usageClass(pct: number): string {
  if (pct >= 90) return 'crit'
  if (pct >= 80) return 'warn'
  return ''
}

export default function HostsPage() {
  const [hosts, setHosts] = useState<HostSummary[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const data = await fetchHosts()
        if (alive) setHosts(data)
      } catch (e) {
        if (alive) setError((e as Error).message)
      } finally {
        if (alive) setLoading(false)
      }
    }
    load()
    const t = setInterval(load, 10000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  if (loading) return <p className="muted">加载中…</p>
  if (error) return <p className="error">{error}</p>

  return (
    <div className="card">
      <h1>机器总览</h1>
      <p className="muted">只读已上报快照，不会触发远程扫盘。每 10 秒自动刷新。</p>
      {hosts.length === 0 ? (
        <p className="muted">暂无机器。请先启动 agent 并完成注册。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th>状态</th>
              <th>CPU</th>
              <th>内存</th>
              <th>最满磁盘</th>
              <th>Agent</th>
            </tr>
          </thead>
          <tbody>
            {hosts.map((h) => (
              <tr key={h.id}>
                <td><Link to={`/hosts/${h.id}`}>{h.name}</Link><div className="muted">{h.hostname}</div></td>
                <td><span className={`badge ${h.status}`}>{h.status}</span></td>
                <td>{formatPercent(h.cpu_usage_percent)}</td>
                <td>{formatPercent(h.memory_used_percent)}</td>
                <td>
                  <div>{h.max_disk_mountpoint || '—'}</div>
                  <div className={`bar ${usageClass(h.max_disk_used_percent)}`}>
                    <span style={{ width: `${Math.min(h.max_disk_used_percent, 100)}%` }} />
                  </div>
                  <div className="muted">{formatPercent(h.max_disk_used_percent)}</div>
                </td>
                <td className="muted">{h.agent_version || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
