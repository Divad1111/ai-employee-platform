/**
 * 角色与权限矩阵（§23）— 默认折叠，可新建/删除自定义角色
 */
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiDelete, apiGet, apiPatch } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { IconPlus } from '../components/Icons'
import { SearchableSelect } from '../components/SearchableSelect'
import { permLabel, roleDisplayName, SCOPE_LABEL, SCOPE_OPTIONS } from '../lib/rbacLabels'
import { usePerm } from '../stores/permissions'

const BUILTIN_ROLES = new Set(['SUPER_ADMIN', 'ADMIN', 'OPERATOR', 'USER', 'VIEWER'])


type Grant = { code?: string; Code?: string; scope?: string; Scope?: string }
type Role = { name: string; description: string; grants: Grant[] }
type PermInfo = { code: string; description: string }

function grantCode(g: Grant) {
  return g.code || g.Code || ''
}
function grantScope(g: Grant) {
  return (g.scope || g.Scope || 'NONE').toUpperCase()
}

export function RolesPage() {
  const { canAll } = usePerm()
  const canCreate = canAll('role.create') || canAll('role.update')
  const canDelete = canAll('role.delete')
  const [roles, setRoles] = useState<Role[]>([])
  const [permMap, setPermMap] = useState<Record<string, string>>({})
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [loading, setLoading] = useState(false)
  /** 展开的角色名集合；默认全部折叠 */
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const [deleteTarget, setDeleteTarget] = useState<Role | null>(null)
  const [deleting, setDeleting] = useState(false)

  const scopeOptions = useMemo(
    () => SCOPE_OPTIONS.map((o) => ({ value: o.value, label: o.label })),
    [],
  )

  const load = async () => {
    setLoading(true)
    setErr('')
    try {
      const [r, p] = await Promise.all([
        apiGet<{ items: Role[] }>('/roles'),
        apiGet<{ items: PermInfo[] }>('/permissions'),
      ])
      setRoles(r.items || [])
      const m: Record<string, string> = {}
      for (const it of p.items || []) m[it.code] = it.description
      setPermMap(m)
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const toggle = (name: string) => {
    setOpen((prev) => ({ ...prev, [name]: !prev[name] }))
  }

  const updateScope = async (role: string, permission: string, scope: string) => {
    if (!permission) return
    setMsg('')
    try {
      await apiPatch(`/roles/${encodeURIComponent(role)}/permissions`, { permission, scope })
      setMsg(`已更新 ${roleDisplayName(role)} / ${permLabel(permission, permMap[permission])} → ${SCOPE_LABEL[scope] || scope}`)
      await load()
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setErr('')
    try {
      await apiDelete(`/roles/${encodeURIComponent(deleteTarget.name)}`)
      setMsg(`已删除角色 ${roleDisplayName(deleteTarget.name, deleteTarget.description)}`)
      setDeleteTarget(null)
      await load()
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="page">
      <div className="page-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1rem' }}>
        <div>
          <h1>角色与权限</h1>
          <p>权限决定「能不能做」，作用范围决定「能对谁做」</p>
        </div>
        {canCreate ? (
          <Link to="/roles/new" className="btn-success btn-sm" style={{ textDecoration: 'none' }}>
            <IconPlus size={14} />
            新建角色
          </Link>
        ) : null}
      </div>

      {err ? <div className="error" style={{ marginBottom: '0.8rem' }}>{err}</div> : null}
      {msg ? <p className="muted">{msg}</p> : null}
      {loading && !roles.length ? <p className="muted">加载中…</p> : null}

      {roles.map((role) => {
        const grants = [...(role.grants || [])].sort((a, b) => grantCode(a).localeCompare(grantCode(b)))
        const expanded = !!open[role.name]
        const builtin = BUILTIN_ROLES.has(role.name)
        return (
          <div key={role.name} className="panel" style={{ marginBottom: '1rem' }}>
            <div className="panel-header role-panel-header">
              <div className="role-panel-title">
                <h2 style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', margin: 0 }}>
                  {roleDisplayName(role.name, role.description)}
                  <span className="badge" style={{ fontWeight: 500 }}>{role.name}</span>
                </h2>
                <p style={{ margin: '0.35rem 0 0' }}>
                  {role.description || '—'} · {grants.length} 项权限
                  {!expanded ? ' · 点击右上角展开' : ''}
                </p>
              </div>
              <div className="role-panel-actions">
                {canDelete && !builtin ? (
                  <button
                    type="button"
                    className="btn-danger btn-sm"
                    onClick={() => setDeleteTarget(role)}
                  >
                    删除
                  </button>
                ) : null}
                <button
                  type="button"
                  className="role-collapse-toggle"
                  onClick={() => toggle(role.name)}
                  aria-expanded={expanded}
                  aria-label={expanded ? '收起' : '展开'}
                  title={expanded ? '收起' : '展开'}
                >
                  <span className={`role-collapse-chevron${expanded ? ' is-open' : ''}`} aria-hidden>▶</span>
                </button>
              </div>
            </div>
            {expanded ? (
              <div className="table-wrapper">
                <table className="table">
                  <thead>
                    <tr>
                      <th>权限</th>
                      <th>当前范围</th>
                      <th className="col-actions">调整</th>
                    </tr>
                  </thead>
                  <tbody>
                    {grants.map((g) => {
                      const code = grantCode(g)
                      const scope = grantScope(g)
                      return (
                        <tr key={`${role.name}-${code}`}>
                          <td>
                            <div style={{ fontWeight: 600 }}>{permLabel(code, permMap[code])}</div>
                            <code className="muted" style={{ fontSize: '0.75rem' }}>{code || '（空）'}</code>
                          </td>
                          <td>
                            <span className="badge">{SCOPE_LABEL[scope] || scope}</span>
                          </td>
                          <td className="col-actions">
                            {role.name === 'SUPER_ADMIN' ? (
                              <span className="muted">通配全部权限</span>
                            ) : (
                              <SearchableSelect
                                value={scope}
                                onChange={(v) => void updateScope(role.name, code, v)}
                                options={scopeOptions}
                                disabled={!code}
                                placeholder="选择作用范围…"
                              />
                            )}
                          </td>
                        </tr>
                      )
                    })}
                    {!grants.length ? (
                      <tr><td colSpan={3} className="empty-tip">该角色暂无权限赋权</td></tr>
                    ) : null}
                  </tbody>
                </table>
              </div>
            ) : null}
          </div>
        )
      })}

      <ConfirmDialog
        open={!!deleteTarget}
        title="删除角色"
        description={deleteTarget ? `确认删除自定义角色「${roleDisplayName(deleteTarget.name, deleteTarget.description)}」？此操作不可恢复。` : ''}
        targetLabel={deleteTarget?.name}
        confirmText="确认删除"
        danger
        busy={deleting}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  )
}
