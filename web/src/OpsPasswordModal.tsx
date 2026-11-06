import { FormEvent, useEffect, useRef, useState } from 'react'
import { registerOpsPrompt } from './opsAuth'

export default function OpsPasswordModal() {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [value, setValue] = useState('')
  const waiters = useRef<Array<(v: string | null) => void>>([])
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    registerOpsPrompt((msg) => new Promise((resolve) => {
      waiters.current.push(resolve)
      setReason(msg)
      setValue('')
      setOpen(true)
    }))
    return () => registerOpsPrompt(async (msg) => window.prompt(msg))
  }, [])

  useEffect(() => {
    if (open) inputRef.current?.focus()
  }, [open])

  if (!open) return null

  const finish = (v: string | null) => {
    const pending = waiters.current
    waiters.current = []
    setOpen(false)
    for (const resolve of pending) resolve(v)
  }

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    finish(value)
  }

  return (
    <div className="ops-overlay" role="dialog" aria-modal="true" aria-labelledby="ops-title">
      <form className="ops-dialog" onSubmit={onSubmit}>
        <h2 id="ops-title">运维口令</h2>
        <p className="muted">{reason || '扫描会占用机器 IO，需要口令后才会下发任务。看板仍可随便看。'}</p>
        <input
          ref={inputRef}
          type="password"
          autoComplete="off"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="RESOURCEHUB_OPS_PASSWORD"
        />
        <div className="ops-actions">
          <button type="button" onClick={() => finish(null)}>取消</button>
          <button type="submit">确认</button>
        </div>
      </form>
    </div>
  )
}
