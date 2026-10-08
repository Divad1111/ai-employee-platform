/**
 * 用户管理列表（MultiUser RBAC §21）
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiDelete, apiGet, apiPost } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { IconPlus, IconRefresh, IconUsers } from '../components/Icons'
import { type SearchOption } from '../components/SearchableSelect'
import { roleDisplayName } from '../lib/rbacLabels'
import { usePerm } from '../stores/permissions'

type UserRow = {
  id: string
  username: string
  display_name: string
  email: string
  status: string
  roles: string[]
  workstation_count: number
  employee_count: number
  last_login_at?: string
  created_at?: string
}

type RoleItem = { name: string; description: string }

export function UsersPage() {
  const { canAll } = usePerm()
  const canCreate = canAll('user.create')
  const canDisable = canAll('user.disable')
  const canDelete = canAll('user.delete')
  const [items, setItems] = useState<UserRow[]>([])
  const [roles, setRoles] = useState<RoleItem[]>([])
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [role, setRole] = useState('USER')
  const [search, setSearch] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<UserRow | null>(null)
  const [deleting, setDeleting] = useState(false)

  const roleOptions: SearchOption[] = useMemo(() => {
    const fallback: RoleItem[] = [
      { name: 'USER', description: '普通用户' },
      { name: 'VIEWER', description: '只读' },
      { name: 'OPERATOR', description: '操作员' },
      { name: 'ADMIN', description: '管理员' },
    ]

    const src = roles.length > 0 ? roles : fallback
    return src
      .filter((r) => r.name !== 'SUPER_ADMIN')
      .map((r) => ({
        value: r.name,
        label: roleDisplayName(r.name, r.description),
        keywords: `${r.name} ${r.description || ''}`,
      }))
  }, [roles])

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return items
    return items.filter((u) => {
      const hay = [
        u.username,
        u.display_name,
        u.email,
        u.status,
        ...(u.roles || []),
      ]
        .join(' ')
        .toLowerCase()
      return hay.includes(q)
    })
  }, [items, search])

  const load = async () => {
    setLoading(true)
    setErr('')
    try {
      const [u, r] = await Promise.all([
        apiGet<{ items: UserRow[] }>('/users'),
        apiGet<{ items: RoleItem[] }>('/roles'),
      ])
      setItems(u.items || [])
      setRoles(r.items || [])
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const onCreate = async (e: FormEvent) => {
    e.preventDefault()
    setErr('')
    const targetRole = (role || 'USER').trim() || 'USER'
    try {
      await apiPost('/users', {
        username: username.trim(),
        password,
        display_name: (displayName || username).trim(),
        roles: [targetRole],
      })
      setShowCreate(false)
      setUsername('')
      setPassword('')
      setDisplayName('')
      setRole('USER')

      await load()
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  const toggle = async (u: UserRow) => {
    const path = u.status === 'disabled' ? `/users/${u.id}/enable` : `/users/${u.id}/disable`
    try {
      await apiPost(path)
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
      await apiDelete(`/users/${deleteTarget.id}`)
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
          <h1>用户管理</h1>
          <p>创建、启停、删除与角色分配。普通用户无法进入本页。</p>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button type="button" className="btn-ghost btn-sm" onClick={() => void load()} disabled={loading}>
            <IconRefresh size={14} />
            刷新
          </button>
          {canCreate ? (
          <button type="button" className="btn-success btn-sm" onClick={() => setShowCreate((v) => !v)}>
            <IconPlus size={14} />
            创建用户
          </button>
          ) : null}
        </div>
      </div>

      {err ? <div className="error" style={{ marginBottom: '0.8rem' }}>{err}</div> : null}

      {showCreate && canCreate ? (
        <div className="panel" style={{ marginBottom: '1rem' }}>
          <div className="panel-header">
            <div>
              <h2>新建用户</h2>
              <p>默认角色可随后在详情中调整</p>
            </div>
          </div>
          <form className="stack-form" onSubmit={onCreate}>
            <label>
              <span className="field-caption">用户名</span>
              <input value={username} onChange={(e) => setUsername(e.target.value)} required minLength={3} placeholder="登录账号" />
            </label>
            <label>
              <span className="field-caption">密码</span>
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} placeholder="至少 8 位" />
            </label>
            <label>
              <span className="field-caption">显示名</span>
              <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="可选，默认与用户名相同" />
            </label>
            <label>
              <span className="field-caption">初始角色</span>
              <select
                className="select"
                value={role}
                onChange={(e) => setRole(e.target.value)}
                required
                style={{ width: '100%', padding: '0.5rem 0.75rem' }}
              >
                {roleOptions.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label} ({opt.value})
                  </option>
                ))}
              </select>
              <p className="muted" style={{ margin: '0.25rem 0 0', fontSize: '0.78rem' }}>
                默认：普通用户（全量基础权限，作用域为仅本人）。可在创建后详情页随时调整。
              </p>
            </label>
            <div className="form-actions">
              <button type="submit" className="btn-success btn-sm">确认创建</button>
              <button type="button" className="btn-ghost btn-sm" onClick={() => setShowCreate(false)}>取消</button>
            </div>
          </form>
        </div>
      ) : null}

      <div className="panel">
        <div className="panel-header" style={{ alignItems: 'center', flexWrap: 'wrap', gap: '0.75rem' }}>
          <div>
            <h2>用户列表</h2>
            <p>
              共 {items.length} 位用户
              {search.trim() ? ` · 筛选后 ${filtered.length} 位` : ''}
            </p>
          </div>
          <div className="toolbar-search">
            <input
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索用户名 / 显示名 / 角色…"
              aria-label="搜索用户"
            />
          </div>
        </div>
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>用户</th>
                <th>角色</th>
                <th>状态</th>
                <th>工作站</th>
                <th>数字员工</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((u) => (
                <tr key={u.id}>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <IconUsers size={16} style={{ color: 'var(--brand-600)' }} />
                      <div>
                        <Link to={`/users/${u.id}`} style={{ fontWeight: 600 }}>
                          {u.username}
                        </Link>
                        <div className="muted" style={{ fontSize: '0.78rem' }}>
                          {u.display_name || '—'}
                        </div>
                      </div>
                    </div>
                  </td>
                  <td>
                    <span className="badge">
                      {(u.roles || []).map((r) => roleDisplayName(r)).join('、') || '—'}
                    </span>
                  </td>
                  <td>
                    <span className={u.status === 'active' ? 'auto-status is-on' : 'auto-status is-off'}>
                      {u.status === 'active' ? '启用' : u.status === 'disabled' ? '禁用' : u.status}
                    </span>
                  </td>
                  <td>{u.workstation_count ?? 0}</td>
                  <td>{u.employee_count ?? 0}</td>
                  <td className="col-actions">
                    <div className="table-actions">
                      <Link to={`/users/${u.id}`} className="btn-ghost btn-sm">详情</Link>
                      {canDisable ? (
                      <button
                        type="button"
                        className={u.status === 'disabled' ? 'btn-success btn-sm' : 'btn-warn btn-sm'}
                        onClick={() => void toggle(u)}
                      >
                        {u.status === 'disabled' ? '启用' : '禁用'}
                      </button>
                      ) : null}
                      {canDelete ? (
                      <button
                        type="button"
                        className="btn-danger btn-sm"
                        onClick={() => setDeleteTarget(u)}
                      >
                        删除
                      </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
              {filtered.length === 0 ? (
                <tr>
                  <td colSpan={6} className="empty-tip">
                    {items.length === 0 ? '暂无用户' : '无匹配的用户'}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      <ConfirmDialog
        open={!!deleteTarget}
        title="删除用户"
        description="软删除后该账号无法登录，列表中不再显示。此操作需具备 user.delete 权限。"
        targetLabel={deleteTarget?.username}
        targetMeta={deleteTarget?.display_name}
        confirmText="确认删除"
        danger
        busy={deleting}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  )
}
