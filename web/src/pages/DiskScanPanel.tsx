import { FormEvent, useEffect, useState } from 'react'
import { DiskEntry, DiskScanReport, formatBytes, SlowJob, fetchDiskScan, startDiskScan } from '../api'

export default function DiskScanPanel({
  hostId,
  mountpoints,
}: {
  hostId: string
  mountpoints: string[]
}) {
  const [root, setRoot] = useState(defaultRoot(mountpoints))
  const [job, setJob] = useState<SlowJob | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setJob(null)
    setError('')
    setBusy(false)
    setRoot(defaultRoot(mountpoints))
  }, [hostId])

  useEffect(() => {
    if (!job || job.status === 'done' || job.status === 'failed') return
    let alive = true
    const t = setInterval(async () => {
      try {
        const next = await fetchDiskScan(hostId, job.id)
        if (alive) setJob(next)
      } catch (e) {
        if (alive) setError((e as Error).message)
      }
    }, 1500)
    return () => { alive = false; clearInterval(t) }
  }, [hostId, job?.id, job?.status])

  const run = async (path: string, force: boolean) => {
    setError('')
    setBusy(true)
    try {
      const next = await startDiskScan(hostId, path, force)
      setRoot(path)
      setJob(next)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    void run(root.trim(), false)
  }

  const report = job?.status === 'done' ? job.report : undefined

  return (
    <>
      <h2>目录分析（慢路径）</h2>
      <p className="muted">
        分层统计<strong>直接子目录</strong>体积，用于定位大户。不跟心跳绑定；禁止从 <code>/</code> 全盘扫描。结果带采集时刻，可能非实时。
      </p>
      <form onSubmit={onSubmit} style={{ marginBottom: 12 }}>
        <input
          value={root}
          onChange={(e) => setRoot(e.target.value)}
          placeholder="/home 或 /var"
          style={{ minWidth: 280 }}
        />
        {' '}
        <button type="submit" disabled={busy}>开始分析</button>
        {' '}
        <button type="button" disabled={busy || !root.trim()} onClick={() => void run(root.trim(), true)}>忽略缓存</button>
      </form>
      <p className="muted">
        快捷：
        {mountpoints.filter((m) => m !== '/').map((m) => (
          <button key={m} type="button" className="linkish" onClick={() => void run(m, false)} style={{ marginRight: 8 }}>
            {m}
          </button>
        ))}
        {mountpoints.includes('/') ? <span>根分区请填写子目录</span> : null}
      </p>
      {error ? <p className="error">{error}</p> : null}
      {job ? (
        <p className="muted">
          任务 {job.status}
          {job.cached ? ' · 复用缓存' : ''}
          {job.error ? ` · ${job.error}` : ''}
        </p>
      ) : null}
      {report ? <ReportView report={report} onDrill={(p) => void run(p, false)} /> : null}
    </>
  )
}

function ReportView({ report, onDrill }: { report: DiskScanReport; onDrill: (path: string) => void }) {
  return (
    <>
      <p className="muted">
        根 {report.root} · as_of {new Date(report.as_of).toLocaleString()} · {report.duration_ms}ms · 合计 {formatBytes(report.root_bytes || 0)}
        {report.partial ? ` · 部分结果（${report.partial_reason || 'truncated'}）` : ''}
        {report.scan_errors ? ` · 错误 ${report.scan_errors}` : ''}
      </p>
      <table>
        <thead>
          <tr>
            <th>路径</th>
            <th>类型</th>
            <th>所有者</th>
            <th>体积</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {(report.entries || []).map((e) => (
            <tr key={e.path}>
              <td>{e.path}</td>
              <td className="muted">{e.entry_type}</td>
              <td className="muted">{e.owner || '—'}</td>
              <td>{formatBytes(e.bytes)}</td>
              <td>
                {e.entry_type === 'dir' ? (
                  <button type="button" className="linkish" onClick={() => onDrill(e.path)}>下钻</button>
                ) : null}
              </td>
            </tr>
          ))}
          {report.others_bytes > 0 ? (
            <tr>
              <td className="muted">Others</td>
              <td></td>
              <td></td>
              <td>{formatBytes(report.others_bytes)}</td>
              <td></td>
            </tr>
          ) : null}
        </tbody>
      </table>
      {(report.by_owner || []).length > 0 ? (
        <>
          <h3>按所有者</h3>
          <table>
            <thead><tr><th>用户</th><th>体积</th></tr></thead>
            <tbody>
              {report.by_owner.map((o) => (
                <tr key={o.owner}>
                  <td>{o.owner || '—'}</td>
                  <td>{formatBytes(o.bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      ) : null}
      {(report.large_files || []).length > 0 ? (
        <>
          <h3>大文件 Top</h3>
          <EntryTable rows={report.large_files} />
        </>
      ) : null}
    </>
  )
}

function EntryTable({ rows }: { rows: DiskEntry[] }) {
  return (
    <table>
      <thead>
        <tr>
          <th>路径</th>
          <th>所有者</th>
          <th>体积</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((e) => (
          <tr key={e.path}>
            <td>{e.path}</td>
            <td className="muted">{e.owner || '—'}</td>
            <td>{formatBytes(e.bytes)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function defaultRoot(mounts: string[]): string {
  const first = mounts.find((m) => m !== '/')
  return first || ''
}
