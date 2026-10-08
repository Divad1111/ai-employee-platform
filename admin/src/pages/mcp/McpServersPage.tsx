import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet } from '../../api/client'
import { getUser } from '../../stores/session'
import { usePerm } from '../../stores/permissions'
import {
  createCredential,
  createMCPServer,
  deleteCredential,
  deleteMCPServer,
  listCredentials,
  listMCPServers,
  updateCredential,
  updateMCPServer,
  type Credential,
  type MCPServer,
} from '../../api/mcp'
import { PageFeatureGuide } from '../../components/PageFeatureGuide'
import { StatusBadge } from '../../components/StatusBadge'
import {
  IconKey,
  IconPlug,
  IconPlus,
  IconRefresh,
  IconSettings,
  IconTrash,
  IconZap,
} from '../../components/Icons'

type Tab = 'servers' | 'credentials'

type SimpleUser = {
  id: string
  username: string
  display_name?: string
}

export function McpServersPage() {
  const { canAll } = usePerm()
  const canWriteServer = canAll('workflow.write')
  const canDeleteServer = canAll('workflow.delete')
  const [tab, setTab] = useState<Tab>('servers')
  const [servers, setServers] = useState<MCPServer[]>([])
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [users, setUsers] = useState<SimpleUser[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')

  // Server Modal
  const [showServerModal, setShowServerModal] = useState(false)
  const [editingServer, setEditingServer] = useState<MCPServer | null>(null)
  const [serverName, setServerName] = useState('')
  const [serverDesc, setServerDesc] = useState('')
  const [serverTransport, setServerTransport] = useState<'http' | 'sse' | 'stdio'>('http')
  const [serverEndpoint, setServerEndpoint] = useState('')
  const [serverHeaders, setServerHeaders] = useState('')

  // Credential Modal
  const [showCredModal, setShowCredModal] = useState(false)
  const [editingCred, setEditingCred] = useState<Credential | null>(null)
  const [credName, setCredName] = useState('')
  const [credOwnerType, setCredOwnerType] = useState<'USER' | 'ORGANIZATION' | 'SERVICE_ACCOUNT'>('USER')
  const [credOwnerId, setCredOwnerId] = useState('')
  const [credProvider, setCredProvider] = useState('github')
  const [credAuthType, setCredAuthType] = useState('pat')
  const [credSecret, setCredSecret] = useState('')

  const userMap = useMemo(() => {
    const map: Record<string, string> = {}
    for (const u of users) {
      map[u.id] = u.display_name ? `${u.display_name} (${u.username})` : u.username
    }
    return map
  }, [users])

  async function loadData() {
    setLoading(true)
    setError('')
    try {
      const [sRes, cRes, uRes] = await Promise.all([
        listMCPServers(),
        listCredentials(),
        apiGet<{ items: SimpleUser[] }>('/users').catch(() => ({ items: [] })),
      ])
      setServers(sRes.items || [])
      setCredentials(cRes.items || [])
      setUsers(uRes.items || [])
    } catch (e: any) {
      setError(e.message || String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadData()
  }, [])

  function openCreateServer() {
    setEditingServer(null)
    setServerName('')
    setServerDesc('')
    setServerTransport('http')
    setServerEndpoint('')
    setServerHeaders('')
    setShowServerModal(true)
  }

  function openEditServer(s: MCPServer) {
    setEditingServer(s)
    setServerName(s.name)
    setServerDesc(s.description || '')
    setServerTransport(s.transport || 'http')
    setServerEndpoint(s.endpoint || '')
    const h = s.config?.headers ? JSON.stringify(s.config.headers, null, 2) : ''
    setServerHeaders(h)
    setShowServerModal(true)
  }

  async function handleSaveServer(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    let headersObj: Record<string, string> | undefined
    if (serverHeaders.trim()) {
      try {
        headersObj = JSON.parse(serverHeaders)
      } catch {
        setError('自定义请求头 JSON 格式无效')
        return
      }
    }
    try {
      if (editingServer) {
        await updateMCPServer(editingServer.id, {
          name: serverName,
          description: serverDesc,
          transport: serverTransport,
          endpoint: serverEndpoint,
          config: headersObj ? { headers: headersObj } : {},
        })
        setMsg(`MCP 服务 [${serverName}] 更新成功`)
      } else {
        await createMCPServer({
          name: serverName,
          description: serverDesc,
          transport: serverTransport,
          endpoint: serverEndpoint,
          config: headersObj ? { headers: headersObj } : {},
        })
        setMsg(`MCP 服务 [${serverName}] 创建成功`)
      }
      setShowServerModal(false)
      await loadData()
    } catch (err: any) {
      setError(err.message || String(err))
    }
  }

  async function handleDeleteServer(id: string, name: string) {
    if (!confirm(`确定删除 MCP 服务 [${name}] 吗？`)) return
    setError('')
    try {
      await deleteMCPServer(id)
      setMsg(`MCP 服务 [${name}] 已删除`)
      await loadData()
    } catch (err: any) {
      setError(err.message || String(err))
    }
  }

  function openCreateCred() {
    const currentUser = getUser()
    setEditingCred(null)
    setCredName('')
    setCredOwnerType('USER')
    setCredOwnerId(currentUser?.id || (users[0]?.id ?? ''))
    setCredProvider('github')
    setCredAuthType('pat')
    setCredSecret('')
    setShowCredModal(true)
  }

  function openEditCred(c: Credential) {
    setEditingCred(c)
    setCredName(c.credential_name)
    setCredOwnerType(c.owner_type)
    setCredOwnerId(c.owner_id || '')
    setCredProvider(c.provider)
    setCredAuthType(c.auth_type)
    setCredSecret('')
    setShowCredModal(true)
  }

  async function handleSaveCred(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    const currentUser = getUser()
    let effectiveOwnerId = credOwnerId.trim()
    if (credOwnerType === 'USER' && !effectiveOwnerId) {
      effectiveOwnerId = currentUser?.id || users[0]?.id || ''
    }
    if (credOwnerType === 'ORGANIZATION') {
      effectiveOwnerId = 'ORGANIZATION'
    }
    try {
      if (editingCred) {
        await updateCredential(editingCred.id, {
          credential_name: credName,
          owner_type: credOwnerType,
          owner_id: effectiveOwnerId,
          provider: credProvider,
          auth_type: credAuthType,
          secret_value: credSecret || undefined,
        })
        setMsg(`凭证 [${credName}] 更新成功`)
      } else {
        if (!credSecret.trim()) {
          setError('新建凭证必须填写密钥/Token')
          return
        }
        await createCredential({
          credential_name: credName,
          owner_type: credOwnerType,
          owner_id: effectiveOwnerId,
          provider: credProvider,
          auth_type: credAuthType,
          secret_value: credSecret,
        })
        setMsg(`凭证 [${credName}] 创建成功`)
      }
      setShowCredModal(false)
      await loadData()
    } catch (err: any) {
      setError(err.message || String(err))
    }
  }

  async function handleDeleteCred(id: string, name: string) {
    if (!confirm(`确定删除身份凭证 [${name}] 吗？`)) return
    setError('')
    try {
      await deleteCredential(id)
      setMsg(`凭证 [${name}] 已安全销毁`)
      await loadData()
    } catch (err: any) {
      setError(err.message || String(err))
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <h1>MCP 服务与能力扩展管理 (MCP Management)</h1>
          </div>
          <p>
            统一管理数字员工可调用的外部能力提供方、系统内置工作流与安全凭证库
          </p>
        </div>
        <div style={{ display: 'flex', gap: 10 }}>
          <button type="button" className="btn-ghost" onClick={() => loadData()} disabled={loading}>
            <IconRefresh size={15} />
            <span>刷新</span>
          </button>
          {tab === 'servers' ? (
            canWriteServer ? (
            <button type="button" className="btn-primary" onClick={openCreateServer}>
              <IconPlus size={15} />
              <span>添加 MCP 服务</span>
            </button>
            ) : null
          ) : (
            <button type="button" className="btn-primary" onClick={openCreateCred}>
              <IconPlus size={15} />
              <span>新建身份凭证</span>
            </button>
          )}
        </div>
      </header>

      <PageFeatureGuide
        title="多用户、多员工与 MCP 身份解耦架构"
        summary="遵循严格的身份隔离原则：数字员工拥有独立工作区，MCP Server 提供扩展能力，Credential 保障以特定身份安全调用。"
        steps={[
          {
            step: '1',
            title: '内置与自定义 MCP',
            desc: 'workflow-mcp 作为系统内置能力，统一提供 SOP 流程、技能与知识库；自定义 MCP 可接入 GitHub、Jira、GitLab 等任意外部协议端。',
            tag: '能力解耦',
          },
          {
            step: '2',
            title: '身份凭证安全存储',
            desc: '所有 Token / OAuth 凭据经 Vault 安全隔离加密，仅在向工作站分发短期会话时解密注入，普通日志恒为脱敏。',
            tag: '凭证隔离',
          },
          {
            step: '3',
            title: '员工专属绑定',
            desc: '通过 Employee MCP Binding 建立员工与能力、凭证的绑定，支持工具白名单/黑名单控制，实现最小权限治理。',
            tag: '权限闭环',
          },
        ]}
      />

      {error ? <div className="banner banner-err">{error}</div> : null}
      {msg ? <div className="banner banner-ok">{msg}</div> : null}

      {/* Tabs */}
      <div className="tab-nav">
        <button
          type="button"
          className={`tab-btn ${tab === 'servers' ? 'active' : ''}`}
          onClick={() => setTab('servers')}
        >
          <IconPlug size={16} />
          <span>MCP 服务列表</span>
          <span className="tab-count">{servers.length}</span>
        </button>
        <button
          type="button"
          className={`tab-btn ${tab === 'credentials' ? 'active' : ''}`}
          onClick={() => setTab('credentials')}
        >
          <IconKey size={16} />
          <span>身份凭证保管库</span>
          <span className="tab-count">{credentials.length}</span>
        </button>
      </div>

      {tab === 'servers' ? (
        <div className="panel">
          <div className="panel-header">
            <div>
              <h2>已注册的 MCP 服务</h2>
              <p>数字员工在执行任务时，可按需动态加载并调用这些服务暴露的工具接口</p>
            </div>
          </div>

          <div className="table-wrapper">
            <table className="table">
              <thead>
                <tr>
                  <th style={{ minWidth: 220 }}>服务名称与标识</th>
                  <th style={{ width: 140 }}>服务类型</th>
                  <th style={{ minWidth: 240 }}>通信协议与端点</th>
                  <th style={{ minWidth: 220 }}>能力描述说明</th>
                  <th style={{ width: 110, textAlign: 'center' }}>绑定员工</th>
                  <th style={{ width: 100 }}>服务状态</th>
                  <th style={{ width: 190, minWidth: 190, textAlign: 'right' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {servers.map((s) => {
                  const isBuiltin = s.server_type === 'builtin' || s.id === 'mcp-workflow'
                  return (
                    <tr key={s.id}>
                      <td>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                          <div
                            style={{
                              width: 34,
                              height: 34,
                              borderRadius: 8,
                              background: isBuiltin
                                ? 'linear-gradient(135deg, #10b981 0%, #059669 100%)'
                                : '#eff6ff',
                              color: isBuiltin ? '#ffffff' : '#2563eb',
                              border: isBuiltin ? 'none' : '1px solid #bfdbfe',
                              display: 'flex',
                              alignItems: 'center',
                              justifyContent: 'center',
                              flexShrink: 0,
                              boxShadow: isBuiltin ? '0 2px 4px rgba(16, 185, 129, 0.25)' : 'none',
                            }}
                          >
                            {isBuiltin ? <IconZap size={17} /> : <IconPlug size={17} />}
                          </div>
                          <div>
                            <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: '0.92rem' }}>
                              {s.name}
                            </div>
                            <div className="mono" style={{ fontSize: '0.74rem', color: 'var(--text-muted)', marginTop: 2 }}>
                              ID: {s.id}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td>
                        {isBuiltin ? (
                          <span className="badge badge-ok" style={{ fontWeight: 600 }}>
                            系统内置 Builtin
                          </span>
                        ) : (
                          <span className="badge badge-info">自定义 Custom</span>
                        )}
                      </td>
                      <td>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                          <div>
                            <span
                              className="badge badge-neutral"
                              style={{ textTransform: 'uppercase', fontSize: '0.72rem', padding: '0.1rem 0.45rem', fontWeight: 600 }}
                            >
                              {s.transport}
                            </span>
                          </div>
                          <div style={{ wordBreak: 'break-all' }}>
                            <code style={{ fontSize: '0.78rem' }}>{s.endpoint || '—'}</code>
                          </div>
                        </div>
                      </td>
                      <td>
                        <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)', lineHeight: 1.45, maxWidth: 320 }}>
                          {s.description || '暂无描述说明'}
                        </div>
                      </td>
                      <td style={{ textAlign: 'center' }}>
                        <span className="badge badge-neutral" style={{ fontWeight: 600 }}>
                          {s.bound_employee_count ?? 0} 人
                        </span>
                      </td>
                      <td>
                        <StatusBadge status={s.status} />
                      </td>
                      <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                        <div style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'flex-end', gap: 6, whiteSpace: 'nowrap' }}>
                          {isBuiltin ? (
                            <Link
                              to="/mcp-servers/workflow-mcp"
                              className="btn-primary btn-sm"
                              style={{ display: 'inline-flex', alignItems: 'center', gap: 5, textDecoration: 'none', whiteSpace: 'nowrap', flexShrink: 0 }}
                            >
                              <IconSettings size={14} />
                              <span style={{ whiteSpace: 'nowrap' }}>进入配置与详情</span>
                            </Link>
                          ) : canWriteServer || canDeleteServer ? (
                            <>
                              {canWriteServer ? (
                              <button
                                type="button"
                                className="btn-ghost btn-sm"
                                onClick={() => openEditServer(s)}
                                style={{ whiteSpace: 'nowrap' }}
                              >
                                编辑
                              </button>
                              ) : null}
                              {canDeleteServer ? (
                              <button
                                type="button"
                                className="btn-ghost btn-sm"
                                style={{ color: '#dc2626', borderColor: '#fecaca', whiteSpace: 'nowrap' }}
                                onClick={() => handleDeleteServer(s.id, s.name)}
                                title="删除此 MCP 服务"
                              >
                                <IconTrash size={14} />
                              </button>
                              ) : null}
                            </>
                          ) : null}
                        </div>
                      </td>
                    </tr>
                  )
                })}
                {servers.length === 0 ? (
                  <tr>
                    <td colSpan={7} style={{ textAlign: 'center', padding: '2.5rem', color: 'var(--text-muted)' }}>
                      暂无已注册的 MCP 服务，点击右上角「添加 MCP 服务」注册
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </div>
      ) : (
        <div className="panel">
          <div className="panel-header">
            <div>
              <h2>身份凭证保管库 (Credentials)</h2>
              <p>维护访问外部系统（如 GitHub、Jira、飞书）的安全凭证，支持个人/组织共享</p>
            </div>
          </div>

          <div className="table-wrapper">
            <table className="table">
              <thead>
                <tr>
                  <th style={{ minWidth: 220 }}>凭证名称与标识</th>
                  <th style={{ width: 130 }}>提供方 (Provider)</th>
                  <th style={{ width: 180, minWidth: 180 }}>所有者 / 归属</th>
                  <th style={{ width: 120 }}>认证方式</th>
                  <th style={{ minWidth: 170 }}>脱敏凭证敏感值</th>
                  <th style={{ width: 100, textAlign: 'center' }}>绑定员工</th>
                  <th style={{ width: 90 }}>状态</th>
                  <th style={{ width: 130, textAlign: 'right' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {credentials.map((c) => (
                  <tr key={c.id}>
                    <td>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                        <div
                          style={{
                            width: 34,
                            height: 34,
                            borderRadius: 8,
                            background: '#fef3c7',
                            color: '#b45309',
                            border: '1px solid #fde68a',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            flexShrink: 0,
                          }}
                        >
                          <IconKey size={17} />
                        </div>
                        <div>
                          <div style={{ fontWeight: 600, color: 'var(--text-primary)', fontSize: '0.92rem' }}>
                            {c.credential_name}
                          </div>
                          <div className="mono" style={{ fontSize: '0.74rem', color: 'var(--text-muted)', marginTop: 2 }}>
                            ID: {c.id}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td>
                      <span className="badge badge-info" style={{ textTransform: 'uppercase', fontWeight: 600 }}>
                        {c.provider}
                      </span>
                    </td>
                    <td>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                        <span className="badge badge-neutral" style={{ alignSelf: 'flex-start' }}>
                          {c.owner_type === 'USER'
                            ? '个人用户'
                            : c.owner_type === 'ORGANIZATION'
                            ? '组织公共'
                            : '服务账号'}
                        </span>
                        {c.owner_type === 'USER' && (
                          <span style={{ fontSize: '0.78rem', color: 'var(--text-secondary)' }}>
                            {userMap[c.owner_id] || (c.owner_id ? c.owner_id.slice(0, 8) + '...' : '—')}
                          </span>
                        )}
                        {c.owner_type === 'SERVICE_ACCOUNT' && (
                          <span className="mono" style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                            {c.owner_id || '—'}
                          </span>
                        )}
                      </div>
                    </td>
                    <td>
                      <code style={{ fontSize: '0.8rem' }}>{c.auth_type.toUpperCase()}</code>
                    </td>
                    <td>
                      <span className="mono" style={{ fontSize: '0.8rem', color: '#047857', background: '#ecfdf5', borderColor: '#a7f3d0' }}>
                        🔒 {c.masked_value || '***'}
                      </span>
                    </td>
                    <td style={{ textAlign: 'center' }}>
                      <span className="badge badge-neutral" style={{ fontWeight: 600 }}>
                        {c.bound_employee_count ?? 0} 人
                      </span>
                    </td>
                    <td>
                      <StatusBadge status={c.status} />
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      <div style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'flex-end', gap: 6 }}>
                        <button type="button" className="btn-ghost btn-sm" onClick={() => openEditCred(c)}>
                          编辑
                        </button>
                        <button
                          type="button"
                          className="btn-ghost btn-sm"
                          style={{ color: '#dc2626', borderColor: '#fecaca' }}
                          onClick={() => handleDeleteCred(c.id, c.credential_name)}
                          title="销毁此凭证"
                        >
                          <IconTrash size={14} />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
                {credentials.length === 0 ? (
                  <tr>
                    <td colSpan={8} style={{ textAlign: 'center', padding: '2.5rem', color: 'var(--text-muted)' }}>
                      暂无凭证记录，点击右上角「新建身份凭证」录入
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Modal: MCP Server (添加 / 编辑) */}
      {showServerModal ? (
        <div className="modal-backdrop">
          <div className="modal-card">
            <div className="modal-header">
              <h3>
                <IconPlug size={18} style={{ color: 'var(--brand-600)' }} />
                <span>{editingServer ? '编辑 MCP 服务' : '添加自定义 MCP 服务'}</span>
              </h3>
              <button
                type="button"
                className="modal-close-btn"
                onClick={() => setShowServerModal(false)}
                title="关闭"
              >
                ✕
              </button>
            </div>
            <form onSubmit={handleSaveServer}>
              <div className="modal-body">
                <div className="modal-form-group">
                  <label>
                    <span>服务标识名 (Name) <span className="req-star">*</span></span>
                  </label>
                  <input
                    value={serverName}
                    onChange={(e) => setServerName(e.target.value)}
                    placeholder="例如 github-mcp 或 jira-mcp"
                    required
                  />
                  <div className="form-hint">英文字符、数字或中划线，用于数字员工与任务调度引擎寻址</div>
                </div>

                <div className="modal-grid-2">
                  <div className="modal-form-group">
                    <label>
                      <span>传输协议 (Transport) <span className="req-star">*</span></span>
                    </label>
                    <select
                      value={serverTransport}
                      onChange={(e) => setServerTransport(e.target.value as any)}
                    >
                      <option value="http">HTTP (Streamable JSON-RPC)</option>
                      <option value="sse">SSE (Server-Sent Events)</option>
                      <option value="stdio">Stdio (本地子进程命令行)</option>
                    </select>
                  </div>
                  <div className="modal-form-group">
                    <label>
                      <span>
                        {serverTransport === 'stdio' ? '执行命令 (Command) ' : '端点 URL (Endpoint) '}
                        <span className="req-star">*</span>
                      </span>
                    </label>
                    <input
                      value={serverEndpoint}
                      onChange={(e) => setServerEndpoint(e.target.value)}
                      placeholder={
                        serverTransport === 'stdio'
                          ? '例如 npx -y @modelcontextprotocol/server-filesystem'
                          : '例如 https://mcp.github.com/v1 或 http://host:8080/mcp'
                      }
                      required
                    />
                  </div>
                </div>

                <div className="modal-form-group">
                  <label>
                    <span>服务功能描述 (Description)</span>
                  </label>
                  <textarea
                    rows={2}
                    value={serverDesc}
                    onChange={(e) => setServerDesc(e.target.value)}
                    placeholder="说明此 MCP 服务所提供的工具集能力及适用业务场景..."
                  />
                </div>

                <div className="modal-form-group">
                  <label>
                    <span>自定义请求头 (Headers JSON, 可选)</span>
                  </label>
                  <textarea
                    rows={3}
                    className="mono"
                    value={serverHeaders}
                    onChange={(e) => setServerHeaders(e.target.value)}
                    placeholder='{"X-Custom-Header": "value"}'
                  />
                  <div className="form-hint">JSON 键值对格式，在与外部 MCP 服务握手连接时由网关自动携带</div>
                </div>
              </div>

              <div className="modal-footer">
                <button type="button" className="btn-ghost" onClick={() => setShowServerModal(false)}>
                  取消
                </button>
                <button type="submit" className="btn-primary">
                  {editingServer ? '保存修改' : '确认添加'}
                </button>
              </div>
            </form>
          </div>
        </div>
      ) : null}

      {/* Modal: Credential (录入 / 编辑) */}
      {showCredModal ? (
        <div className="modal-backdrop">
          <div className="modal-card">
            <div className="modal-header">
              <h3>
                <IconKey size={18} style={{ color: 'var(--brand-600)' }} />
                <span>{editingCred ? '编辑身份凭证' : '录入新身份凭证'}</span>
              </h3>
              <button
                type="button"
                className="modal-close-btn"
                onClick={() => setShowCredModal(false)}
                title="关闭"
              >
                ✕
              </button>
            </div>
            <form onSubmit={handleSaveCred}>
              <div className="modal-body">
                <div className="modal-form-group">
                  <label>
                    <span>凭证名称 (Credential Name) <span className="req-star">*</span></span>
                  </label>
                  <input
                    value={credName}
                    onChange={(e) => setCredName(e.target.value)}
                    placeholder="例如 张三的个人 GitHub PAT 或 运维工单 Jira 访问令牌"
                    required
                  />
                  <div className="form-hint">便于管理员与员工辨识该凭证适用的账号与业务场景</div>
                </div>

                <div className="modal-grid-2">
                  <div className="modal-form-group">
                    <label>
                      <span>Provider 服务提供方 <span className="req-star">*</span></span>
                    </label>
                    <select value={credProvider} onChange={(e) => setCredProvider(e.target.value)}>
                      <option value="github">GitHub</option>
                      <option value="feishu">Feishu (飞书)</option>
                      <option value="jira">Jira</option>
                      <option value="gitlab">GitLab</option>
                      <option value="custom">通用自定义 (Custom)</option>
                    </select>
                  </div>
                  <div className="modal-form-group">
                    <label>
                      <span>认证方式 (Auth Type) <span className="req-star">*</span></span>
                    </label>
                    <select value={credAuthType} onChange={(e) => setCredAuthType(e.target.value)}>
                      <option value="pat">Personal Access Token (PAT)</option>
                      <option value="api_key">API Key</option>
                      <option value="bearer">Bearer Token</option>
                      <option value="oauth">OAuth 2.0</option>
                      <option value="basic">Basic Auth</option>
                    </select>
                  </div>
                </div>

                <div className="modal-grid-2">
                  <div className="modal-form-group">
                    <label>
                      <span>所有者类型 (Owner Type) <span className="req-star">*</span></span>
                    </label>
                    <select
                      value={credOwnerType}
                      onChange={(e) => {
                        const newType = e.target.value as any
                        setCredOwnerType(newType)
                        if (newType === 'USER') {
                          const cur = getUser()
                          setCredOwnerId(cur?.id || (users[0]?.id ?? ''))
                        } else if (newType === 'ORGANIZATION') {
                          setCredOwnerId('ORGANIZATION')
                        } else {
                          setCredOwnerId('')
                        }
                      }}
                    >
                      <option value="USER">个人用户 (USER)</option>
                      <option value="ORGANIZATION">组织公共 (ORGANIZATION)</option>
                      <option value="SERVICE_ACCOUNT">服务账号 (SERVICE_ACCOUNT)</option>
                    </select>
                  </div>
                  <div className="modal-form-group">
                    {credOwnerType === 'USER' ? (
                      <>
                        <label>
                          <span>归属用户 (User) <span className="req-star">*</span></span>
                        </label>
                        <select
                          value={credOwnerId}
                          onChange={(e) => setCredOwnerId(e.target.value)}
                          required
                        >
                          {users.length === 0 ? (
                            <option value={credOwnerId || ''}>
                              {userMap[credOwnerId] || credOwnerId || '加载用户列表中...'}
                            </option>
                          ) : (
                            users.map((u) => {
                              const isSelf = u.id === getUser()?.id
                              return (
                                <option key={u.id} value={u.id}>
                                  {u.display_name ? `${u.display_name} (${u.username})` : u.username}
                                  {isSelf ? ' [当前登录]' : ''}
                                </option>
                              )
                            })
                          )}
                          {credOwnerId && !users.some((u) => u.id === credOwnerId) && (
                            <option value={credOwnerId}>
                              {userMap[credOwnerId] || credOwnerId}
                            </option>
                          )}
                        </select>
                        <div className="form-hint">选择此凭证归属的平台用户，数字员工执行该用户任务时优先采用</div>
                      </>
                    ) : credOwnerType === 'ORGANIZATION' ? (
                      <>
                        <label>
                          <span>归属范围</span>
                        </label>
                        <input
                          value="全平台组织公共 (全员共享，无需用户ID)"
                          disabled
                          style={{ background: 'var(--bg-card-hover)', color: 'var(--text-muted)' }}
                        />
                        <div className="form-hint">组织公共凭证对全平台授权数字员工可用</div>
                      </>
                    ) : (
                      <>
                        <label>
                          <span>服务账号标识 (Service Account) <span className="req-star">*</span></span>
                        </label>
                        <input
                          value={credOwnerId}
                          onChange={(e) => setCredOwnerId(e.target.value)}
                          placeholder="例如 system-bot 或 automation"
                          required
                        />
                        <div className="form-hint">填入系统服务账号名称或应用 ID</div>
                      </>
                    )}
                  </div>
                </div>

                <div className="modal-form-group">
                  <label>
                    <span>
                      {editingCred ? '更新密钥 / 敏感值 (留空则保持原密钥不变)' : '凭证密钥 / Token 明文 '}
                      {!editingCred && <span className="req-star">*</span>}
                    </span>
                  </label>
                  <input
                    type="password"
                    value={credSecret}
                    onChange={(e) => setCredSecret(e.target.value)}
                    placeholder={editingCred ? '留空保持已有加密密钥不变' : '输入 ghp_xxx 或对应 API Token 明文'}
                    required={!editingCred}
                  />
                  <div className="form-hint" style={{ color: '#059669', display: 'flex', alignItems: 'center', gap: 4 }}>
                    <span>🔒 凭证提交后将存入加密机密保管箱，在界面中仅脱敏展示，保障访问安全。</span>
                  </div>
                </div>
              </div>

              <div className="modal-footer">
                <button type="button" className="btn-ghost" onClick={() => setShowCredModal(false)}>
                  取消
                </button>
                <button type="submit" className="btn-primary">
                  {editingCred ? '保存凭证' : '确认录入'}
                </button>
              </div>
            </form>
          </div>
        </div>
      ) : null}
    </section>
  )
}
