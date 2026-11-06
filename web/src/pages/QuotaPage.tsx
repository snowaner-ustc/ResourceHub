import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  QuotaPolicy,
  QuotaUsage,
  createQuotaPolicy,
  deleteQuotaPolicy,
  fetchQuotaPolicies,
  fetchQuotaUsages,
  formatBytes,
  formatPercent,
  patchQuotaPolicy,
  triggerQuotaScan,
} from '../api'

const GiB = 1024 * 1024 * 1024

export default function QuotaPage() {
  const [policies, setPolicies] = useState<QuotaPolicy[]>([])
  const [usages, setUsages] = useState<QuotaUsage[]>([])
  const [error, setError] = useState('')
  const [name, setName] = useState('home-subdir')
  const [pattern, setPattern] = useState('/home/*/*')
  const [limitGiB, setLimitGiB] = useState('100')
  const [group, setGroup] = useState('')
  const [hours, setHours] = useState('24')

  const reload = async () => {
    const [p, u] = await Promise.all([fetchQuotaPolicies(), fetchQuotaUsages()])
    setPolicies(p)
    setUsages(u)
  }

  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        await reload()
      } catch (e) {
        if (alive) setError((e as Error).message)
      }
    }
    load()
    const t = setInterval(load, 10000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  const onCreate = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    const gib = Number(limitGiB)
    const hrs = Number(hours)
    if (!gib || gib <= 0) {
      setError('限额必须大于 0 GiB')
      return
    }
    try {
      await createQuotaPolicy({
        name: name.trim(),
        enabled: true,
        host_group: group.trim(),
        path_pattern: pattern.trim(),
        limit_bytes: Math.round(gib * GiB),
        warn_percent: 80,
        critical_percent: 100,
        scan_interval_seconds: Math.max(60, Math.round((hrs || 24) * 3600)),
        exclude: ['.cache', 'node_modules'],
      })
      await reload()
    } catch (err) {
      setError((err as Error).message)
    }
  }

  return (
    <div className="card">
      <h1>软配额</h1>
      <p className="banner">
        仅软限制：超限只告警/提醒，<strong>不拦截写入、不自动删除</strong>。扫描走慢路径，不绑定看板刷新。
      </p>
      {error ? <p className="error">{error}</p> : null}

      <h2>策略</h2>
      <form onSubmit={onCreate} style={{ display: 'grid', gap: 8, marginBottom: 16 }}>
        <label>名称 <input value={name} onChange={(e) => setName(e.target.value)} required /></label>
        <label>路径模板 <input value={pattern} onChange={(e) => setPattern(e.target.value)} style={{ minWidth: 280 }} required /></label>
        <label>每文件夹限额（GiB） <input value={limitGiB} onChange={(e) => setLimitGiB(e.target.value)} /></label>
        <label>扫描间隔（小时） <input value={hours} onChange={(e) => setHours(e.target.value)} /></label>
        <label>仅作用于分组（可空） <input value={group} onChange={(e) => setGroup(e.target.value)} placeholder="例如 training" /></label>
        <div><button type="submit">创建策略</button></div>
      </form>

      {policies.length === 0 ? (
        <p className="muted">还没有策略。例如 <code>/home/*/*</code> 表示每个用户下的一级文件夹。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th>模板</th>
              <th>限额</th>
              <th>间隔</th>
              <th>状态</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {policies.map((p) => (
              <tr key={p.id}>
                <td>{p.name}{p.host_group ? <div className="muted">分组 {p.host_group}</div> : null}</td>
                <td className="muted">{p.path_pattern}</td>
                <td>{formatBytes(p.limit_bytes)}（{p.warn_percent}% / {p.critical_percent}%）</td>
                <td className="muted">{Math.round(p.scan_interval_seconds / 3600)}h</td>
                <td><span className={`badge ${p.enabled ? 'online' : 'offline'}`}>{p.enabled ? 'enabled' : 'disabled'}</span></td>
                <td>
                  <button type="button" className="linkish" onClick={() => void triggerQuotaScan(p.id).then(reload).catch((e) => setError((e as Error).message))}>立即扫描</button>
                  {' · '}
                  <button
                    type="button"
                    className="linkish"
                    onClick={() => void patchQuotaPolicy(p.id, { ...p, enabled: !p.enabled }).then(reload).catch((e) => setError((e as Error).message))}
                  >
                    {p.enabled ? '停用' : '启用'}
                  </button>
                  {' · '}
                  <button type="button" className="linkish" onClick={() => void deleteQuotaPolicy(p.id).then(reload).catch((e) => setError((e as Error).message))}>删除</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>最新用量</h2>
      <p className="muted">来自最近一次定向扫描缓存，不是实时 du。看 as_of 判断新旧。扫描间隔请大于实际 walk 时间，否则超时机会按间隔反复扫。</p>
      {usages.some((u) => u.partial_scan) ? (
        <p className="muted">存在不完整扫描：未扫到的路径不会自动消失，已删除目录需等一次完整扫描才会清除。</p>
      ) : null}
      {usages.length === 0 ? (
        <p className="muted">尚无用量。创建策略后点「立即扫描」，并确保对应机器的 agent 在线。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>机器</th>
              <th>策略</th>
              <th>路径</th>
              <th>所有者</th>
              <th>已用</th>
              <th>限额</th>
              <th>利用率</th>
              <th>as_of</th>
            </tr>
          </thead>
          <tbody>
            {usages.map((u) => (
              <tr key={`${u.host_id}-${u.policy_id}-${u.path}`}>
                <td><Link to={`/hosts/${u.host_id}`}>{u.host_name}</Link></td>
                <td className="muted">{u.policy_name}</td>
                <td>{u.path}</td>
                <td className="muted">{u.owner || '—'}</td>
                <td>{formatBytes(u.used_bytes)}</td>
                <td>{formatBytes(u.limit_bytes)}</td>
                <td className={u.used_percent >= 100 ? 'error' : u.used_percent >= 80 ? 'warn-text' : ''}>
                  {formatPercent(u.used_percent)}
                </td>
                <td className="muted">{new Date(u.as_of).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
