/**
 * 工作站详情：创建者可设置公用，并调整可使用的角色与用户。
 */
import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { apiGet, apiPut } from '../api/client'
import { roleDisplayName } from '../lib/rbacLabels'
import { StatusBadge } from '../components/StatusBadge'

type WS = {
  id: string
  name: string
  status: string
  cert_status?: string
}

type Sharing = {
  workstation_id: string
  is_public: boolean
  grant_roles?: string[]
  created_by_user_id?: string
  can_manage: boolean
  members?: Array<{ user_id: string; display_name?: string; username?: string; role: string }>
  users?: Array<{ id: string; username: string; display_name?: string }>
}

type RoleItem = { name: string; description: string }

export function WorkstationDetailPage() {
  const { id } = useParams()
  const [ws, setWs] = useState<WS | null>(null)
  const [sharing, setSharing] = useState<Sharing | null>(null)
  const [roles, setRoles] = useState<RoleItem[]>([])
  const [isPublic, setIsPublic] = useState(false)
  const [grantRoles, setGrantRoles] = useState<string[]>([])
  const [memberIDs, setMemberIDs] = useState<string[]>([])
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [saving, setSaving] = useState(false)

  const load = async () => {
    if (!id) return
    const [node, share, roleRes] = await Promise.all([
      apiGet<WS>(`/workstations/${id}`),
      apiGet<Sharing>(`/workstations/${id}/sharing`),
      apiGet<{ items: RoleItem[] }>('/roles').catch(() => ({ items: [] as RoleItem[] })),
    ])
    setWs(node)
    setSharing(share)
    setIsPublic(!!share.is_public)
    setGrantRoles(share.grant_roles || [])
    const creator = share.created_by_user_id || ''
    setMemberIDs((share.members || []).map((m) => m.user_id).filter((uid) => uid && uid !== creator))
    setRoles((roleRes.items || []).filter((r) => r.name !== 'SUPER_ADMIN'))
  }

  useEffect(() => {
    void load().catch((e: unknown) => setErr(e instanceof Error ? e.message : String(e)))
  }, [id])

  function toggleRole(name: string) {
    setGrantRoles((prev) => (prev.includes(name) ? prev.filter((r) => r !== name) : [...prev, name]))
  }

  function toggleUser(userID: string) {
    setMemberIDs((prev) => (prev.includes(userID) ? prev.filter((x) => x !== userID) : [...prev, userID]))
  }

  async function save() {
    if (!id || !sharing?.can_manage) return
    if (isPublic && grantRoles.length === 0) {
      setErr('公用工作站请至少选择一个默认授权角色')
      return
    }
    setSaving(true)
    setErr('')
    setMsg('')
    try {
      const next = await apiPut<Sharing>(`/workstations/${id}/sharing`, {
        public: isPublic,
        grant_roles: isPublic ? grantRoles : [],
        member_user_ids: memberIDs,
      })
      setSharing(next)
      setMsg('授权已保存')
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  if (err && !ws) return <div className="page"><div className="error">{err}</div></div>
  if (!ws || !sharing) return <div className="page"><p className="muted">加载中…</p></div>

  const roleList = roles.length > 0 ? roles : [
    { name: 'ADMIN', description: '管理员' },
    { name: 'OPERATOR', description: '操作员' },
    { name: 'USER', description: '普通用户' },
    { name: 'VIEWER', description: '只读' },
  ]
  const creator = sharing.created_by_user_id || ''

  return (
    <div className="page">
      <div className="page-header">
        <Link to="/workstations" className="muted" style={{ fontSize: '0.85rem' }}>← 返回工作站列表</Link>
        <h1 style={{ marginTop: '0.35rem' }}>{ws.name || ws.id}</h1>
        <p style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <StatusBadge status={ws.status} />
          <span className="muted">{ws.id}</span>
        </p>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>使用授权</h2>
            <p>{sharing.can_manage ? '你是创建者，可以设置公用并调整角色和用户' : '只有创建者可以调整授权'}</p>
          </div>
        </div>
        <div style={{ padding: '0 1.1rem 1.1rem' }}>
          {!sharing.can_manage ? (
            <div>
              <p>公用工作站：{sharing.is_public ? '是' : '否'}</p>
              <p>默认授权角色：{(sharing.grant_roles || []).map((r) => roleDisplayName(r)).join('、') || '—'}</p>
              <p>已授权用户：</p>
              <ul>
                {(sharing.members || []).map((m) => (
                  <li key={m.user_id}>{m.display_name || m.username || m.user_id}（{m.role === 'OWNER' ? '创建者' : '成员'}）</li>
                ))}
              </ul>
            </div>
          ) : (
            <div>
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
                <input type="checkbox" checked={isPublic} onChange={(e) => setIsPublic(e.target.checked)} />
                设为公用工作站
              </label>
              {isPublic ? (
                <div style={{ marginTop: '0.8rem' }}>
                  <div style={{ fontWeight: 600, marginBottom: '0.4rem' }}>默认可使用的角色</div>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.6rem 1rem' }}>
                    {roleList.map((r) => (
                      <label key={r.name} style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                        <input type="checkbox" checked={grantRoles.includes(r.name)} onChange={() => toggleRole(r.name)} />
                        {roleDisplayName(r.name, r.description)}
                      </label>
                    ))}
                  </div>
                </div>
              ) : null}
              <div style={{ marginTop: '1rem' }}>
                <div style={{ fontWeight: 600, marginBottom: '0.4rem' }}>可使用的用户</div>
                <p className="muted" style={{ marginTop: 0 }}>创建者始终保留所有者权限。</p>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                  {(sharing.users || []).filter((u) => u.id !== creator).map((u) => (
                    <label key={u.id} style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                      <input type="checkbox" checked={memberIDs.includes(u.id)} onChange={() => toggleUser(u.id)} />
                      {u.display_name || u.username} <span className="muted">{u.username}</span>
                    </label>
                  ))}
                </div>
              </div>
              {err ? <p className="error">{err}</p> : null}
              {msg ? <p className="muted">{msg}</p> : null}
              <button type="button" className="btn-success btn-sm" style={{ marginTop: '0.8rem' }} disabled={saving} onClick={() => void save()}>
                {saving ? '保存中…' : '保存授权'}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
