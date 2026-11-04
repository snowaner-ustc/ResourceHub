import { MetricPoint } from './api'

export default function Sparkline({
  title,
  points,
  color = '#2563eb',
}: {
  title: string
  points: MetricPoint[]
  color?: string
}) {
  const w = 520
  const h = 120
  const pad = 8
  if (!points.length) {
    return (
      <div className="chart">
        <div className="label">{title}</div>
        <p className="muted">暂无历史数据（上报后约 10s 起有点）。</p>
      </div>
    )
  }
  const xs = points.map((p) => new Date(p.ts).getTime())
  const ys = points.map((p) => p.value)
  const minX = Math.min(...xs)
  const maxX = Math.max(...xs)
  const minY = 0
  const maxY = Math.max(100, ...ys)
  const dx = Math.max(maxX - minX, 1)
  const dy = Math.max(maxY - minY, 1)
  const coords = points.map((p, i) => {
    const x = pad + ((xs[i] - minX) / dx) * (w - pad * 2)
    const y = h - pad - ((p.value - minY) / dy) * (h - pad * 2)
    return `${x},${y}`
  })
  const last = points[points.length - 1]
  return (
    <div className="chart">
      <div className="label">{title} · 最新 {last.value.toFixed(1)}%</div>
      <svg viewBox={`0 0 ${w} ${h}`} width="100%" height={h} role="img" aria-label={title}>
        <polyline fill="none" stroke={color} strokeWidth="2" points={coords.join(' ')} />
      </svg>
    </div>
  )
}
