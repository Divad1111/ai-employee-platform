/**
 * 用户详情（§22）
 */
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { apiGet, apiPatch, apiPost } from '../api/client'
import { roleDisplayName } from '../lib/rbacLabels'
import { usePerm } from '../stores/permissions'
import { clearSession } from '../stores/session'
import './UserDetailPage.css'

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

export function UserDetailPage({ self = false }: { self?: boolean }) {
  const { id: routeId } = useParams()
  const navigate = useNavigate()
  const { user, roles: myRoles, canAll } = usePerm()
  const id = self ? user?.id : routeId
  const iAmSuper = myRoles.includes('SUPER_ADMIN')
  const [d, setD] = useState<Detail | null>(null)
  const [err, setErr] = useState('')
  const [roles, setRoles] = useState<RoleItem[]>([])
  const [role, setRole] = useState('')
  const [savingRole, setSavingRole] = useState(false)
  const [roleErr, setRoleErr] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [savingPassword, setSavingPassword] = useState(false)
  const [passwordError, setPasswordError] = useState('')
  const [passwordMessage, setPasswordMessage] = useState('')

  const load = async () => {
    if (!id) return
    try {
      setD(await apiGet<Detail>(`/users/${id}`))
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  useEffect(() => {
    setD(null)
    setErr('')
    setPassword('')
    setConfirmPassword('')
    setPasswordError('')
    setPasswordMessage('')
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
  const isSelf = !!id && user?.id === id
  const canResetPassword = d?.id === id && (isSelf || canAll('user.update')) && (!targetIsSuper || iAmSuper)
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

  async function resetPassword(event: FormEvent) {
    event.preventDefault()
    if (!d || !canResetPassword || savingPassword) return
    setPasswordError('')
    setPasswordMessage('')
    if (password !== confirmPassword) {
      setPasswordError('两次输入的密码不一致')
      return
    }
    setSavingPassword(true)
    try {
      await apiPatch(`/users/${d.id}`, { password })
      setPassword('')
      setConfirmPassword('')
      if (isSelf) {
        clearSession()
        navigate('/login', { replace: true, state: { message: '密码已更新，请使用新密码重新登录' } })
      } else {
        setPasswordMessage('密码已重置，该用户需使用新密码重新登录')
      }
    } catch (error) {
      setPasswordError(error instanceof Error ? error.message : '重置密码失败')
    } finally {
      setSavingPassword(false)
    }
  }

  if (err) return <div className="page"><div className="error">{err}</div></div>
  if (!d) return <div className="page"><p className="muted">加载中…</p></div>

  return (
    <div className="page">
      <div className="page-header" style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem' }}>
        <div>
          <Link to={canAll('user.read') ? '/users' : '/'} className="muted" style={{ fontSize: '0.85rem' }}>
            {canAll('user.read') ? '← 返回用户列表' : '← 返回首页'}
          </Link>
          <h1 style={{ marginTop: '0.35rem' }}>{self ? '个人设置' : d.display_name || d.username}</h1>
          <p>
            {d.username} · {(d.roles || []).map((r) => roleDisplayName(r)).join('、') || '无角色'} ·{' '}
            {d.status === 'active' ? '启用' : d.status}
          </p>
        </div>
        {canAll('user.disable') && (!targetIsSuper || iAmSuper) && <button
          type="button"
          className={d.status === 'disabled' ? 'btn-success btn-sm' : 'btn-warn btn-sm'}
          onClick={() => void toggle()}
        >
          {d.status === 'disabled' ? '启用用户' : '禁用用户'}
        </button>}
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

      {canResetPassword && <section className="panel" aria-labelledby="reset-password-title">
        <div className="panel-header">
          <div>
            <h2 id="reset-password-title">{isSelf ? '修改密码' : '重置密码'}</h2>
            <p>更新后，该账号的现有登录会话失效，需要使用新密码重新登录。</p>
          </div>
        </div>
        <form className="user-password-form" onSubmit={resetPassword}>
          <div className="user-password-fields">
            <label htmlFor="user-new-password">新密码
              <input id="user-new-password" type="password" autoComplete="new-password"
                placeholder="至少 8 位" required minLength={8} maxLength={72}
                value={password} disabled={savingPassword} onChange={event => setPassword(event.target.value)} />
            </label>
            <label htmlFor="user-confirm-password">确认新密码
              <input id="user-confirm-password" type="password" autoComplete="new-password"
                placeholder="再次输入新密码" required minLength={8} maxLength={72}
                value={confirmPassword} disabled={savingPassword} onChange={event => setConfirmPassword(event.target.value)} />
            </label>
          </div>
          {passwordError && <p className="error" role="alert">{passwordError}</p>}
          {passwordMessage && <p className="ok-msg" role="status">{passwordMessage}</p>}
          <button disabled={savingPassword} type="submit">{savingPassword ? '保存中…' : isSelf ? '修改密码' : '重置密码'}</button>
        </form>
      </section>}

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
