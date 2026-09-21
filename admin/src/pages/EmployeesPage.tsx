/**
 * Employees 数字员工列表与创建/删除。
 */
import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiDelete, apiGet, apiPost } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { EntityName } from '../components/EntityName'
import { IconAlertTriangle, IconPlus, IconRefresh, IconTrash, IconUsers } from '../components/Icons'
import { PageFeatureGuide } from '../components/PageFeatureGuide'

type Emp = {
  id: string
  name: string
  status: string
  default_provider: string
  workstation_id: string
  workspace_id: string
}

type WsItem = {
  id: string
  path: string
  repository?: string
}

export function EmployeesPage() {
  const [items, setItems] = useState<Emp[]>([])
  const [workstations, setWorkstations] = useState<Array<{ id: string; name: string }>>([])
  const [workspaces, setWorkspaces] = useState<WsItem[]>([])
  const [name, setName] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')

  // 删除相关状态
  const [deleteTarget, setDeleteTarget] = useState<Emp | null>(null)
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

  async function load() {
    setLoading(true)
    try {
      const [empData, wsData, wspData] = await Promise.all([
        apiGet<{ items: Emp[] }>('/employees'),
        apiGet<{ items: Array<{ id: string; name: string }> }>('/workstations').catch(() => ({ items: [] })),
        apiGet<{ items: WsItem[] }>('/workspaces').catch(() => ({ items: [] })),
      ])
      setItems(empData.items ?? [])
      setWorkstations(wsData.items ?? [])
      setWorkspaces(wspData.items ?? [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    checkTotpStatus()
  }, [])

  const wsMap = Object.fromEntries(
    workstations.map((w) => [w.id, w.name && w.name !== w.id ? w.name : `工作站-${w.id.slice(-6)}`])
  )

  const wspMap = Object.fromEntries(
    workspaces.map((w) => [w.id, w.repository ? `${w.repository}` : (w.path ? w.path.split('/').pop() || w.path : w.id)])
  )

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      await apiPost('/employees', { name })
      setName('')
      setMsg('数字员工创建成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建数字员工失败')
    }
  }

  function openDeleteModal(emp: Emp) {
    setDeleteTarget(emp)
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
      // 若填写了二次核验密码，先执行 step-up 提权
      if (adminPassword.trim()) {
        await apiPost('/auth/step-up', {
          password: adminPassword.trim(),
          totp: totpCode.trim() || undefined,
        })
      }
      await apiDelete(`/employees/${deleteTarget.id}`)
      setMsg(`数字员工 [${deleteTarget.name}] (${deleteTarget.id}) 已安全删除`)
      setDeleteTarget(null)
      await load()
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : '删除数字员工失败'
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

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>数字员工档案管理 (Employees)</h1>
          <p>数字员工代表具备独立上下文与工作区绑定的 AI 协作实体</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => void load()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新列表</span>
        </button>
      </header>

      {msg ? (
        <div className="badge badge-ok" style={{ display: 'block', padding: '0.65rem 1rem', marginBottom: '1rem', fontSize: '0.86rem' }}>
          ✅ {msg}
        </div>
      ) : null}

      <PageFeatureGuide
        title="数字员工协同架构与三元绑定指引"
        summary="数字员工是具备专属技能、权限策略与记忆上下文的自治 AI 智能体，通过绑定具体的工作站与本地工作区实现自主编码与协作。"
        steps={[
          {
            step: '1',
            title: '创建专属数字身份',
            desc: '设定员工专属工号与代号，平台将为其分配独立的权限 Profile 与上下文隔离沙箱。',
            tag: '数字实体',
          },
          {
            step: '2',
            title: '工作站算力节点挂载',
            desc: '将员工指派到具体的物理机/虚拟机工作站节点（Workstation），任务下发时由该节点代为运行。',
            tag: '算力映射',
          },
          {
            step: '3',
            title: '项目工作区路径挂载',
            desc: '关联员工常驻的项目目录（Workspace），确保任务执行生成的文件精确落在指定工程内。',
            tag: '目录挂载',
          },
        ]}
      />

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>新增数字员工</h2>
            <p>输入员工名称快速创建，创建后可进入详情绑定工作站与代码工作区</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onCreate}>
          <input
            placeholder="员工姓名 / 代号 (例: 研发助理小智)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            style={{ width: '280px' }}
            required
          />
          <button type="submit">
            <IconPlus size={15} />
            <span>立即创建员工</span>
          </button>
        </form>
        {error ? <div className="error" style={{ marginTop: '0.6rem' }}>{error}</div> : null}
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>员工名录列表</h2>
            <p>共登记 {items.length} 位数字员工</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>数字员工 (姓名 / 编号)</th>
                <th>运行状态</th>
                <th>默认驱动引擎</th>
                <th>绑定工作站</th>
                <th>绑定工作区</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((e) => (
                <tr key={e.id}>
                  <td>
                    <EntityName
                      name={e.name}
                      id={e.id}
                      to={`/employees/${e.id}`}
                      icon={<IconUsers size={16} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td>
                    <StatusBadge status={e.status} />
                  </td>
                  <td>
                    <span className="badge" style={{ textTransform: 'capitalize' }}>
                      {e.default_provider || '未指定'}
                    </span>
                  </td>
                  <td>
                    {e.workstation_id ? (
                      <EntityName
                        name={wsMap[e.workstation_id]}
                        id={e.workstation_id}
                        fallback={e.workstation_id}
                      />
                    ) : (
                      <span style={{ color: 'var(--text-muted)' }}>未绑定</span>
                    )}
                  </td>
                  <td>
                    {e.workspace_id ? (
                      <EntityName
                        name={wspMap[e.workspace_id]}
                        id={e.workspace_id}
                        fallback={e.workspace_id}
                      />
                    ) : (
                      <span style={{ color: 'var(--text-muted)' }}>未绑定</span>
                    )}
                  </td>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                      <Link to={`/employees/${e.id}`} className="btn-ghost btn-sm" style={{ display: 'inline-flex' }}>
                        配置详情 →
                      </Link>
                      <button
                        type="button"
                        className="btn-danger btn-sm"
                        title="删除该数字员工"
                        onClick={() => openDeleteModal(e)}
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
                  <td colSpan={6} className="empty-tip">暂无数字员工记录，请在上方创建</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      {/* 删除数字员工危险操作二次确认弹窗 */}
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
                <span style={{ color: '#b91c1c', fontWeight: 700 }}>{deleteTarget.name}</span>
              </div>
              <div style={{ color: '#7f1d1d', fontSize: '0.78rem', fontFamily: 'monospace' }}>
                员工 ID: {deleteTarget.id}
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
                onChange={(e) => setAdminPassword(e.target.value)}
                style={{ width: '100%', boxSizing: 'border-box' }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') void confirmDelete()
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
                  onChange={(e) => setTotpCode(e.target.value.trim())}
                  maxLength={6}
                  style={{
                    width: '100%',
                    boxSizing: 'border-box',
                    fontSize: '1.05rem',
                    textAlign: 'center',
                    letterSpacing: '3px',
                    fontWeight: 600,
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') void confirmDelete()
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
                {deleting ? '正在安全删除...' : '确定删除'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
