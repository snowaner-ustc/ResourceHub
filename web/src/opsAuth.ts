const OPS_KEY = 'resourcehub.opsPassword'
const OPS_HEADER = 'X-Ops-Password'

type PromptFn = (reason: string) => Promise<string | null>

let promptOps: PromptFn = async (reason) => window.prompt(reason)

type Guard = boolean | undefined
let guardCache: Guard
let promptInflight: Promise<string | null> | null = null

export function registerOpsPrompt(fn: PromptFn) {
  promptOps = fn
}

async function probeOpsGuard(): Promise<Guard> {
  if (guardCache !== undefined) return guardCache
  try {
    const res = await fetch('/api/v1/health')
    if (!res.ok) return undefined
    const body = await res.json() as { ops_guard?: unknown }
    if (typeof body.ops_guard === 'boolean') {
      guardCache = body.ops_guard
      return guardCache
    }
  } catch {
    /* health unreachable or not JSON: do not cache a false "no password needed" */
  }
  return undefined
}

function askPassword(reason: string): Promise<string | null> {
  if (!promptInflight) {
    promptInflight = promptOps(reason).finally(() => {
      promptInflight = null
    })
  }
  return promptInflight
}

export function forgetOpsPassword() {
  sessionStorage.removeItem(OPS_KEY)
}

export class OpsCancelledError extends Error {
  constructor() {
    super('已取消扫描')
    this.name = 'OpsCancelledError'
  }
}

export async function withOpsHeaders(
  run: (headers: Record<string, string>) => Promise<Response>,
): Promise<Response> {
  const guard = await probeOpsGuard()
  if (guard === false) {
    return run({})
  }

  const send = (password: string | null) => {
    const headers: Record<string, string> = {}
    if (password) headers[OPS_HEADER] = password
    return run(headers)
  }

  let password = sessionStorage.getItem(OPS_KEY)
  if (guard === true && !password) {
    password = await askPassword('这次会在机器上扫描目录，请输入运维口令')
    if (!password) throw new OpsCancelledError()
    sessionStorage.setItem(OPS_KEY, password)
  }

  let res = await send(password)
  if (res.status === 401) {
    guardCache = true
    forgetOpsPassword()
    password = await askPassword('口令不正确或尚未输入，请输入运维口令')
    if (!password) throw new OpsCancelledError()
    sessionStorage.setItem(OPS_KEY, password)
    res = await send(password)
  }
  return res
}

export function isOpsCancelled(e: unknown): boolean {
  return e instanceof OpsCancelledError || (e as Error)?.name === 'OpsCancelledError'
}
