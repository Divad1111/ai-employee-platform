/**
 * 当前登录用户权限（来自 /auth/me）
 */
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { apiGet } from '../api/client'
import { getUser } from '../stores/session'

type PermContextValue = {
  /** null = 加载中 */
  ready: boolean
  roles: string[]
  /** 权限码集合；含 * 表示超级管理员 */
  perms: Set<string>
  can: (...codes: string[]) => boolean
  /** 任一码命中即可 */
  canAny: (...codes: string[]) => boolean
  /** 该权限码的范围是全部资源（超级管理员视为 ALL） */
  canAll: (code: string) => boolean
}

const PermContext = createContext<PermContextValue>({
  ready: false,
  roles: [],
  perms: new Set(),
  can: () => false,
  canAny: () => false,
  canAll: () => false,
})

function buildPermSet(permissions: Array<{ name: string; scope?: string }> | undefined, roles: string[]) {
  const s = new Set<string>()
  for (const p of permissions || []) {
    if (p?.name) s.add(p.name)
  }
  if (s.has('*') || roles.includes('SUPER_ADMIN')) {
    s.add('*')
  }
  return s
}

export function PermissionProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false)
  const [roles, setRoles] = useState<string[]>(() => getUser()?.roles || [])
  const [perms, setPerms] = useState<Set<string>>(() => new Set())
  const [scopes, setScopes] = useState<Record<string, string>>({})

  useEffect(() => {
    let cancelled = false
    apiGet<{ permissions?: Array<{ name: string; scope?: string }>; roles?: string[] }>('/auth/me')
      .then((me) => {
        if (cancelled) return
        const r = me.roles || getUser()?.roles || []
        setRoles(r)
        setPerms(buildPermSet(me.permissions, r))
        const next: Record<string, string> = {}
        for (const p of me.permissions || []) {
          if (p?.name) next[p.name] = (p.scope || '').toUpperCase()
        }
        setScopes(next)
        setReady(true)
      })
      .catch(() => {
        if (cancelled) return
        const r = getUser()?.roles || []
        setRoles(r)
        setPerms(buildPermSet([], r))
        setReady(true)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const value = useMemo<PermContextValue>(() => {
    const can = (...codes: string[]) => {
      if (!ready) return false
      if (perms.has('*')) return true
      return codes.every((c) => perms.has(c))
    }
    const canAny = (...codes: string[]) => {
      if (!ready) return false
      if (perms.has('*')) return true
      return codes.some((c) => perms.has(c))
    }
    const canAll = (code: string) => {
      if (!ready) return false
      if (perms.has('*')) return true
      return scopes[code] === 'ALL'
    }
    return { ready, roles, perms, can, canAny, canAll }
  }, [ready, roles, perms, scopes])

  return <PermContext.Provider value={value}>{children}</PermContext.Provider>
}

export function usePerm() {
  return useContext(PermContext)
}
