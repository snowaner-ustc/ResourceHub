import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { fetchHosts, formatPercent, HostSummary, PROCESS_COUNT_WARN } from '../api'

function usageClass(pct: number): string {
  if (pct >= 90) return 'crit'
  if (pct >= 80) return 'warn'
  return ''
}

export default function HostsPage() {
  const [hosts, setHosts] = useState<HostSummary[]>([])
  const [group, setGroup] = useState('')
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

  const groups = useMemo(() => {
    const s = new Set<string>()
    hosts.forEach((h) => { if (h.group) s.add(h.group) })
    return Array.from(s).sort()
  }, [hosts])

  const visible = group ? hosts.filter((h) => h.group === group) : hosts

  if (loading) return <p className="muted">加载中…</p>
  if (error) return <p className="error">{error}</p>

  return (
    <div className="card">
      <h1>机器总览</h1>
      <p className="muted">只读已上报快照。每 10 秒自动刷新。</p>
      <p>
        分组：
        <select value={group} onChange={(e) => setGroup(e.target.value)}>
          <option value="">全部</option>
          {groups.map((g) => <option key={g} value={g}>{g}</option>)}
        </select>
      </p>
      {visible.length === 0 ? (
        <p className="muted">暂无机器。请先启动 agent 并完成注册。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th>分组</th>
              <th>状态</th>
              <th>CPU</th>
              <th>内存</th>
              <th>GPU</th>
              <th>最满磁盘</th>
              <th>进程</th>
              <th>僵尸</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((h) => (
              <tr key={h.id}>
                <td><Link to={`/hosts/${h.id}`}>{h.name}</Link><div className="muted">{h.hostname}</div></td>
                <td className="muted">{h.group || '—'}</td>
                <td><span className={`badge ${h.status}`}>{h.status}</span></td>
                <td>{formatPercent(h.cpu_usage_percent)}</td>
                <td>{formatPercent(h.memory_used_percent)}</td>
                <td>
                  {h.gpu_count > 0 ? (
                    <>
                      <div>{h.gpu_count} 卡 · {formatPercent(h.max_gpu_util_percent)}</div>
                      <div className="muted">显存 {formatPercent(h.max_gpu_mem_percent)}</div>
                    </>
                  ) : <span className="muted">无</span>}
                </td>
                <td>
                  <div>{h.max_disk_mountpoint || '—'}</div>
                  <div className={`bar ${usageClass(h.max_disk_used_percent)}`}>
                    <span style={{ width: `${Math.min(h.max_disk_used_percent, 100)}%` }} />
                  </div>
                  <div className="muted">{formatPercent(h.max_disk_used_percent)}</div>
                </td>
                <td className={h.process_total > PROCESS_COUNT_WARN ? 'warn-text' : 'muted'}>
                  {h.process_total || '—'}
                </td>
                <td>
                  {h.zombie_count > 0 ? (
                    <span className="badge critical">僵尸 {h.zombie_count}</span>
                  ) : (
                    <span className="muted">0</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
