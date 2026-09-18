/**
 * Employee 详情：对齐设计文档 §10，聚合 overview + 可编辑绑定。
 */
import { FormEvent, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { apiGet, apiPatch, apiPost } from '../api/client'

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
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  // 可编辑绑定字段
  const [provider, setProvider] = useState('')
  const [wsId, setWsId] = useState('')
  const [workspaceId, setWorkspaceId] = useState('')
  const [permProfile, setPermProfile] = useState('')

  async function load() {
    if (!id) return
    const data = await apiGet<Overview>(`/employees/${id}/overview`)
    setOv(data)
    const e = data.employee
    setProvider(e.default_provider || '')
    setWsId(e.workstation_id || '')
    setWorkspaceId(e.workspace_id || '')
    setPermProfile(e.permission_profile || '')
  }

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [id])

  async function saveBindings(ev: FormEvent) {
    ev.preventDefault()
    if (!id) return
    setError('')
    setMsg('')
    try {
      await apiPatch(`/employees/${id}`, {
        default_provider: provider,
        workstation_id: wsId,
        workspace_id: workspaceId,
        permission_profile: permProfile,
      })
      setMsg('绑定已保存')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
    }
  }

  async function disable() {
    if (!id) return
    setError('')
    try {
      await apiPost(`/employees/${id}/disable`)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : '停用失败')
    }
  }

  if (error && !ov) return <p className="error">{error}</p>
  if (!ov) return <p className="muted">加载中…</p>

  const emp = ov.employee
  const feishu = ov.feishu
  const skills = ov.skills ?? []
  const knowledge = ov.knowledge ?? []

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>{emp.name}</h1>
          <p className="muted">
            {emp.id} · <span className={`badge ${emp.status === 'DISABLED' ? 'badge-err' : 'badge-ok'}`}>{emp.status}</span>
          </p>
        </div>
        {emp.status !== 'DISABLED' ? (
          <button type="button" className="btn-danger" onClick={() => void disable()}>
            停用
          </button>
        ) : null}
      </header>
      {error ? <p className="error">{error}</p> : null}
      {msg ? <p className="ok-msg">{msg}</p> : null}

      <div className="detail-grid">
        <div className="block">
          <h2>基本信息</h2>
          <p className="muted">描述：{emp.description || '—'}</p>
          <p className="muted">角色：{emp.role_summary || '—'}</p>
        </div>

        <div className="block">
          <h2>Runtime / 绑定</h2>
          <form className="stack-form" onSubmit={saveBindings}>
            <label>
              Provider
              <input value={provider} onChange={(e) => setProvider(e.target.value)} placeholder="如 cursor" />
            </label>
            <label>
              Workstation ID
              <input value={wsId} onChange={(e) => setWsId(e.target.value)} placeholder="WS-…" />
            </label>
            <label>
              Workspace ID
              <input value={workspaceId} onChange={(e) => setWorkspaceId(e.target.value)} placeholder="WKS-…" />
            </label>
            <label>
              Permission Profile
              <input value={permProfile} onChange={(e) => setPermProfile(e.target.value)} placeholder="default" />
            </label>
            <button type="submit">保存绑定</button>
          </form>
        </div>

        <div className="block">
          <h2>Feishu</h2>
          {feishu ? (
            <pre>{`OpenID：${feishu.feishu_open_id || '—'}
Alias：${feishu.feishu_bot_alias || '—'}
Chat：${feishu.chat_id || '—'}`}</pre>
          ) : (
            <p className="muted">
              未绑定 · 前往 <Link to="/feishu">Feishu</Link> 配置
            </p>
          )}
        </div>

        <div className="block">
          <h2>Skills</h2>
          {skills.length === 0 ? (
            <p className="muted">
              无 · <Link to="/skills">管理 Skills</Link>
            </p>
          ) : (
            <ul className="compact-list">
              {skills.map((s) => (
                <li key={s.id}>
                  {s.name} <span className="muted">({s.category || s.id})</span>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="block">
          <h2>Knowledge</h2>
          {knowledge.length === 0 ? (
            <p className="muted">
              无 · <Link to="/knowledge">管理 Knowledge</Link>
            </p>
          ) : (
            <ul className="compact-list">
              {knowledge.map((k) => (
                <li key={k.id}>
                  {k.title} <span className="muted">{k.summary ? `— ${k.summary}` : ''}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      <div className="panel">
        <h2>Sessions</h2>
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Status</th>
              <th>Provider</th>
              <th>Workstation</th>
            </tr>
          </thead>
          <tbody>
            {(ov.sessions ?? []).length === 0 ? (
              <tr>
                <td colSpan={4} className="muted">
                  无 Session
                </td>
              </tr>
            ) : (
              (ov.sessions ?? []).map((s) => (
                <tr key={s.id}>
                  <td className="mono">{s.id}</td>
                  <td>
                    <span className="badge">{s.status}</span>
                  </td>
                  <td>{s.provider || '—'}</td>
                  <td>{s.workstation_id || '—'}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      <div className="panel">
        <h2>Jobs</h2>
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Status</th>
              <th>Prompt</th>
            </tr>
          </thead>
          <tbody>
            {(ov.jobs ?? []).length === 0 ? (
              <tr>
                <td colSpan={3} className="muted">
                  无 Job
                </td>
              </tr>
            ) : (
              (ov.jobs ?? []).slice(0, 20).map((j) => (
                <tr key={j.id}>
                  <td>
                    <Link to={`/jobs/${j.id}`}>{j.id}</Link>
                  </td>
                  <td>
                    <span className="badge">{j.status}</span>
                  </td>
                  <td className="mono">{j.prompt || '—'}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
