import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Alert, fetchAlerts } from '../api'

export default function AlertsPage() {
  const [alerts, setAlerts] = useState<Alert[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const data = await fetchAlerts('firing')
        if (alive) setAlerts(data)
      } catch (e) {
        if (alive) setError((e as Error).message)
      }
    }
    load()
    const t = setInterval(load, 15000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  if (error) return <p className="error">{error}</p>

  return (
    <div className="card">
      <h1>告警</h1>
      <p className="muted">当前 firing 告警（磁盘阈值 / Agent 掉线）。</p>
      {alerts.length === 0 ? (
        <p className="muted">暂无活跃告警。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>严重度</th>
              <th>机器</th>
              <th>规则</th>
              <th>消息</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody>
            {alerts.map((a) => (
              <tr key={a.id}>
                <td><span className={`badge ${a.severity}`}>{a.severity}</span></td>
                <td><Link to={`/hosts/${a.host_id}`}>{a.host_name}</Link></td>
                <td className="muted">{a.rule_type}</td>
                <td>{a.message}</td>
                <td className="muted">{new Date(a.fired_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
