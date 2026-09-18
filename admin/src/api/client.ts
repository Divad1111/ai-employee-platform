/**
 * Control Plane HTTP API 客户端。
 * 所有数据访问必须经 REST，禁止前端直连数据库。
 */

const TOKEN_KEY = 'aie_admin_token'

/** 读取本地会话 token */
export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

/** 保存 token */
export function setToken(token: string | null): void {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

/** 发起 JSON 请求 */
export async function apiRequest<T>(
  path: string,
  opts: RequestInit & { json?: unknown } = {},
): Promise<T> {
  const headers = new Headers(opts.headers)
  headers.set('Accept', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  let body = opts.body
  if (opts.json !== undefined) {
    headers.set('Content-Type', 'application/json')
    body = JSON.stringify(opts.json)
  }
  const res = await fetch(`/api${path}`, { ...opts, headers, body })
  if (res.status === 401) {
    setToken(null)
  }
  if (!res.ok) {
    let msg = `${res.status}`
    try {
      const err = (await res.json()) as { error?: string }
      if (err.error) msg = err.error
    } catch {
      /* ignore */
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
