const OPS_KEY = 'resourcehub.opsPassword'
const OPS_HEADER = 'X-Ops-Password'

type PromptFn = (reason: string) => Promise<string | null>

let promptOps: PromptFn = async (reason) => window.prompt(reason)

let guardCache: boolean | undefined

export function registerOpsPrompt(fn: PromptFn) {
  promptOps = fn
}

export async function opsGuardEnabled(): Promise<boolean> {
  if (guardCache !== undefined) return guardCache
  try {
    const res = await fetch('/api/v1/health')
    const body = await res.json() as { ops_guard?: boolean }
    guardCache = !!body.ops_guard
  } catch {
    guardCache = false
  }
  return guardCache
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
  if (!(await opsGuardEnabled())) {
    return run({})
  }
  let password = sessionStorage.getItem(OPS_KEY)
  if (!password) {
    password = await promptOps('这次会在机器上扫描目录，请输入运维口令')
    if (!password) throw new OpsCancelledError()
    sessionStorage.setItem(OPS_KEY, password)
  }
  let res = await run({ [OPS_HEADER]: password })
  if (res.status === 401) {
    forgetOpsPassword()
    password = await promptOps('口令不正确，请重新输入运维口令')
    if (!password) throw new OpsCancelledError()
    sessionStorage.setItem(OPS_KEY, password)
    res = await run({ [OPS_HEADER]: password })
  }
  return res
}

export function isOpsCancelled(e: unknown): boolean {
  return e instanceof OpsCancelledError || (e as Error)?.name === 'OpsCancelledError'
}
