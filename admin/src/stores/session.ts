/**
 * 登录会话：token 持久化到 localStorage，刷新保持登录。
 */

import { getToken, setToken } from '../api/client'

export type SessionUser = {
  id: string
  username: string
  roles: string[]
}

let user: SessionUser | null = null

export function isAuthenticated(): boolean {
  return Boolean(getToken())
}

export function getUser(): SessionUser | null {
  return user
}

export function setSession(token: string, next: SessionUser): void {
  setToken(token)
  user = next
}

export function clearSession(): void {
  setToken(null)
  user = null
}
