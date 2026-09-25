import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
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

export function McpServersPage() {
  const [tab, setTab] = useState<Tab>('servers')
  const [servers, setServers] = useState<MCPServer[]>([])
  const [credentials, setCredentials] = useState<Credential[]>([])
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

  async function loadData() {
    setLoading(true)
    setError('')
    try {
      const [sRes, cRes] = await Promise.all([listMCPServers(), listCredentials()])
      setServers(sRes.items || [])
      setCredentials(cRes.items || [])
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
    setEditingCred(null)
    setCredName('')
    setCredOwnerType('USER')
    setCredOwnerId('')
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
    try {
      if (editingCred) {
        await updateCredential(editingCred.id, {
          credential_name: credName,
          owner_type: credOwnerType,
          owner_id: credOwnerId,
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
          owner_id: credOwnerId,
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
            <button type="button" className="btn-primary" onClick={openCreateServer}>
              <IconPlus size={15} />
              <span>添加 MCP 服务</span>
            </button>
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
      <div className="tab-nav" style={{ marginBottom: '1.2rem' }}>
        <button
          type="button"
          className={`tab-btn ${tab === 'servers' ? 'active' : ''}`}
          onClick={() => setTab('servers')}
        >
          <IconPlug size={16} />
          <span>MCP 服务列表 ({servers.length})</span>
        </button>
        <button
          type="button"
          className={`tab-btn ${tab === 'credentials' ? 'active' : ''}`}
          onClick={() => setTab('credentials')}
        >
          <IconKey size={16} />
          <span>身份凭证保管库 ({credentials.length})</span>
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
                  <th>服务标识 / 名称</th>
                  <th>类型</th>
                  <th>协议与端点</th>
                  <th>描述说明</th>
                  <th>绑定员工数</th>
                  <th>状态</th>
                  <th style={{ textAlign: 'right' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {servers.map((s) => {
                  const isBuiltin = s.server_type === 'builtin' || s.id === 'mcp-workflow'
                  return (
                    <tr key={s.id}>
                      <td>
                        <div style={{ fontWeight: 600, display: 'flex', alignItems: 'center', gap: 6 }}>
                          {isBuiltin ? <IconZap size={16} style={{ color: 'var(--primary)' }} /> : <IconPlug size={16} />}
                          <span>{s.name}</span>
                        </div>
                        <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>ID: {s.id}</div>
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
                        <span className="badge" style={{ marginRight: 6, textTransform: 'uppercase' }}>
                          {s.transport}
                        </span>
                        <code style={{ fontSize: '0.84rem' }}>{s.endpoint || '—'}</code>
                      </td>
                      <td style={{ maxWidth: 300, fontSize: '0.86rem', color: 'var(--text-secondary)' }}>
                        {s.description || '—'}
                      </td>
                      <td>
                        <span className="badge badge-neutral">
                          {s.bound_employee_count ?? 0} 位员工绑定
                        </span>
                      </td>
                      <td>
                        <StatusBadge status={s.status} />
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        <div style={{ display: 'inline-flex', gap: 8 }}>
                          {isBuiltin ? (
                            <Link
                              to="/mcp-servers/workflow-mcp"
                              className="btn-primary btn-sm"
                              style={{ display: 'inline-flex', alignItems: 'center', gap: 4, textDecoration: 'none' }}
                            >
                              <IconSettings size={14} />
                              <span>进入配置与详情</span>
                            </Link>
                          ) : (
                            <>
                              <button
                                type="button"
                                className="btn-ghost btn-sm"
                                onClick={() => openEditServer(s)}
                              >
                                编辑
                              </button>
                              <button
                                type="button"
                                className="btn-ghost btn-sm"
                                style={{ color: 'var(--danger)' }}
                                onClick={() => handleDeleteServer(s.id, s.name)}
                              >
                                <IconTrash size={14} />
                              </button>
                            </>
                          )}
                        </div>
                      </td>
                    </tr>
                  )
                })}
                {servers.length === 0 ? (
                  <tr>
                    <td colSpan={7} style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
                      暂无已注册的 MCP 服务
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
                  <th>凭证名称</th>
                  <th>Provider</th>
                  <th>所有者类型</th>
                  <th>认证类型</th>
                  <th>脱敏凭证</th>
                  <th>绑定员工数</th>
                  <th>状态</th>
                  <th style={{ textAlign: 'right' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {credentials.map((c) => (
                  <tr key={c.id}>
                    <td>
                      <div style={{ fontWeight: 600 }}>{c.credential_name}</div>
                      <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>ID: {c.id}</div>
                    </td>
                    <td>
                      <span className="badge badge-info" style={{ textTransform: 'uppercase' }}>
                        {c.provider}
                      </span>
                    </td>
                    <td>
                      <span className="badge badge-neutral">
                        {c.owner_type === 'USER'
                          ? '个人用户'
                          : c.owner_type === 'ORGANIZATION'
                          ? '组织公共'
                          : '服务账号'}
                      </span>
                    </td>
                    <td>
                      <code style={{ fontSize: '0.84rem' }}>{c.auth_type}</code>
                    </td>
                    <td>
                      <code style={{ fontSize: '0.84rem' }}>{c.masked_value || '***'}</code>
                    </td>
                    <td>
                      <span className="badge badge-neutral">{c.bound_employee_count ?? 0}</span>
                    </td>
                    <td>
                      <StatusBadge status={c.status} />
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      <div style={{ display: 'inline-flex', gap: 8 }}>
                        <button type="button" className="btn-ghost btn-sm" onClick={() => openEditCred(c)}>
                          编辑
                        </button>
                        <button
                          type="button"
                          className="btn-ghost btn-sm"
                          style={{ color: 'var(--danger)' }}
                          onClick={() => handleDeleteCred(c.id, c.credential_name)}
                        >
                          <IconTrash size={14} />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
                {credentials.length === 0 ? (
                  <tr>
                    <td colSpan={8} style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
                      暂无凭证记录，点击右上角「新建身份凭证」录入
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Modal: MCP Server */}
      {showServerModal ? (
        <div className="modal-backdrop">
          <div className="modal-card" style={{ maxWidth: 540 }}>
            <div className="modal-header">
              <h3>{editingServer ? '编辑 MCP 服务' : '添加自定义 MCP 服务'}</h3>
              <button type="button" className="btn-ghost" onClick={() => setShowServerModal(false)}>
                ✕
              </button>
            </div>
            <form onSubmit={handleSaveServer}>
              <div className="form-group" style={{ marginBottom: 12 }}>
                <label>服务标识名 (Name) *</label>
                <input
                  value={serverName}
                  onChange={(e) => setServerName(e.target.value)}
                  placeholder="例如 github-mcp 或 jira-mcp"
                  required
                />
              </div>

              <div className="form-group" style={{ marginBottom: 12 }}>
                <label>传输协议 (Transport) *</label>
                <select
                  value={serverTransport}
                  onChange={(e) => setServerTransport(e.target.value as any)}
                >
                  <option value="http">HTTP (Streamable / JSON-RPC)</option>
                  <option value="sse">SSE (Server-Sent Events)</option>
                  <option value="stdio">Stdio (本地子进程命令行)</option>
                </select>
              </div>

              <div className="form-group" style={{ marginBottom: 12 }}>
                <label>
                  {serverTransport === 'stdio' ? '执行命令 (Command / Executable) *' : '端点 URL (Endpoint) *'}
                </label>
                <input
                  value={serverEndpoint}
                  onChange={(e) => setServerEndpoint(e.target.value)}
                  placeholder={
                    serverTransport === 'stdio'
                      ? '例如 npx -y @modelcontextprotocol/server-filesystem 或 python3'
                      : '例如 https://mcp.github.com/v1 或 http://10.0.0.8:8080/mcp'
                  }
                  required
                />
              </div>

              <div className="form-group" style={{ marginBottom: 12 }}>
                <label>服务描述说明</label>
                <textarea
                  rows={2}
                  value={serverDesc}
                  onChange={(e) => setServerDesc(e.target.value)}
                  placeholder="说明此 MCP 服务所提供的工具集能力及适用场景..."
                />
              </div>

              <div className="form-group" style={{ marginBottom: 16 }}>
                <label>自定义请求头 (Headers JSON, 可选)</label>
                <textarea
                  rows={3}
                  className="mono"
                  style={{ fontSize: '0.84rem' }}
                  value={serverHeaders}
                  onChange={(e) => setServerHeaders(e.target.value)}
                  placeholder='{"X-Custom-Header": "value"}'
                />
              </div>

              <div className="modal-actions" style={{ display: 'flex', justifyContent: 'flex-end', gap: 10 }}>
                <button type="button" className="btn-ghost" onClick={() => setShowServerModal(false)}>
                  取消
                </button>
                <button type="submit" className="btn-primary">
                  保存
                </button>
              </div>
            </form>
          </div>
        </div>
      ) : null}

      {/* Modal: Credential */}
      {showCredModal ? (
        <div className="modal-backdrop">
          <div className="modal-card" style={{ maxWidth: 540 }}>
            <div className="modal-header">
              <h3>{editingCred ? '编辑身份凭证' : '录入新身份凭证'}</h3>
              <button type="button" className="btn-ghost" onClick={() => setShowCredModal(false)}>
                ✕
              </button>
            </div>
            <form onSubmit={handleSaveCred}>
              <div className="form-group" style={{ marginBottom: 12 }}>
                <label>凭证名称 *</label>
                <input
                  value={credName}
                  onChange={(e) => setCredName(e.target.value)}
                  placeholder="例如 张三的个人 GitHub PAT 或 公司 Jira Bot"
                  required
                />
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
                <div className="form-group">
                  <label>Provider 提供方 *</label>
                  <select value={credProvider} onChange={(e) => setCredProvider(e.target.value)}>
                    <option value="github">GitHub</option>
                    <option value="feishu">Feishu (飞书)</option>
                    <option value="jira">Jira</option>
                    <option value="gitlab">GitLab</option>
                    <option value="custom">通用自定义 (Custom)</option>
                  </select>
                </div>
                <div className="form-group">
                  <label>认证方式 (Auth Type) *</label>
                  <select value={credAuthType} onChange={(e) => setCredAuthType(e.target.value)}>
                    <option value="pat">Personal Access Token (PAT)</option>
                    <option value="api_key">API Key</option>
                    <option value="bearer">Bearer Token</option>
                    <option value="oauth">OAuth 2.0</option>
                    <option value="basic">Basic Auth</option>
                  </select>
                </div>
              </div>

              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
                <div className="form-group">
                  <label>所有者类型 *</label>
                  <select
                    value={credOwnerType}
                    onChange={(e) => setCredOwnerType(e.target.value as any)}
                  >
                    <option value="USER">个人用户 (USER)</option>
                    <option value="ORGANIZATION">组织公共 (ORGANIZATION)</option>
                    <option value="SERVICE_ACCOUNT">服务账号 (SERVICE_ACCOUNT)</option>
                  </select>
                </div>
                <div className="form-group">
                  <label>归属用户/机构 ID</label>
                  <input
                    value={credOwnerId}
                    onChange={(e) => setCredOwnerId(e.target.value)}
                    placeholder="留空则默认归属当前登录用户"
                  />
                </div>
              </div>

              <div className="form-group" style={{ marginBottom: 16 }}>
                <label>
                  {editingCred ? '更新密钥 / 敏感值 (留空则保持原值不变)' : '凭证密钥 / Token 明文 *'}
                </label>
                <input
                  type="password"
                  value={credSecret}
                  onChange={(e) => setCredSecret(e.target.value)}
                  placeholder={editingCred ? '留空保持已有加密密钥不变' : '输入 ghp_xxx 或 API Key'}
                  required={!editingCred}
                />
                <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', marginTop: 4 }}>
                  凭证提交后将存入加密保管箱，在界面中仅脱敏展示，保障凭据安全。
                </div>
              </div>

              <div className="modal-actions" style={{ display: 'flex', justifyContent: 'flex-end', gap: 10 }}>
                <button type="button" className="btn-ghost" onClick={() => setShowCredModal(false)}>
                  取消
                </button>
                <button type="submit" className="btn-primary">
                  保存凭证
                </button>
              </div>
            </form>
          </div>
        </div>
      ) : null}
    </section>
  )
}
