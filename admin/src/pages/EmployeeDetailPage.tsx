/**
 * Employee 详情：对齐设计文档 §10，聚合 overview + 可编辑绑定。
 */
import { FormEvent, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { apiGet, apiPatch, apiPost } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { EntityName } from '../components/EntityName'

type Emp = {
  id: string
  name: string
  status: string
  description: string
  role_summary: string
  default_provider: string
  workstation_id: string
  workspace_id: string
  permission_profile: string
}

type Session = {
  id: string
  status: string
  provider: string
  workstation_id: string
  last_activity_at?: string
}

type Job = {
  id: string
  status: string
  prompt: string
}

type Skill = { id: string; name: string; category: string }
type Knowledge = { id: string; title: string; summary: string }
type FeishuBinding = {
  employee_id: string
  feishu_open_id: string
  feishu_bot_alias: string
  chat_id: string
}

type Overview = {
  employee: Emp
  sessions: Session[]
  jobs: Job[]
  skills: Skill[] | null
  knowledge: Knowledge[] | null
  feishu: FeishuBinding | null
}

export function EmployeeDetailPage() {
  const { id } = useParams()
  const [ov, setOv] = useState<Overview | null>(null)
  const [workstations, setWorkstations] = useState<Array<{ id: string; name: string; status: string }>>([])
  const [workspaces, setWorkspaces] = useState<Array<{ id: string; name?: string; path?: string }>>([])
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  // 可编辑绑定字段
  const [provider, setProvider] = useState('')
  const [wsId, setWsId] = useState('')
  const [workspaceId, setWorkspaceId] = useState('')

  // 快速新建工作区
  const [showCreateWs, setShowCreateWs] = useState(false)
  const [newWsPath, setNewWsPath] = useState('')
  const [newWsRepo, setNewWsRepo] = useState('')
  const [newWsBranch, setNewWsBranch] = useState('main')
  const [wsCreating, setWsCreating] = useState(false)

  async function onCreateWsQuick(ev: FormEvent) {
    ev.preventDefault()
    if (!newWsPath.trim()) return
    if (!wsId.trim()) {
      setError('请先在上方选择工作站节点，再创建该节点上的工作区路径')
      return
    }
    setWsCreating(true)
    setError('')
    try {
      const res = await apiPost<{ id: string }>('/workspaces', {
        workstation_id: wsId.trim(),
        path: newWsPath.trim(),
        repository: newWsRepo.trim(),
        branch: newWsBranch.trim() || 'main',
        employee_id: id,
      })
      setWorkspaceId(res.id)
      setNewWsPath('')
      setNewWsRepo('')
      setShowCreateWs(false)
      setMsg(`工作区创建成功 (${res.id}) 并已选中，请点击「保存配置」完成绑定`)
      // 重新加载工作区列表
      const wspData = await apiGet<{ items: Array<{ id: string; name?: string; path?: string }> }>('/workspaces').catch(() => ({ items: [] }))
      setWorkspaces(wspData.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建工作区失败')
    } finally {
      setWsCreating(false)
    }
  }

  async function load() {
    if (!id) return
    const [data, wsData, wspData] = await Promise.all([
      apiGet<Overview>(`/employees/${id}/overview`),
      apiGet<{ items: Array<{ id: string; name: string; status: string }> }>('/workstations').catch(() => ({ items: [] })),
      apiGet<{ items: Array<{ id: string; name?: string; path?: string }> }>('/workspaces').catch(() => ({ items: [] })),
    ])
    setOv(data)
    setWorkstations(wsData.items ?? [])
    setWorkspaces(wspData.items ?? [])
    setProvider(data.employee.default_provider || '')
    setWsId(data.employee.workstation_id || '')
    setWorkspaceId(data.employee.workspace_id || '')
  }

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [id])

  async function onSaveBindings(e: FormEvent) {
    e.preventDefault()
    if (!id) return
    setError('')
    setMsg('')
    try {
      await apiPatch(`/employees/${id}`, {
        default_provider: provider,
        workstation_id: wsId,
        workspace_id: workspaceId,
      })
      setMsg('绑定关系保存成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function onStartSession() {
    if (!id) return
    setError('')
    setMsg('')
    try {
      await apiPost('/sessions', {
        employee_id: id,
        workstation_id: wsId || undefined,
        provider: provider || undefined,
      })
      setMsg('会话启动指令已下发')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '启动会话失败')
    }
  }

  if (!ov) {
    return (
      <section>
        <div className="empty-tip">正在加载员工档案信息...</div>
      </section>
    )
  }

  const { employee: e, sessions, jobs, skills, knowledge, feishu } = ov

  return (
    <section>
      <header className="page-header">
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <Link to="/employees" style={{ fontSize: '0.88rem' }}>← 返回员工列表</Link>
            <span style={{ color: 'var(--text-muted)' }}>/</span>
            <EntityName name={e.name} id={e.id} />
          </div>
          <h1 style={{ marginTop: '0.4rem', display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <span>{e.name}</span>
            <StatusBadge status={e.status} />
          </h1>
          <p>{e.description || e.role_summary || '暂无职责说明'}</p>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button type="button" onClick={() => void onStartSession()}>
            启动专属会话
          </button>
        </div>
      </header>

      {error ? <div className="error">{error}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>核心配置与资源绑定</h2>
            <p>指定驱动 Provider、运行工作站节点与本地代码工作区</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onSaveBindings}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', fontSize: '0.82rem', fontWeight: 600 }}>
            驱动引擎 (Provider)
            <select value={provider} onChange={(ev) => setProvider(ev.target.value)}>
              <option value="">未指定 (继承工作站默认)</option>
              <option value="cursor">Cursor (ACP 协议)</option>
              <option value="codex">Codex (本地 Runtime)</option>
            </select>
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', fontSize: '0.82rem', fontWeight: 600 }}>
            绑定工作站 (Workstation ID)
            <input
              list="detail-ws-options"
              placeholder="选择已有工作站或输入 ID"
              value={wsId}
              onChange={(ev) => setWsId(ev.target.value)}
              style={{ width: '280px' }}
            />
            <datalist id="detail-ws-options">
              {workstations.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.name && w.name !== w.id ? `${w.name} (${w.id})` : w.id} [{w.status}]
                </option>
              ))}
            </datalist>
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', fontSize: '0.82rem', fontWeight: 600 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span>绑定工作区 (Workspace ID)</span>
              <button
                type="button"
                className="btn-ghost btn-sm"
                style={{ padding: '0.1rem 0.4rem', fontSize: '0.72rem', height: 'auto' }}
                onClick={() => setShowCreateWs((prev) => !prev)}
              >
                {showCreateWs ? '✕ 取消新建' : '➕ 快速新建'}
              </button>
            </div>
            <input
              list="detail-workspace-options"
              placeholder="选择已有工作区或输入 ID"
              value={workspaceId}
              onChange={(ev) => setWorkspaceId(ev.target.value)}
              style={{ width: '260px' }}
            />
            <datalist id="detail-workspace-options">
              {workspaces.map((ws) => (
                <option key={ws.id} value={ws.id}>
                  {ws.path ? `${ws.path} (${ws.id})` : ws.id}
                </option>
              ))}
            </datalist>
          </label>
          <button type="submit" style={{ alignSelf: 'flex-end' }}>保存配置</button>
        </form>

        {showCreateWs && (
          <div style={{ marginTop: '1rem', padding: '1rem', background: '#f8fafc', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)' }}>
            <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', color: 'var(--brand-700)' }}>快速新建项目工作区</h4>
            <p style={{ margin: '0 0 0.75rem', fontSize: '0.82rem', color: 'var(--text-muted)' }}>
              将登记到上方已选工作站节点上的本机路径{wsId ? `（${wsId}）` : '；请先选择工作站'}
            </p>
            <form onSubmit={onCreateWsQuick} style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
              <input
                placeholder="该工作站本机代码工程绝对路径 (必填)"
                value={newWsPath}
                onChange={(e) => setNewWsPath(e.target.value)}
                style={{ flex: 2, minWidth: '260px' }}
                required
                disabled={!wsId}
              />
              <input
                placeholder="Git 仓库名 (可选)"
                value={newWsRepo}
                onChange={(e) => setNewWsRepo(e.target.value)}
                style={{ flex: 1, minWidth: '150px' }}
              />
              <input
                placeholder="分支 (默认 main)"
                value={newWsBranch}
                onChange={(e) => setNewWsBranch(e.target.value)}
                style={{ width: '100px' }}
              />
              <button type="submit" className="btn btn-sm" disabled={wsCreating}>
                {wsCreating ? '创建中...' : '确认创建并选中'}
              </button>
            </form>
          </div>
        )}
      </div>

      <div className="detail-grid">
        <div className="panel" style={{ margin: 0 }}>
          <h2>运行会话历史 (Sessions)</h2>
          <div className="table-wrapper" style={{ marginTop: '0.75rem' }}>
            <table className="table">
              <thead>
                <tr>
                  <th>会话标识</th>
                  <th>状态</th>
                  <th>驱动</th>
                </tr>
              </thead>
              <tbody>
                {sessions && sessions.length > 0 ? (
                  sessions.map((s) => (
                    <tr key={s.id}>
                      <td>
                        <EntityName name={`会话 #${s.id.slice(0, 8)}`} id={s.id} />
                      </td>
                      <td><StatusBadge status={s.status} /></td>
                      <td>{s.provider || '—'}</td>
                    </tr>
                  ))
                ) : (
                  <tr><td colSpan={3} className="empty-tip">暂无会话历史</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </div>

        <div className="panel" style={{ margin: 0 }}>
          <h2>关联任务记录 (Jobs)</h2>
          <div className="table-wrapper" style={{ marginTop: '0.75rem' }}>
            <table className="table">
              <thead>
                <tr>
                  <th>任务需求描述</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {jobs && jobs.length > 0 ? (
                  jobs.map((j) => (
                    <tr key={j.id}>
                      <td style={{ maxWidth: '260px' }}>
                        <EntityName
                          name={j.prompt || `任务 #${j.id.slice(0, 8)}`}
                          id={j.id}
                          to={`/jobs/${j.id}`}
                        />
                      </td>
                      <td><StatusBadge status={j.status} /></td>
                      <td>
                        <Link to={`/jobs/${j.id}`} className="btn-ghost btn-sm">
                          时间线 →
                        </Link>
                      </td>
                    </tr>
                  ))
                ) : (
                  <tr><td colSpan={3} className="empty-tip">暂无任务记录</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <div className="detail-grid" style={{ marginTop: '1rem' }}>
        <div className="panel" style={{ margin: 0 }}>
          <h2>赋能技能列表 (Skills)</h2>
          {skills && skills.length > 0 ? (
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.75rem' }}>
              {skills.map((sk) => (
                <span key={sk.id} className="badge badge-ok">
                  {sk.name} ({sk.category || '通用'})
                </span>
              ))}
            </div>
          ) : (
            <p className="empty-tip">未关联专门技能库</p>
          )}
        </div>

        <div className="panel" style={{ margin: 0 }}>
          <h2>知识库文档 (Knowledge)</h2>
          {knowledge && knowledge.length > 0 ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem', marginTop: '0.75rem' }}>
              {knowledge.map((k) => (
                <div key={k.id} style={{ fontSize: '0.85rem' }}>
                  <strong>{k.title}</strong>: <span style={{ color: 'var(--text-muted)' }}>{k.summary}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="empty-tip">未绑定知识库文档</p>
          )}
        </div>
      </div>

      {feishu ? (
        <div className="panel" style={{ marginTop: '1rem' }}>
          <h2>飞书协同绑定状态</h2>
          <p style={{ margin: '0.5rem 0', fontSize: '0.88rem' }}>
            机器人别名: <strong>@{feishu.feishu_bot_alias}</strong> · 飞书 OpenID: <span className="mono">{feishu.feishu_open_id}</span>
          </p>
        </div>
      ) : null}
    </section>
  )
}
