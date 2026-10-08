/**
 * 用户详情（§22）
 */
import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { apiGet, apiPatch, apiPost } from '../api/client'
import { roleDisplayName } from '../lib/rbacLabels'
import { usePerm } from '../stores/permissions'

type Detail = {
  id: string
  username: string
  display_name: string
  email: string
  status: string
  roles: string[]
  workstations?: Array<{ workstation_id: string; name?: string; role: string }>
  employees?: Array<{ id: string; name: string }>
}

type RoleItem = { name: string; description: string }

function stationRoleLabel(role: string) {
  if (role === 'OWNER') return '所有者'
  if (role === 'MEMBER') return '成员'
  if (role === '角色授权') return '按角色授权'
  return role || '—'
}

export function UserDetailPage() {
  const { id } = useParams()
  const { roles: myRoles } = usePerm()
  const iAmSuper = myRoles.includes('SUPER_ADMIN')
  const [d, setD] = useState<Detail | null>(null)
  const [err, setErr] = useState('')
  const [roles, setRoles] = useState<RoleItem[]>([])
  const [role, setRole] = useState('')
  const [savingRole, setSavingRole] = useState(false)
  const [roleErr, setRoleErr] = useState('')

  const load = async () => {
    if (!id) return
    try {
      setD(await apiGet<Detail>(`/users/${id}`))
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  useEffect(() => {
    void load()
  }, [id])

  useEffect(() => {
    if (!iAmSuper) return
    apiGet<{ items: RoleItem[] }>('/roles')
      .then((res) => setRoles(res.items || []))
      .catch(() => setRoles([]))
  }, [iAmSuper])

  const targetIsSuper = (d?.roles || []).includes('SUPER_ADMIN')
  const canEditRole = iAmSuper && !!d && !targetIsSuper
  const roleOptions = useMemo(() => {
    const src = roles.length > 0 ? roles : [
      { name: 'ADMIN', description: '管理员' },
      { name: 'OPERATOR', description: '操作员' },
      { name: 'USER', description: '普通用户' },
      { name: 'VIEWER', description: '只读' },
    ]
    return src.filter((r) => r.name !== 'SUPER_ADMIN')
  }, [roles])

  const roleKey = (d?.roles || []).join(',')
  useEffect(() => {
    if (!d) return
    const current = (d.roles || []).find((r) => r !== 'SUPER_ADMIN') || d.roles?.[0] || 'USER'
    setRole(current)
  }, [d, roleKey])

  const saveRole = async () => {
    if (!d || !canEditRole || !role) return
    setSavingRole(true)
    setRoleErr('')
    try {
      await apiPatch(`/users/${d.id}`, { roles: [role] })
      await load()
    } catch (e: unknown) {
      setRoleErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSavingRole(false)
    }
  }

  const toggle = async () => {
    if (!d) return
    const path = d.status === 'disabled' ? `/users/${d.id}/enable` : `/users/${d.id}/disable`
    try {
      await apiPost(path)
      await load()
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  if (err) return <div className="page"><div className="error">{err}</div></div>
  if (!d) return <div className="page"><p className="muted">加载中…</p></div>

  return (
    <div className="page">
      <div className="page-header" style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem' }}>
        <div>
          <Link to="/users" className="muted" style={{ fontSize: '0.85rem' }}>← 返回用户列表</Link>
          <h1 style={{ marginTop: '0.35rem' }}>{d.display_name || d.username}</h1>
          <p>
            {d.username} · {(d.roles || []).map((r) => roleDisplayName(r)).join('、') || '无角色'} ·{' '}
            {d.status === 'active' ? '启用' : d.status}
          </p>
        </div>
        <button
          type="button"
          className={d.status === 'disabled' ? 'btn-success btn-sm' : 'btn-warn btn-sm'}
          onClick={() => void toggle()}
        >
          {d.status === 'disabled' ? '启用用户' : '禁用用户'}
        </button>
      </div>

      <div className="panel" style={{ marginBottom: '1rem' }}>
        <div className="panel-header">
          <div>
            <h2>基本信息</h2>
            <p>账号与联系方式</p>
          </div>
        </div>
        <div style={{ padding: '0 1.1rem 1.1rem' }}>
          <p>邮箱：{d.email || '—'}</p>
          <p>状态：{d.status}</p>
          <p>角色：{(d.roles || []).map((r) => roleDisplayName(r)).join('、') || '—'}</p>
          {canEditRole ? (
            <label style={{ display: 'block', marginTop: '0.75rem' }}>
              <span className="field-caption">修改角色</span>
              <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginTop: '0.35rem' }}>
                <select
                  className="select"
                  value={role}
                  onChange={(e) => setRole(e.target.value)}
                  style={{ minWidth: '12rem', padding: '0.45rem 0.7rem' }}
                >
                  {roleOptions.map((opt) => (
                    <option key={opt.name} value={opt.name}>
                      {roleDisplayName(opt.name, opt.description)}
                    </option>
                  ))}
                </select>
                <button type="button" className="btn-success btn-sm" disabled={savingRole || !role} onClick={() => void saveRole()}>
                  {savingRole ? '保存中…' : '保存角色'}
                </button>
              </div>
              {roleErr ? <p className="error" style={{ marginTop: '0.4rem' }}>{roleErr}</p> : null}
            </label>
          ) : iAmSuper && targetIsSuper ? (
            <p className="muted" style={{ marginTop: '0.5rem' }}>超级管理员的角色不能修改。</p>
          ) : null}
        </div>
      </div>

      <div className="panel" style={{ marginBottom: '1rem' }}>
        <div className="panel-header">
          <div>
            <h2>授权工作站</h2>
            <p>来自 workstation_users 成员关系</p>
          </div>
        </div>
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>工作站</th>
                <th>工作站 ID</th>
                <th>站内角色</th>
              </tr>
            </thead>
            <tbody>
              {(d.workstations || []).map((w) => (
                <tr key={w.workstation_id}>
                  <td>{w.name || w.workstation_id}</td>
                  <td className="muted">{w.workstation_id}</td>
                  <td>{stationRoleLabel(w.role)}</td>
                </tr>
              ))}
              {!d.workstations?.length ? (
                <tr><td colSpan={3} className="empty-tip">尚未授权任何工作站</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>名下数字员工</h2>
            <p>owner_user_id 归属本用户</p>
          </div>
        </div>
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>名称</th>
                <th>ID</th>
              </tr>
            </thead>
            <tbody>
              {(d.employees || []).map((e) => (
                <tr key={e.id}>
                  <td><Link to={`/employees/${e.id}`}>{e.name}</Link></td>
                  <td className="muted">{e.id}</td>
                </tr>
              ))}
              {!d.employees?.length ? (
                <tr><td colSpan={2} className="empty-tip">暂无数字员工</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
