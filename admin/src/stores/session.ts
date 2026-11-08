/**
 * 登录会话：HttpOnly Cookie 持久化，前端只保存会话存在标记，刷新保持登录。
 */

import { hasSession, setSessionPresent } from '../api/client'

export type SessionUser = {
  id: string
  username: string
  roles: string[]
}

let user: SessionUser | null = null

export function isAuthenticated(): boolean {
  return hasSession()
}

export function getUser(): SessionUser | null {
  return user
}

export function setSession(next: SessionUser): void {
  setSessionPresent(true)
  user = next
}

export function clearSession(): void {
  setSessionPresent(false)
  user = null
}
