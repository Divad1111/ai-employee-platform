/**
 * 工作站详情：创建者可设置公用，并调整可使用的角色与用户。
 */
import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { apiGet, apiPut } from '../api/client'
import { roleDisplayName } from '../lib/rbacLabels'
import { StatusBadge } from '../components/StatusBadge'
import { usePerm } from '../stores/permissions'
import { getInfrastructure, type Host } from '../api/infrastructure'
import { IconFolder, IconPlus, IconTrash } from '../components/Icons'
import './WorkstationDetailPage.css'

type WS = {
  id: string
  name: string
  status: string
  workspace_roots?: string[]
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
  const { canAll } = usePerm()
  const [infraHost, setInfraHost] = useState<Host | null>(null)
  useEffect(() => {
    let active = true
    if (id && canAll('infra.read')) {
      void getInfrastructure().then((v) => {
        if (active) setInfraHost(v.hosts.find((h) => h.workstation_id === id) || null)
      }).catch(() => { if (active) setInfraHost(null) })
    }
    return () => { active = false }
  }, [id, canAll])
  const [ws, setWs] = useState<WS | null>(null)
  const [sharing, setSharing] = useState<Sharing | null>(null)
  const [roots, setRoots] = useState<string[]>([''])
  const [rootsSaving, setRootsSaving] = useState(false)
  const [rootsError, setRootsError] = useState('')
  const [rootsMessage, setRootsMessage] = useState('')
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
    setRoots(node.workspace_roots?.length ? node.workspace_roots : [''])
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

  async function saveRoots(event: FormEvent) {
    event.preventDefault()
    if (!id || !ws) return
    setRootsSaving(true)
    setRootsError('')
    setRootsMessage('')
    const nextRoots = roots.map(root => root.trim()).filter(Boolean)
    try {
      await apiPut(`/workstations/${id}/workspace-roots`, { roots: nextRoots })
      setWs({ ...ws, workspace_roots: nextRoots })
      setRoots(nextRoots.length ? nextRoots : [''])
      setRootsMessage('根目录已保存')
    } catch (error) {
      setRootsError(error instanceof Error ? error.message : String(error))
    } finally {
      setRootsSaving(false)
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
    <div className="page workstation-detail">
      <Link to="/workstations" className="workstation-back">← 返回工作站列表</Link>
      <header className="page-header workstation-heading">
        <div>
          <h1>{ws.name || ws.id}</h1>
          <p className="workstation-id">{ws.id}</p>
          {infraHost && <Link to={`/pve/hosts/${infraHost.id}`}>管理关联主机 · {infraHost.name} / 软件部署 / SSH</Link>}
        </div>
        <StatusBadge status={ws.status} />
      </header>

      {err ? <p role="alert" className="error">{err}</p> : null}
      {msg ? <p role="status" className="muted">{msg}</p> : null}
      <section className="panel workspace-roots-panel" aria-labelledby="workspace-roots-title">
        <div className="panel-header">
          <div>
            <h2 id="workspace-roots-title">工作区根目录</h2>
            <p>用户目录由系统按平台登录名创建，项目工作区位于用户目录内。</p>
          </div>
        </div>
        {sharing.can_manage || canAll('workstation.write') ? (
          <form className="workspace-roots-form" onSubmit={saveRoots}>
            <div className="workspace-roots-label">工作站本机路径</div>
            <div className="workspace-root-list">
              {roots.map((root, index) => (
                <div className="workspace-root-row" key={index}>
                  <IconFolder size={18} className="workspace-root-icon" aria-hidden="true" />
                  <input
                    aria-label={`根目录 ${index + 1}`}
                    aria-describedby="workspace-root-path-help"
                    autoComplete="off"
                    spellCheck={false}
                    placeholder="例如 D:\AIProjects 或 /srv/ai-projects"
                    value={root}
                    disabled={rootsSaving}
                    onChange={event => {
                      setRoots(roots.map((value, row) => row === index ? event.target.value : value))
                      setRootsMessage('')
                    }}
                  />
                  <button type="button" className="btn-ghost workspace-root-remove"
                    aria-label={`移除根目录 ${index + 1}`} title="移除此目录"
                    disabled={rootsSaving}
                    onClick={() => {
                      setRoots(roots.length === 1 ? [''] : roots.filter((_, row) => row !== index))
                      setRootsMessage('')
                    }}>
                    <IconTrash size={16} />
                  </button>
                </div>
              ))}
            </div>
            <p className="workspace-root-help" id="workspace-root-path-help">填写绝对路径，每行一个目录。添加的目录将在绑定工作区时供用户选择。</p>
            {rootsError && <p role="alert" className="error">{rootsError}</p>}
            <div className="workspace-roots-footer">
              <button type="button" className="btn-ghost" disabled={rootsSaving}
                onClick={() => { setRoots([...roots, '']); setRootsMessage('') }}>
                <IconPlus size={16} />添加目录
              </button>
              <div className="workspace-roots-save">
                {rootsMessage && <span role="status">{rootsMessage}</span>}
                <button disabled={rootsSaving}>{rootsSaving ? '保存中…' : '保存根目录'}</button>
              </div>
            </div>
          </form>
        ) : <div className="workspace-root-list">{ws.workspace_roots?.length
          ? ws.workspace_roots.map(root => <div className="workspace-root-readonly" key={root}><IconFolder size={18} /><code>{root}</code></div>)
          : <p className="muted">尚未配置根目录</p>}</div>}
      </section>
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
