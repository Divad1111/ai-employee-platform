/**
 * 用户详情（§22）
 */
import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'

type Detail = {
  id: string
  username: string
  display_name: string
  email: string
  status: string
  roles: string[]
  workstations?: Array<{ workstation_id: string; role: string }>
  employees?: Array<{ id: string; name: string }>
}

export function UserDetailPage() {
  const { id } = useParams()
  const [d, setD] = useState<Detail | null>(null)
  const [err, setErr] = useState('')

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
            {d.username} · {(d.roles || []).join(', ') || '无角色'} ·{' '}
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
          <p>角色：{(d.roles || []).join(', ') || '—'}</p>
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
                <th>工作站 ID</th>
                <th>站内角色</th>
              </tr>
            </thead>
            <tbody>
              {(d.workstations || []).map((w) => (
                <tr key={w.workstation_id}>
                  <td>{w.workstation_id}</td>
                  <td>{w.role}</td>
                </tr>
              ))}
              {!d.workstations?.length ? (
                <tr><td colSpan={2} className="empty-tip">尚未授权任何工作站</td></tr>
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
