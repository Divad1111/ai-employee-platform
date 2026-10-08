/**
 * Employee 详情：对齐设计文档 §10，聚合 overview + 可编辑绑定。
 */
import { FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { apiDelete, apiGet, apiPatch, apiPost } from '../api/client'
import {
  grantEmployeeWorkflow,
  listWorkflows,
  revokeEmployeeWorkflow,
  type SkillPackage,
  type Workflow,
} from '../api/workflowmcp'
import {
  bindEmployeeMCP,
  listCredentials,
  listEmployeeMCPBindings,
  listMCPServers,
  unbindEmployeeMCP,
  updateEmployeeMCPBinding,
  type Credential,
  type EmployeeMCPBinding,
  type MCPServer,
} from '../api/mcp'
import { StatusBadge } from '../components/StatusBadge'
import { EntityName } from '../components/EntityName'
import { IconAlertTriangle } from '../components/Icons'
import { SearchableSelect } from '../components/SearchableSelect'

/** Provider 中文名在选项里按工作站上报动态生成。 */

type WsNode = {
  id: string
  name: string
  status: string
  providers?: string[]
  models?: Record<string, Array<{ id: string; label: string }>>
}

type Emp = {
  id: string
  name: string
  status: string
  description: string
  role_summary: string
  default_provider: string
  default_model: string
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
  const [workstations, setWorkstations] = useState<WsNode[]>([])
  const [workspaces, setWorkspaces] = useState<Array<{ id: string; name?: string; path?: string }>>([])
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  // 可编辑绑定字段
  const [provider, setProvider] = useState('')
  const [model, setModel] = useState('')
  const [modelsLoading, setModelsLoading] = useState(false)
  const modelsReq = useRef(0)
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

  // 多用户 MCP 绑定状态
  const [mcpBindings, setMcpBindings] = useState<EmployeeMCPBinding[]>([])
  const [mcpServers, setMcpServers] = useState<MCPServer[]>([])
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [selectedServerId, setSelectedServerId] = useState('')
  const [selectedCredId, setSelectedCredId] = useState('')
  const [mcpBindingSubmitting, setMcpBindingSubmitting] = useState(false)

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
  const providerOptions = useMemo(() => {
    const labels: Record<string, string> = {
      cursor: 'Cursor ACP',
      codex: 'Codex',
      antigravity: 'Antigravity',
    }
    const ws = workstations.find((w) => w.id === wsId)
    const installed = ws?.providers ?? []
    return installed.map((p) => ({
      value: p,
      label: labels[p] || p,
      keywords: p,
    }))
  }, [workstations, wsId])
  const defaultModelsByProvider: Record<string, Array<{ id: string; label: string }>> = {
    cursor: [
      { id: 'default[]', label: 'Auto (自动智能选择)' },
      { id: 'claude-sonnet-4-5', label: 'Claude Sonnet 4.5' },
      { id: 'claude-opus-4-5', label: 'Claude Opus 4.5' },
      { id: 'gpt-5', label: 'GPT-5' },
      { id: 'composer-2.5', label: 'Composer 2.5' },
      { id: 'gemini-3-flash', label: 'Gemini 3 Flash' },
      { id: 'kimi-k3', label: 'Kimi K3' },
    ],
    codex: [
      { id: 'gpt-5', label: 'GPT-5 (默认推荐)' },
      { id: 'gpt-4o', label: 'GPT-4o' },
      { id: 'o3-mini', label: 'o3-mini' },
    ],
    antigravity: [
      { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro (推荐)' },
      { id: 'gemini-2.5-flash', label: 'Gemini 2.5 Flash' },
      { id: 'gemini-2.0-flash', label: 'Gemini 2.0 Flash' },
      { id: 'claude-3-7-sonnet', label: 'Claude 3.7 Sonnet' },
    ],
  }

  const modelOptions = useMemo(() => {
    const ws = workstations.find((w) => w.id === wsId)
    const list = ws?.models?.[provider] ?? []
    if (list.length > 0) {
      return list.map((m) => ({
        value: m.id,
        label: m.label && m.label !== m.id ? `${m.label} (${m.id})` : m.id,
        keywords: `${m.id} ${m.label || ''}`,
      }))
    }
    const defaults = defaultModelsByProvider[provider] ?? []
    return defaults.map((m) => ({
      value: m.id,
      label: `${m.label} (未上报时推荐)`,
      keywords: `${m.id} ${m.label}`,
    }))
  }, [workstations, wsId, provider])
  async function refreshModelsFromWorkstation() {
    if (!wsId || !provider) return
    const seq = ++modelsReq.current
    setModelsLoading(true)
    setError('')
    try {
      const data = await apiPost<{
        models?: WsNode['models']
        providers?: string[]
      }>(`/workstations/${encodeURIComponent(wsId)}/models/refresh`, {})
      if (seq !== modelsReq.current) return
      setWorkstations((prev) =>
        prev.map((w) =>
          w.id === wsId
            ? {
                ...w,
                models: data.models ?? w.models,
                providers: data.providers?.length ? data.providers : w.providers,
              }
            : w,
        ),
      )
    } catch (e) {
      if (seq === modelsReq.current) {
        setError(e instanceof Error ? e.message : '拉取模型列表失败')
      }
    } finally {
      if (seq === modelsReq.current) setModelsLoading(false)
    }
  }

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
  const unbondedServers = useMemo(() => {
    const boundIds = new Set(mcpBindings.map((b) => b.mcp_server_id))
    return mcpServers.filter((s) => !boundIds.has(s.id))
  }, [mcpServers, mcpBindings])

  const mcpServerOptions = useMemo(
    () =>
      unbondedServers.map((s) => ({
        value: s.id,
        label: `${s.name} (${s.id}) · ${s.server_type}`,
        keywords: `${s.id} ${s.name} ${s.server_type}`,
      })),
    [unbondedServers],
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
    const [data, wsData, wspData, wfAll, bindings, servers, creds] = await Promise.all([
      apiGet<Overview>(`/employees/${id}/overview`),
      apiGet<{ items: WsNode[] }>('/workstations').catch(() => ({ items: [] })),
      apiGet<{ items: Array<{ id: string; name?: string; path?: string }> }>('/workspaces').catch(() => ({ items: [] })),
      listWorkflows().catch(() => ({ items: [] as Workflow[] })),
      listEmployeeMCPBindings(id).catch(() => ({ items: [] as EmployeeMCPBinding[] })),
      listMCPServers().catch(() => ({ items: [] as MCPServer[] })),
      listCredentials().catch(() => ({ items: [] as Credential[] })),
    ])
    setOv(data)
    setWorkstations(wsData.items ?? [])
    setWorkspaces(wspData.items ?? [])
    setAllWorkflows(wfAll.items ?? [])
    setMcpBindings(bindings.items ?? [])
    setMcpServers(servers.items ?? [])
    setCredentials(creds.items ?? [])
    setProvider(data.employee.default_provider || '')
    setModel(data.employee.default_model || '')
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
        default_model: model,
        workstation_id: wsId,
        workspace_id: workspaceId,
      })
      setMsg('绑定关系保存成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function handleAddMcpBinding(ev: FormEvent) {
    ev.preventDefault()
    if (!id || !selectedServerId) return
    setMcpBindingSubmitting(true)
    setError('')
    try {
      await bindEmployeeMCP(id, {
        mcp_server_id: selectedServerId,
        credential_id: selectedCredId || undefined,
        enabled: true,
      })
      setSelectedServerId('')
      setSelectedCredId('')
      setMsg('成功绑定 MCP 服务扩展能力')
      const bRes = await listEmployeeMCPBindings(id)
      setMcpBindings(bRes.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : '绑定 MCP 失败')
    } finally {
      setMcpBindingSubmitting(false)
    }
  }

  async function handleToggleMcpBinding(binding: EmployeeMCPBinding) {
    if (!id) return
    setError('')
    try {
      await updateEmployeeMCPBinding(id, binding.id, {
        enabled: !binding.enabled,
      })
      const bRes = await listEmployeeMCPBindings(id)
      setMcpBindings(bRes.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新状态失败')
    }
  }

  async function handleChangeBindingCredential(bindingId: string, credentialId: string) {
    if (!id) return
    setError('')
    try {
      await updateEmployeeMCPBinding(id, bindingId, {
        credential_id: credentialId || undefined,
      })
      setMsg('凭证绑定更新成功')
      const bRes = await listEmployeeMCPBindings(id)
      setMcpBindings(bRes.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新凭证失败')
    }
  }

  async function handleUnbindMcp(binding: EmployeeMCPBinding) {
    if (!id) return
    if (binding.mcp_server_id === 'mcp-workflow') {
      if (!confirm('确定要解绑内置 workflow-mcp 吗？解绑后该员工将无法调度执行平台预置的任何工作流与技能包。')) {
        return
      }
    }
    setError('')
    try {
      await unbindEmployeeMCP(id, binding.id)
      setMsg('已解绑 MCP 服务')
      const bRes = await listEmployeeMCPBindings(id)
      setMcpBindings(bRes.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : '解绑失败')
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
            <p>先选工作站和驱动引擎。打开模型列表时会向该工作站要一份最新模型</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onSaveBindings}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', fontSize: '0.82rem', fontWeight: 600 }}>
            绑定工作站 (Workstation ID)
            <SearchableSelect
              value={wsId}
              onChange={(id) => {
                setWsId(id)
                const ws = workstations.find((w) => w.id === id)
                const installed = ws?.providers ?? []
                const nextProvider = provider && installed.includes(provider) ? provider : ''
                if (nextProvider !== provider) setProvider(nextProvider)
                const models = ws?.models?.[nextProvider] ?? []
                if (model && !models.some((m) => m.id === model)) setModel('')
              }}
              options={wsNodeOptions}
              placeholder="选择或搜索工作站…"
              allowCustom
              style={{ minWidth: 260 }}
            />
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', fontSize: '0.82rem', fontWeight: 600 }}>
            驱动引擎 (Provider)
            <SearchableSelect
              value={provider}
              onChange={(id) => {
                setProvider(id)
                const ws = workstations.find((w) => w.id === wsId)
                const models = ws?.models?.[id] ?? []
                if (model && !models.some((m) => m.id === model)) setModel('')
              }}
              options={providerOptions}
              placeholder={
                !wsId
                  ? '请先选择工作站'
                  : providerOptions.length
                    ? '选择该工作站已安装的引擎…'
                    : '该工作站尚未上报已安装引擎'
              }
              disabled={!wsId}
              style={{ minWidth: 220 }}
            />
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', fontSize: '0.82rem', fontWeight: 600 }}>
            模型 (Model)
            <SearchableSelect
              value={model}
              onChange={setModel}
              options={modelOptions}
              placeholder={
                !provider
                  ? '请先选择驱动引擎'
                  : modelsLoading
                    ? '正在向工作站拉取模型列表…'
                    : '选择模型或输入自定义模型名称…'
              }
              allowCustom
              disabled={!provider || modelsLoading}
              onOpen={() => void refreshModelsFromWorkstation()}
              style={{ minWidth: 280 }}
            />
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', fontSize: '0.82rem', fontWeight: 600 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span>绑定工作区</span>
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
              placeholder="选择已登记的项目工作区…"
              style={{ minWidth: 240 }}
            />
            {workspaceId && !workspaceOptions.some((o) => o.value === workspaceId) ? (
              <span style={{ fontWeight: 500, color: 'var(--warning, #b45309)' }}>
                当前值「{workspaceId}」还不是已登记工作区。已选择工作站时，再次保存会按该本机路径登记，并出现在「项目工作区」。
              </span>
            ) : null}
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

      {/* 多用户 MCP 扩展能力与身份凭证绑定 */}
      <div className="panel" style={{ marginTop: '1rem' }}>
        <div className="panel-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
              <h2>MCP 扩展能力与身份凭证绑定 (Multi-User MCP)</h2>
              <span className="badge badge-ok" style={{ fontSize: '0.75rem' }}>能力与身份解耦</span>
            </div>
            <p style={{ marginTop: '0.25rem', fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
              MCP 服务提供工具能力，身份凭证（Credential）确定执行身份。平台在任务调度时按此配置动态解密并注入 MCP 配置。
            </p>
          </div>
          <Link to="/mcp-servers" className="btn-ghost btn-sm" style={{ whiteSpace: 'nowrap' }}>
            前往 MCP 服务与凭证保管库 →
          </Link>
        </div>

        {/* 添加绑定表单 */}
        {unbondedServers.length > 0 ? (
          <form
            className="inline-form"
            onSubmit={handleAddMcpBinding}
            style={{
              marginTop: '0.85rem',
              padding: '0.9rem 1.1rem',
              background: '#f8fafc',
              borderRadius: '10px',
              border: '1px solid #e2e8f0',
              display: 'flex',
              gap: '1rem',
              flexWrap: 'wrap',
              alignItems: 'flex-end',
            }}
          >
            <label style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem', fontSize: '0.84rem', fontWeight: 600, color: 'var(--text-secondary)' }}>
              选择要挂载的 MCP 服务
              <SearchableSelect
                value={selectedServerId}
                onChange={setSelectedServerId}
                options={mcpServerOptions}
                placeholder="选择未挂载的 MCP 服务…"
                style={{ minWidth: 280, height: 38 }}
              />
            </label>
            <label style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem', fontSize: '0.84rem', fontWeight: 600, color: 'var(--text-secondary)' }}>
              选择执行身份凭证 (可选)
              <select
                value={selectedCredId}
                onChange={(e) => setSelectedCredId(e.target.value)}
                style={{ minWidth: 240, height: 38, borderRadius: 8 }}
              >
                <option value="">（无凭证 / 免鉴权访问）</option>
                {credentials.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.credential_name} ({c.provider} · {c.auth_type.toUpperCase()})
                  </option>
                ))}
              </select>
            </label>
            <button
              type="submit"
              className="btn-primary"
              disabled={!selectedServerId || mcpBindingSubmitting}
              style={{ height: 38, padding: '0 1.25rem' }}
            >
              {mcpBindingSubmitting ? '正在绑定...' : '＋ 绑定 MCP 能力'}
            </button>
          </form>
        ) : (
          <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
            ✓ 所有已注册的 MCP 服务均已绑定到此员工。如需新增 MCP，请前往 <Link to="/mcp-servers">MCP 服务管理</Link>。
          </div>
        )}

        {/* 已绑定列表 */}
        <div className="table-wrapper" style={{ marginTop: '1rem' }}>
          <table className="table">
            <thead>
              <tr>
                <th style={{ minWidth: 220 }}>MCP 服务名称</th>
                <th style={{ width: 170 }}>服务类型 / 协议</th>
                <th style={{ minWidth: 230 }}>绑定身份凭证 (Credential)</th>
                <th style={{ width: 110 }}>运行状态</th>
                <th style={{ width: 160, textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {mcpBindings.length > 0 ? (
                mcpBindings.map((b) => {
                  const server = mcpServers.find((s) => s.id === b.mcp_server_id)
                  const isBuiltinWorkflow = b.mcp_server_id === 'mcp-workflow'
                  return (
                    <tr key={b.id}>
                      <td>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                          <div
                            style={{
                              width: 32,
                              height: 32,
                              borderRadius: 8,
                              background: isBuiltinWorkflow
                                ? 'linear-gradient(135deg, #10b981 0%, #059669 100%)'
                                : '#eff6ff',
                              color: isBuiltinWorkflow ? '#ffffff' : '#2563eb',
                              border: isBuiltinWorkflow ? 'none' : '1px solid #bfdbfe',
                              display: 'flex',
                              alignItems: 'center',
                              justifyContent: 'center',
                              flexShrink: 0,
                            }}
                          >
                            {isBuiltinWorkflow ? '⚡' : '🔌'}
                          </div>
                          <div>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                              <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>
                                {server?.name || b.mcp_server_name || b.mcp_server_id}
                              </span>
                              {isBuiltinWorkflow && (
                                <span className="badge badge-ok" style={{ fontSize: '0.7rem', padding: '0.1rem 0.35rem' }}>
                                  系统内置
                                </span>
                              )}
                            </div>
                            <div className="mono" style={{ fontSize: '0.74rem', color: 'var(--text-muted)', marginTop: 2 }}>
                              ID: {b.mcp_server_id}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                          <div>
                            <span
                              className="badge badge-neutral"
                              style={{ textTransform: 'uppercase', fontSize: '0.72rem', padding: '0.1rem 0.4rem', fontWeight: 600 }}
                            >
                              {server?.transport || 'http'}
                            </span>
                          </div>
                          <span style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                            {server?.server_type === 'builtin' ? '核心基础服务' : '自定义扩展能力'}
                          </span>
                        </div>
                      </td>
                      <td>
                        <select
                          value={b.credential_id || ''}
                          onChange={(e) => void handleChangeBindingCredential(b.id, e.target.value)}
                          style={{
                            fontSize: '0.84rem',
                            padding: '0.35rem 0.65rem',
                            minWidth: 200,
                            maxWidth: 260,
                            height: 34,
                            borderRadius: 6,
                          }}
                        >
                          <option value="">（无凭证 / 免鉴权访问）</option>
                          {credentials.map((c) => (
                            <option key={c.id} value={c.id}>
                              {c.credential_name} ({c.provider})
                            </option>
                          ))}
                        </select>
                      </td>
                      <td>
                        <button
                          type="button"
                          className={b.enabled ? 'badge badge-ok' : 'badge badge-neutral'}
                          style={{ cursor: 'pointer', border: '1px solid transparent', padding: '0.25rem 0.6rem' }}
                          onClick={() => void handleToggleMcpBinding(b)}
                          title="点击切换启用状态"
                        >
                          {b.enabled ? '✓ 已启用' : '已停用'}
                        </button>
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        <div style={{ display: 'inline-flex', gap: 6, justifyContent: 'flex-end', alignItems: 'center' }}>
                          {isBuiltinWorkflow && (
                            <Link to="/mcp-servers/workflow-mcp" className="btn-ghost btn-sm">
                              工作流配置 →
                            </Link>
                          )}
                          <button
                            type="button"
                            className="btn-ghost btn-sm"
                            style={{ color: '#dc2626', borderColor: '#fecaca' }}
                            onClick={() => void handleUnbindMcp(b)}
                          >
                            解绑
                          </button>
                        </div>
                      </td>
                    </tr>
                  )
                })
              ) : (
                <tr>
                  <td colSpan={5} className="empty-tip">暂无挂载任何 MCP 服务</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div className="panel" style={{ marginTop: '1rem' }}>
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

      <div className="panel" style={{ marginTop: '1rem' }}>
        <div className="panel-header">
          <div>
            <h2>飞书协同绑定状态</h2>
            <p>{feishu ? '该员工已绑定飞书呼叫别名' : '尚未绑定飞书，绑定后可在飞书里通过别名呼叫该员工'}</p>
          </div>
          {feishu ? null : (
            <Link className="btn btn-sm" to={`/feishu?employee=${encodeURIComponent(e.id)}#feishu-bindings`}>
              绑定飞书
            </Link>
          )}
        </div>
        {feishu ? (
          <p style={{ margin: '0 1.1rem 1rem', fontSize: '0.88rem' }}>
            机器人别名: <strong>@{feishu.feishu_bot_alias}</strong> · 飞书 OpenID: <span className="mono">{feishu.feishu_open_id || '—'}</span>
          </p>
        ) : null}
      </div>

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
