import { useEffect, useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { EntityName } from '../components/EntityName'
import { IconAlertTriangle, IconCheckCircle, IconPlus, IconRefresh, IconServer, IconTerminal, IconTrash } from '../components/Icons'
import { PageFeatureGuide } from '../components/PageFeatureGuide'

type WS = {
  id: string
  name: string
  status: string
  cert_status: string
  fingerprint: string
  last_heartbeat_at?: string
  cpu_percent?: number
  memory_percent?: number
}

type TokenResult = {
  token: string
  id: string
  expires_at: string
  label: string
}

export function WorkstationsPage() {
  const [items, setItems] = useState<WS[]>([])
  const [loading, setLoading] = useState(false)
  const [showEnroll, setShowEnroll] = useState(false)
  const [msg, setMsg] = useState('')

  // 删除相关状态
  const [deleteTarget, setDeleteTarget] = useState<WS | null>(null)
  const [adminPassword, setAdminPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [totpEnabled, setTotpEnabled] = useState(false)
  const [showManualTotp, setShowManualTotp] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [modalError, setModalError] = useState('')

  const checkTotpStatus = () => {
    void apiGet<{ enabled: boolean }>('/auth/totp')
      .then((res) => setTotpEnabled(!!res.enabled))
      .catch(() => setTotpEnabled(false))
  }

  useEffect(() => {
    checkTotpStatus()
  }, [])

  // 令牌生成表单
  const [label, setLabel] = useState('开发计算节点')
  const [ttlHours, setTtlHours] = useState(24)
  const [serverHost, setServerHost] = useState(window.location.hostname || '127.0.0.1')
  const [serverPort, setServerPort] = useState('8080')
  const [tokenResult, setTokenResult] = useState<TokenResult | null>(null)
  const [generating, setGenerating] = useState(false)
  const [tokenErr, setTokenErr] = useState('')
  const [activeTab, setActiveTab] = useState<'unix' | 'windows'>('unix')
  const [copied, setCopied] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const d = await apiGet<{ items: WS[] }>('/workstations')
      setItems(d.items ?? [])
    } finally {
      setLoading(false)
    }
  }

  async function renameWorkstation(id: string, oldName: string) {
    const defaultVal = oldName && oldName !== id ? oldName : ''
    const newName = prompt('请输入工作站节点的新名称（例如：Mac开发机 / GPU计算节点）：', defaultVal)
    if (!newName || !newName.trim() || newName.trim() === oldName) return
    try {
      await apiPatch(`/workstations/${id}`, { name: newName.trim() })
      await load()
    } catch (e: unknown) {
      alert('重命名失败: ' + (e instanceof Error ? e.message : '未知错误'))
    }
  }

  function openDeleteModal(ws: WS) {
    setDeleteTarget(ws)
    setAdminPassword('')
    setTotpCode('')
    setModalError('')
    setShowManualTotp(false)
    checkTotpStatus()
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    setDeleting(true)
    setModalError('')
    try {
      if (adminPassword.trim()) {
        await apiPost('/auth/step-up', {
          password: adminPassword.trim(),
          totp: totpCode.trim() || undefined,
        })
      }
      await apiDelete(`/workstations/${deleteTarget.id}`)
      setMsg(`工作站计算节点 [${deleteTarget.name}] (${deleteTarget.id}) 已安全移除，数字证书已自动吊销`)
      setDeleteTarget(null)
      await load()
    } catch (err: unknown) {
      const errMsg = err instanceof Error ? err.message : '删除工作站失败'
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

  useEffect(() => {
    void load()
  }, [])

  async function handleCreateToken(e: React.FormEvent) {
    e.preventDefault()
    setGenerating(true)
    setTokenErr('')
    setTokenResult(null)
    setCopied(false)
    try {
      const res = await apiPost<TokenResult>('/enrollment/tokens', {
        label: label.trim() || '工作站计算节点',
        ttl_hours: Number(ttlHours) || 24,
      })
      setTokenResult(res)
    } catch (err: unknown) {
      setTokenErr(err instanceof Error ? err.message : '生成接入令牌失败')
    } finally {
      setGenerating(false)
    }
  }

  const serverUrl = `http://${serverHost.trim() || '127.0.0.1'}:${serverPort.trim() || '8080'}`

  const unixCommand = tokenResult
    ? `export AIE_DATA_DIR="$HOME/.aie"
aew register --server ${serverUrl} --token ${tokenResult.token}
aew service install
aew service start
aew service status`
    : ''

  const winCommand = tokenResult
    ? `aew register --server ${serverUrl} --token ${tokenResult.token}
aew service install
aew service start
aew service status`
    : ''

  function copyCommand(text: string) {
    void navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2500)
  }

  const onlineCount = items.filter((w) => w.status === 'ONLINE' || w.status === 'BUSY').length

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>工作站计算节点 (Workstations)</h1>
          <p>承载 AI 数字员工本地 Runtime、工具链沙箱与代码操作环境的主机节点</p>
        </div>
        <div style={{ display: 'flex', gap: '0.6rem' }}>
          <button
            type="button"
            className="btn"
            onClick={() => {
              setShowEnroll((prev) => !prev)
              if (!showEnroll && items.length === 0 && !tokenResult) {
                // 默认打开
              }
            }}
          >
            <IconPlus size={15} />
            <span>{showEnroll ? '收起接入向导' : '➕ 接入新工作站'}</span>
          </button>
          <button type="button" className="btn-ghost" onClick={() => void load()} disabled={loading}>
            <IconRefresh size={15} />
            <span>刷新列表</span>
          </button>
        </div>
      </header>

      {msg ? (
        <div className="badge badge-ok" style={{ display: 'block', padding: '0.65rem 1rem', marginBottom: '1rem', fontSize: '0.86rem' }}>
          ✅ {msg}
        </div>
      ) : null}

      <PageFeatureGuide
        title="工作站节点接入、证书分发与保活架构指引"
        summary="Workstation 是连接在物理主机或云虚拟机上的轻量级执行节点（aew），通过纯出站双向长连接实现零外网开放端口的绝对安全通信。"
        steps={[
          {
            step: '1',
            title: '生成一性接入令牌 (Enrollment)',
            desc: '在后台生成包含有效期的临时注册 Token，为新机器注入信任基础。',
            tag: '凭证下发',
          },
          {
            step: '2',
            title: 'mTLS 双向证书颁发与落盘',
            desc: '工作站执行 aew register，由内置 CA 自动签署客户端 X.509 证书与公私钥对，彻底摒弃静态口令。',
            tag: '身份认证',
          },
          {
            step: '3',
            title: '仅出站长连接与心跳遥测',
            desc: '工作站常驻服务通过 TLS 端口 9090 主动上联，无需开放公网端口，秒级遥测 CPU、内存与 Agent 活跃状态。',
            tag: '反向通道',
          },
        ]}
      />

      {/* 节点接入向导与令牌生成面板 */}
      {showEnroll && (
        <div className="panel" style={{ border: '2px solid var(--brand-500)', background: '#fcfdfd' }}>
          <div className="panel-header">
            <div>
              <h2 style={{ color: 'var(--brand-700)', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <IconTerminal size={18} />
                接入新工作站节点向导 (Workstation Enrollment)
              </h2>
              <p>向中心服务器 Control Plane 申请一次性接入令牌 (Token)，并在工作站端运行注册命令</p>
            </div>
          </div>

          <form onSubmit={handleCreateToken} style={{ marginBottom: '1.2rem' }}>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '1rem', alignItems: 'flex-end' }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                <label style={{ fontSize: '0.85rem', fontWeight: 600 }}>节点备注名称</label>
                <input
                  type="text"
                  value={label}
                  onChange={(e) => setLabel(e.target.value)}
                  placeholder="例: 开发机-Mac / 算力节点-Windows"
                  style={{ width: '220px', padding: '0.45rem 0.65rem' }}
                  required
                />
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                <label style={{ fontSize: '0.85rem', fontWeight: 600 }}>令牌有效期 (小时)</label>
                <input
                  type="number"
                  value={ttlHours}
                  onChange={(e) => setTtlHours(Number(e.target.value))}
                  min={1}
                  max={720}
                  style={{ width: '110px', padding: '0.45rem 0.65rem' }}
                  required
                />
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                <label style={{ fontSize: '0.85rem', fontWeight: 600 }}>中心服务器 IP/域名</label>
                <input
                  type="text"
                  value={serverHost}
                  onChange={(e) => setServerHost(e.target.value)}
                  placeholder="127.0.0.1"
                  style={{ width: '180px', padding: '0.45rem 0.65rem' }}
                  required
                />
              </div>

              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                <label style={{ fontSize: '0.85rem', fontWeight: 600 }}>HTTP 端口</label>
                <input
                  type="text"
                  value={serverPort}
                  onChange={(e) => setServerPort(e.target.value)}
                  style={{ width: '80px', padding: '0.45rem 0.65rem' }}
                  required
                />
              </div>

              <button type="submit" className="btn" disabled={generating}>
                {generating ? '正在生成…' : '生成一次性接入令牌'}
              </button>
            </div>
          </form>

          {tokenErr && <div className="error" style={{ marginBottom: '1rem' }}>{tokenErr}</div>}

          {tokenResult && (
            <div style={{ background: '#f8fafc', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '1.2rem' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.8rem', flexWrap: 'wrap', gap: '0.5rem' }}>
                <div>
                  <span style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>接入令牌已就绪（单次使用有效）：</span>
                  <div className="mono" style={{ fontSize: '1rem', fontWeight: 700, color: 'var(--brand-700)', marginTop: '0.2rem' }}>
                    {tokenResult.token}
                  </div>
                </div>
                <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>
                  有效期至: {new Date(tokenResult.expires_at).toLocaleString()}
                </div>
              </div>

              <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '0.6rem' }}>
                <button
                  type="button"
                  onClick={() => setActiveTab('unix')}
                  className={activeTab === 'unix' ? 'btn' : 'btn-ghost'}
                  style={{ fontSize: '0.82rem', padding: '0.3rem 0.8rem' }}
                >
                  macOS / Linux
                </button>
                <button
                  type="button"
                  onClick={() => setActiveTab('windows')}
                  className={activeTab === 'windows' ? 'btn' : 'btn-ghost'}
                  style={{ fontSize: '0.82rem', padding: '0.3rem 0.8rem' }}
                >
                  Windows (PowerShell)
                </button>
              </div>

              <div style={{ position: 'relative' }}>
                <pre
                  className="mono"
                  style={{
                    background: '#0f172a',
                    color: '#f8fafc',
                    padding: '1rem 1.2rem',
                    borderRadius: 'var(--radius-sm)',
                    overflowX: 'auto',
                    fontSize: '0.86rem',
                    lineHeight: '1.5',
                  }}
                >
                  {activeTab === 'unix' ? unixCommand : winCommand}
                </pre>
                <button
                  type="button"
                  className="btn"
                  onClick={() => copyCommand(activeTab === 'unix' ? unixCommand : winCommand)}
                  style={{
                    position: 'absolute',
                    top: '0.6rem',
                    right: '0.6rem',
                    fontSize: '0.78rem',
                    padding: '0.3rem 0.6rem',
                  }}
                >
                  {copied ? '✅ 已复制命令' : '复制代码'}
                </button>
              </div>

              <div style={{ marginTop: '0.8rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '0.5rem' }}>
                <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                  💡 全平台支持直接运行 <code>aew</code> 命令（服务安装时会自动注册系统软链接；也可随时运行 <code>aew link</code> 手动配置）。
                </div>
                <button
                  type="button"
                  className="btn-ghost"
                  onClick={() => {
                    void load()
                    setShowEnroll(false)
                  }}
                >
                  <IconCheckCircle size={15} />
                  <span>已完成配置，收起并查看节点</span>
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* 节点状态统计条 */}
      <div style={{ display: 'flex', gap: '1rem', marginBottom: '1.2rem' }}>
        <div style={{ background: '#fff', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '0.8rem 1.2rem', flex: 1 }}>
          <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>总接入计算节点</div>
          <div style={{ fontSize: '1.5rem', fontWeight: 700, color: 'var(--text-primary)' }}>{items.length}</div>
        </div>
        <div style={{ background: '#fff', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '0.8rem 1.2rem', flex: 1 }}>
          <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>在线活跃节点 (Active)</div>
          <div style={{ fontSize: '1.5rem', fontWeight: 700, color: 'var(--brand-600)' }}>{onlineCount}</div>
        </div>
        <div style={{ background: '#fff', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '0.8rem 1.2rem', flex: 1 }}>
          <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>节点通信加密机制</div>
          <div style={{ fontSize: '0.92rem', fontWeight: 600, color: 'var(--text-secondary)', marginTop: '0.35rem' }}>
            双向 mTLS (gRPC :9090)
          </div>
        </div>
      </div>

      {/* 节点列表 */}
      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>已接入工作站列表</h2>
            <p>基于数字证书信任链、心跳探针与资源负载管控</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>计算节点名称 (Node Name)</th>
                <th>运行状态</th>
                <th>证书信任状态</th>
                <th>客户端证书指纹 (Fingerprint)</th>
                <th>最近心跳通信时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((w) => (
                <tr key={w.id}>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
                      <EntityName
                        name={w.name && w.name !== w.id ? w.name : `工作站-${w.id.slice(-6)}`}
                        id={w.id}
                        icon={<IconServer size={16} style={{ color: 'var(--brand-600)' }} />}
                      />
                      {(!w.name || w.name === w.id) && (
                        <span className="badge" style={{ fontSize: '0.68rem', padding: '0.1rem 0.35rem', background: '#fef3c7', color: '#b45309', border: '1px solid #fde68a' }}>
                          待命名
                        </span>
                      )}
                    </div>
                  </td>
                  <td>
                    <StatusBadge status={w.status} />
                  </td>
                  <td>
                    <StatusBadge status={w.cert_status || 'UNKNOWN'} />
                  </td>
                  <td>
                    <span className="mono" title={w.fingerprint}>
                      {w.fingerprint ? `${w.fingerprint.slice(0, 16)}…` : '—'}
                    </span>
                  </td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {w.last_heartbeat_at && !w.last_heartbeat_at.startsWith('0001')
                      ? new Date(w.last_heartbeat_at).toLocaleString()
                      : '等待初次心跳'}
                  </td>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                      <button
                        type="button"
                        className="btn-ghost btn-sm"
                        title="点击重命名节点名称"
                        onClick={() => void renameWorkstation(w.id, w.name)}
                      >
                        ✏️ 改名
                      </button>
                      <button
                        type="button"
                        className="btn-danger btn-sm"
                        title="从平台删除该工作站并自动吊销证书"
                        onClick={() => openDeleteModal(w)}
                      >
                        <IconTrash size={13} />
                        <span>删除</span>
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={6} style={{ textAlign: 'center', padding: '3rem 1rem' }}>
                    <div style={{ color: 'var(--text-muted)', marginBottom: '1rem' }}>
                      暂无工作站节点接入。请点击上方按钮生成注册令牌并接入计算节点。
                    </div>
                    <button
                      type="button"
                      className="btn"
                      onClick={() => setShowEnroll(true)}
                    >
                      <IconPlus size={15} />
                      <span>立即接入第一台工作站</span>
                    </button>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      {/* 删除工作站高危操作二次确认弹窗 */}
      {deleteTarget ? (
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
              width: '490px',
              maxWidth: '92vw',
              boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1)',
              border: '1px solid #fee2e2',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', marginBottom: '1rem' }}>
              <IconAlertTriangle size={22} style={{ color: '#ef4444' }} />
              <h3 style={{ margin: 0, fontSize: '1.15rem', color: '#b91c1c' }}>
                安全告警：移除工作站计算节点
              </h3>
            </div>

            <p style={{ fontSize: '0.86rem', color: 'var(--text-secondary)', lineHeight: 1.6, margin: '0 0 1rem' }}>
              移除工作站节点属于<strong>不可逆关键运维操作</strong>：
            </p>
            <ul style={{ fontSize: '0.82rem', color: 'var(--text-secondary)', paddingLeft: '1.2rem', margin: '0 0 1rem', lineHeight: 1.6 }}>
              <li>系统将自动<strong>吊销（Revoke）该工作站的 mTLS 客户端证书</strong>，切断握手通信；</li>
              <li>已绑定到该工作站的数字员工将自动解除算力映射；</li>
              <li><strong>若该计算节点当前有正在执行中的任务，系统将拒绝删除以保护任务执行完整性。</strong></li>
            </ul>

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
                <strong>待移除节点：</strong>
                <span style={{ color: '#b91c1c', fontWeight: 700 }}>{deleteTarget.name}</span>
              </div>
              <div style={{ color: '#7f1d1d', fontSize: '0.78rem', fontFamily: 'monospace' }}>
                节点 ID: {deleteTarget.id}
              </div>
              {deleteTarget.fingerprint ? (
                <div style={{ color: '#7f1d1d', fontSize: '0.75rem', fontFamily: 'monospace', marginTop: '0.2rem' }}>
                  证书指纹: {deleteTarget.fingerprint.slice(0, 24)}…
                </div>
              ) : null}
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
                onClick={() => setDeleteTarget(null)}
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
                {deleting ? '正在安全删除与吊销...' : '确定删除'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
