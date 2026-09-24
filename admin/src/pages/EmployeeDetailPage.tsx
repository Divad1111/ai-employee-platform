/**
 * Employee 详情：对齐设计文档 §10，聚合 overview + 可编辑绑定。
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { apiDelete, apiGet, apiPatch, apiPost } from '../api/client'
import {
  grantEmployeeWorkflow,
  issueMCPToken,
  listMCPTokens,
  listWorkflows,
  revokeEmployeeWorkflow,
  revokeMCPToken,
  type MCPToken,
  type SkillPackage,
  type Workflow,
} from '../api/workflowmcp'
import { StatusBadge } from '../components/StatusBadge'
import { EntityName } from '../components/EntityName'
import { IconAlertTriangle } from '../components/Icons'
import { SearchableSelect } from '../components/SearchableSelect'

/** Provider 中文选项 */
const PROVIDER_OPTIONS = [
  { value: '', label: '未指定', keywords: 'inherit default' },
  { value: 'cursor', label: 'Cursor ACP', keywords: 'cursor acp' },
  { value: 'codex', label: 'Codex', keywords: 'codex runtime' },
]

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
  workflows: Workflow[] | null
  effective_skills: SkillPackage[] | null
  feishu: FeishuBinding | null
}

export function EmployeeDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [ov, setOv] = useState<Overview | null>(null)
  const [workstations, setWorkstations] = useState<Array<{ id: string; name: string; status: string }>>([])
  const [workspaces, setWorkspaces] = useState<Array<{ id: string; name?: string; path?: string }>>([])
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  // 可编辑绑定字段
  const [provider, setProvider] = useState('')
  const [wsId, setWsId] = useState('')
  const [workspaceId, setWorkspaceId] = useState('')

  // 删除相关状态
  const [showDeleteModal, setShowDeleteModal] = useState(false)
  const [adminPassword, setAdminPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [totpEnabled, setTotpEnabled] = useState(false)
  const [showManualTotp, setShowManualTotp] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [modalError, setModalError] = useState('')
  const [allWorkflows, setAllWorkflows] = useState<Workflow[]>([])
  const [grantWfId, setGrantWfId] = useState('')
  const [mcpTokens, setMcpTokens] = useState<MCPToken[]>([])
  const [lastSecret, setLastSecret] = useState('')

  const checkTotpStatus = () => {
    void apiGet<{ enabled: boolean }>('/auth/totp')
      .then((res) => setTotpEnabled(!!res.enabled))
      .catch(() => setTotpEnabled(false))
  }

  useEffect(() => {
    checkTotpStatus()
  }, [])

  // 快速新建工作区
  const [showCreateWs, setShowCreateWs] = useState(false)
  const [newWsPath, setNewWsPath] = useState('')
  const [newWsRepo, setNewWsRepo] = useState('')
  const [newWsBranch, setNewWsBranch] = useState('main')
  const [wsCreating, setWsCreating] = useState(false)

  const wsNodeOptions = useMemo(
    () =>
      workstations.map((w) => ({
        value: w.id,
        label: w.name && w.name !== w.id ? `${w.name} [${w.status}]` : `${w.id} [${w.status}]`,
        keywords: `${w.id} ${w.name || ''} ${w.status}`,
      })),
    [workstations],
  )
  const workspaceOptions = useMemo(
    () =>
      workspaces.map((ws) => ({
        value: ws.id,
        label: ws.path || ws.name || ws.id,
        keywords: `${ws.id} ${ws.path || ''} ${ws.name || ''}`,
      })),
    [workspaces],
  )
  const workflowOptions = useMemo(
    () =>
      allWorkflows.map((w) => ({
        value: w.id,
        label: w.name,
        keywords: w.id,
      })),
    [allWorkflows],
  )

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
    const [data, wsData, wspData, wfAll, tokens] = await Promise.all([
      apiGet<Overview>(`/employees/${id}/overview`),
      apiGet<{ items: Array<{ id: string; name: string; status: string }> }>('/workstations').catch(() => ({ items: [] })),
      apiGet<{ items: Array<{ id: string; name?: string; path?: string }> }>('/workspaces').catch(() => ({ items: [] })),
      listWorkflows().catch(() => ({ items: [] as Workflow[] })),
      listMCPTokens(id).catch(() => ({ items: [] as MCPToken[] })),
    ])
    setOv(data)
    setWorkstations(wsData.items ?? [])
    setWorkspaces(wspData.items ?? [])
    setAllWorkflows(wfAll.items ?? [])
    setMcpTokens(tokens.items ?? [])
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

  async function confirmDelete() {
    if (!id) return
    setDeleting(true)
    setModalError('')
    try {
      if (adminPassword.trim()) {
        await apiPost('/auth/step-up', {
          password: adminPassword.trim(),
          totp: totpCode.trim() || undefined,
        })
      }
      await apiDelete(`/employees/${id}`)
      navigate('/employees')
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : '删除失败'
      if (errMsg.includes('TOTP') || errMsg.includes('动态验证码') || errMsg.includes('动态口令') || errMsg.includes('双因子')) {
        setTotpEnabled(true)
        setShowManualTotp(true)
        setModalError(errMsg)
      } else if (errMsg.includes('step-up') || errMsg.includes('二次认证')) {
        setModalError('此操作属于高危操作，请输入管理员登录密码进行二次身份核验')
      } else {
        setModalError(errMsg)
      }
    } finally {
      setDeleting(false)
    }
  }

  if (!ov) {
    return (
      <section>
        <div className="empty-tip">正在加载员工档案信息...</div>
      </section>
    )
  }

  const { employee: e, sessions, jobs, workflows, effective_skills, feishu } = ov

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
          <button
            type="button"
            className="btn-danger"
            onClick={() => {
              setShowDeleteModal(true)
              setAdminPassword('')
              setTotpCode('')
              setModalError('')
              setShowManualTotp(false)
              checkTotpStatus()
            }}
          >
            删除员工
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
            <SearchableSelect
              value={provider}
              onChange={setProvider}
              options={PROVIDER_OPTIONS}
              placeholder="选择驱动引擎…"
              style={{ minWidth: 180 }}
            />
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', fontSize: '0.82rem', fontWeight: 600 }}>
            绑定工作站 (Workstation ID)
            <SearchableSelect
              value={wsId}
              onChange={setWsId}
              options={wsNodeOptions}
              placeholder="选择或搜索工作站…"
              allowCustom
              style={{ minWidth: 260 }}
            />
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
            <SearchableSelect
              value={workspaceId}
              onChange={setWorkspaceId}
              options={workspaceOptions}
              placeholder="选择或搜索工作区…"
              allowCustom
              style={{ minWidth: 240 }}
            />
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
          <h2>已授权工作流</h2>
          <form
            className="inline-form"
            style={{ marginTop: '0.75rem' }}
            onSubmit={(ev) => {
              ev.preventDefault()
              if (!id || !grantWfId) return
              void grantEmployeeWorkflow(id, grantWfId)
                .then(() => load())
                .then(() => setMsg('已授权工作流'))
                .catch((err) => setError(err instanceof Error ? err.message : String(err)))
            }}
          >
            <SearchableSelect
              value={grantWfId}
              onChange={setGrantWfId}
              options={workflowOptions}
              placeholder="选择工作流…"
              style={{ minWidth: 220 }}
            />
            <button type="submit">授权</button>
          </form>
          {workflows && workflows.length > 0 ? (
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.75rem' }}>
              {workflows.map((w) => (
                <span key={w.id} className="badge badge-ok" style={{ display: 'inline-flex', gap: '0.35rem', alignItems: 'center' }}>
                  {w.name}
                  <button
                    type="button"
                    className="btn-ghost btn-sm"
                    style={{ padding: '0 0.25rem' }}
                    onClick={() => {
                      if (!id) return
                      void revokeEmployeeWorkflow(id, w.id).then(() => load()).catch((err) => setError(String(err)))
                    }}
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          ) : (
            <p className="empty-tip">尚未授权任何工作流</p>
          )}
          <h3 style={{ marginTop: '1rem' }}>因此可访问的技能包</h3>
          {effective_skills && effective_skills.length > 0 ? (
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.5rem' }}>
              {effective_skills.map((sk) => (
                <span key={sk.id} className="badge">{sk.name || sk.id} @{sk.cursor_name}</span>
              ))}
            </div>
          ) : (
            <p className="empty-tip">无（随工作流引用自动开放）</p>
          )}
        </div>

        <div className="panel" style={{ margin: 0 }}>
          <h2>MCP Token</h2>
          <button
            type="button"
            className="btn-sm"
            style={{ marginTop: '0.5rem' }}
            onClick={() => {
              if (!id) return
              void issueMCPToken(id, 'admin-issued')
                .then((res) => {
                  setLastSecret(res.secret)
                  setMsg('已签发 MCP Token（明文仅显示一次）')
                  return listMCPTokens(id)
                })
                .then((t) => setMcpTokens(t.items ?? []))
                .catch((err) => setError(err instanceof Error ? err.message : String(err)))
            }}
          >
            签发只读 Token
          </button>
          {lastSecret && (
            <pre style={{ marginTop: '0.75rem', fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
              {JSON.stringify({ url: '/mcp', headers: { Authorization: `Bearer ${lastSecret}` } }, null, 2)}
            </pre>
          )}
          <ul style={{ marginTop: '0.75rem', paddingLeft: '1.1rem' }}>
            {mcpTokens.map((t) => (
              <li key={t.id} style={{ fontSize: '0.85rem', marginBottom: '0.35rem' }}>
                {t.id} · {t.scope} · {t.label || '—'}
                {!t.revoked_at && (
                  <button
                    type="button"
                    className="btn-danger btn-sm"
                    style={{ marginLeft: '0.5rem' }}
                    onClick={() => void revokeMCPToken(t.id).then(() => load()).catch((err) => setError(String(err)))}
                  >
                    吊销
                  </button>
                )}
              </li>
            ))}
          </ul>
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

      {/* 删除数字员工危险操作二次确认弹窗 */}
      {showDeleteModal ? (
        <div
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(15, 23, 42, 0.65)',
            backdropFilter: 'blur(4px)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1000,
          }}
        >
          <div
            style={{
              background: '#ffffff',
              borderRadius: '12px',
              padding: '1.75rem',
              width: '480px',
              maxWidth: '92vw',
              boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1)',
              border: '1px solid #fee2e2',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', marginBottom: '1rem' }}>
              <IconAlertTriangle size={22} style={{ color: '#ef4444' }} />
              <h3 style={{ margin: 0, fontSize: '1.15rem', color: '#b91c1c' }}>
                安全告警：删除数字员工
              </h3>
            </div>

            <p style={{ fontSize: '0.86rem', color: 'var(--text-secondary)', lineHeight: 1.6, margin: '0 0 1rem' }}>
              删除数字员工将永久移除该员工的档案记录与技能/知识库绑定，已关联的项目工作区将被自动解绑。<strong>若该员工当前有正在执行中的任务，系统将拒绝删除以保护任务连续性。</strong>
            </p>

            <div
              style={{
                background: '#fef2f2',
                border: '1px solid #fecaca',
                borderRadius: '8px',
                padding: '0.75rem 1rem',
                marginBottom: '1rem',
                fontSize: '0.85rem',
              }}
            >
              <div style={{ color: '#991b1b', marginBottom: '0.25rem' }}>
                <strong>待删除员工：</strong>
                <span style={{ color: '#b91c1c', fontWeight: 700 }}>{e.name}</span>
              </div>
              <div style={{ color: '#7f1d1d', fontSize: '0.78rem', fontFamily: 'monospace' }}>
                员工 ID: {e.id}
              </div>
            </div>

            {modalError ? (
              <div
                style={{
                  background: '#fef2f2',
                  border: '1px solid #fca5a5',
                  color: '#dc2626',
                  padding: '0.65rem 0.85rem',
                  borderRadius: '6px',
                  fontSize: '0.85rem',
                  fontWeight: 600,
                  marginBottom: '1rem',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.5rem',
                }}
              >
                <IconAlertTriangle size={18} style={{ color: '#dc2626', flexShrink: 0 }} />
                <span>{modalError}</span>
              </div>
            ) : null}

            <div style={{ marginBottom: '1.25rem' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.4rem' }}>
                <label
                  style={{
                    fontSize: '0.84rem',
                    fontWeight: 600,
                    color: 'var(--text-primary)',
                  }}
                >
                  管理员登录密码 (高危操作二次认证)
                </label>
                {!totpEnabled && !showManualTotp && (
                  <button
                    type="button"
                    className="btn-ghost btn-sm"
                    style={{ padding: 0, fontSize: '0.78rem', color: 'var(--brand-600)', height: 'auto', border: 'none', background: 'none', cursor: 'pointer' }}
                    onClick={() => setShowManualTotp(true)}
                  >
                    + 输入 TOTP 动态码
                  </button>
                )}
              </div>
              <input
                autoFocus
                type="password"
                placeholder="请输入管理员密码以确认删除 (默认 admin123)"
                value={adminPassword}
                onChange={(ev) => setAdminPassword(ev.target.value)}
                style={{ width: '100%', boxSizing: 'border-box' }}
                onKeyDown={(ev) => {
                  if (ev.key === 'Enter') void confirmDelete()
                }}
              />
              <span style={{ display: 'block', fontSize: '0.75rem', color: 'var(--text-muted)', marginTop: '0.35rem' }}>
                若最近 10 分钟内已完成过二次认证提权，可直接点击确定删除。
              </span>
            </div>

            {(totpEnabled || showManualTotp) ? (
              <div style={{ marginBottom: '1.25rem' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.4rem' }}>
                  <label
                    style={{
                      fontSize: '0.84rem',
                      fontWeight: 600,
                      color: 'var(--text-primary)',
                    }}
                  >
                    TOTP 6 位动态验证码 {totpEnabled ? '(该账号已启用双因子认证)' : '(双因子动态口令)'}
                  </label>
                  {!totpEnabled && showManualTotp && (
                    <button
                      type="button"
                      className="btn-ghost btn-sm"
                      style={{ padding: 0, fontSize: '0.75rem', color: 'var(--text-muted)', height: 'auto', border: 'none', background: 'none', cursor: 'pointer' }}
                      onClick={() => { setShowManualTotp(false); setTotpCode('') }}
                    >
                      收起
                    </button>
                  )}
                </div>
                <input
                  type="text"
                  placeholder="输入 Authenticator 中的 6 位口令 (如 123456)"
                  value={totpCode}
                  onChange={(ev) => setTotpCode(ev.target.value.trim())}
                  maxLength={6}
                  style={{
                    width: '100%',
                    boxSizing: 'border-box',
                    fontSize: '1.05rem',
                    textAlign: 'center',
                    letterSpacing: '3px',
                    fontWeight: 600,
                  }}
                  onKeyDown={(ev) => {
                    if (ev.key === 'Enter') void confirmDelete()
                  }}
                />
              </div>
            ) : null}

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.6rem' }}>
              <button
                type="button"
                className="btn-ghost"
                onClick={() => setShowDeleteModal(false)}
                disabled={deleting}
              >
                取消
              </button>
              <button
                type="button"
                className="btn-danger"
                onClick={() => void confirmDelete()}
                disabled={deleting}
              >
                {deleting ? '正在安全删除...' : '确定删除'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
