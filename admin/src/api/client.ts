/**
 * Control Plane HTTP API 客户端。
 * 所有数据访问必须经 REST，禁止前端直连数据库。
 */

const SESSION_KEY = 'aie_browser_session'
// 清除旧版本保存的 Bearer Token，升级后重新登录。
localStorage.removeItem('aie_admin_token')
export function hasSession(): boolean { return localStorage.getItem(SESSION_KEY) === '1' }
export function setSessionPresent(present: boolean): void {
  if (present) localStorage.setItem(SESSION_KEY, '1')
  else localStorage.removeItem(SESSION_KEY)
}
export function browserHeaders(): Record<string, string> { return { 'X-AIE-Browser': '1' } }

/** 发起 JSON 请求 */
export async function apiRequest<T>(
  path: string,
  opts: RequestInit & { json?: unknown } = {},
): Promise<T> {
  const headers = new Headers(opts.headers)
  headers.set('Accept', 'application/json')
  headers.set('X-AIE-Browser', '1')
  let body = opts.body
  if (opts.json !== undefined) {
    headers.set('Content-Type', 'application/json')
    body = JSON.stringify(opts.json)
  }
  const res = await fetch(`/api${path}`, { ...opts, headers, body })
  if (!res.ok) {
    let msg = `${res.status}`
    try {
      const err = (await res.json()) as { error?: string; needs_mfa_enrollment?: boolean }
      if (err.error) msg = err.error
      if (err.needs_mfa_enrollment && window.location.pathname !== "/approvals") window.location.assign("/approvals")
    } catch {
      /* ignore */
    }
    // 二次认证密码错误不能清掉当前登录会话。
    const wrongPassword = msg.includes('密码错误')
    if (res.status === 401 && !wrongPassword) {
      setSessionPresent(false)
    }
    throw new Error(msg)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export const apiGet = <T>(path: string) => apiRequest<T>(path)
export const apiPost = <T>(path: string, json?: unknown) =>
  apiRequest<T>(path, { method: 'POST', json })
export const apiPut = <T>(path: string, json?: unknown) =>
  apiRequest<T>(path, { method: 'PUT', json })
export const apiPatch = <T>(path: string, json?: unknown) =>
  apiRequest<T>(path, { method: 'PATCH', json })
export const apiDelete = <T>(path: string) =>
  apiRequest<T>(path, { method: 'DELETE' })
