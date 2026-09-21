import { useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { apiGet, apiPost } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { IconCheckCircle, IconRefresh, IconKey, IconShield, IconAlertTriangle } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { PageFeatureGuide } from '../components/PageFeatureGuide'

type Approval = {
  id: string
  job_id: string
  action: string
  status: string
  critical: boolean
  reason: string
  created_at: string
}

type JobItem = {
  id: string
  prompt: string
}

export function ApprovalsPage() {
  const [items, setItems] = useState<Approval[]>([])
  const [jobMap, setJobMap] = useState<Record<string, string>>({})
  const [totp, setTotp] = useState('')
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [totpEnabled, setTotpEnabled] = useState(false)
  const [loading, setLoading] = useState(false)

  // 内存 TOTP 绑定弹窗状态（纯内存 Canvas 渲染，不持久化、不落盘缓存）
  // 内存 TOTP 绑定弹窗状态（纯内存 Canvas 渲染，不持久化、不落盘缓存）
  const [enrollSecret, setEnrollSecret] = useState<string | null>(null)
  const [qrCodeDataUrl, setQrCodeDataUrl] = useState<string>('')
  const [copied, setCopied] = useState(false)
  const [activationTotp, setActivationTotp] = useState('')
  const [activationErr, setActivationErr] = useState('')

  // 安全防越权校验弹窗状态
  const [enrollModalOpen, setEnrollModalOpen] = useState(false)
  const [reEnrollModalOpen, setReEnrollModalOpen] = useState(false)
  const [disableModalOpen, setDisableModalOpen] = useState(false)

  // 严格拆分：管理员登录密码 与 当前 TOTP 动态口令
  const [adminPassword, setAdminPassword] = useState('')
  const [currentTotp, setCurrentTotp] = useState('')
  const [modalErr, setModalErr] = useState('')
  const [actionLoading, setActionLoading] = useState(false)

  const reload = () => {
    setLoading(true)
    Promise.all([
      apiGet<{ items: Approval[] }>('/approvals').catch((e: Error) => {
        setErr(e.message)
        return { items: [] as Approval[] }
      }),
      apiGet<{ items: JobItem[] }>('/jobs').catch(() => ({ items: [] as JobItem[] })),
      apiGet<{ enabled: boolean }>('/auth/totp').catch(() => ({ enabled: false })),
    ])
      .then(([appData, jobsData, totpData]) => {
        setItems(appData.items ?? [])
        setJobMap(Object.fromEntries((jobsData.items ?? []).map((j) => [j.id, j.prompt])))
        setTotpEnabled(!!totpData.enabled)
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    reload()
  }, [])

  // 1. 首次开通：输入密码后生成二维码
  const doEnrollInitial = async () => {
    setModalErr('')
    setActionLoading(true)
    try {
      const r = await apiPost<{ secret: string }>('/auth/totp/enroll', {
        password: adminPassword.trim(),
      })
      const sec = r.secret
      setEnrollSecret(sec)
      setActivationTotp('')
      setActivationErr('')

      const totpUri = `otpauth://totp/AIEmployee:admin?secret=${sec}&issuer=AIEmployee&period=30&digits=6`
      const dataUrl = await QRCode.toDataURL(totpUri, {
        width: 220,
        margin: 2,
        color: { dark: '#0f172a', light: '#ffffff' },
      })
      setQrCodeDataUrl(dataUrl)
      setEnrollModalOpen(false)
      setAdminPassword('')
    } catch (e) {
      setModalErr((e as Error).message)
    } finally {
      setActionLoading(false)
    }
  }

  // 2. 扫码后输入动态码激活确认
  const doConfirmActivation = async () => {
    setActivationErr('')
    setActionLoading(true)
    try {
      await apiPost('/auth/totp/confirm', {
        totp: activationTotp.trim(),
      })
      setMsg('🎉 TOTP 双因子认证已成功验证口令并正式激活生效！')
      setEnrollSecret(null)
      setQrCodeDataUrl('')
      setActivationTotp('')
      reload()
    } catch (e) {
      setActivationErr((e as Error).message)
    } finally {
      setActionLoading(false)
    }
  }

  // 3. 已开启状态下重新生成：同时输入密码与当前动态码
  const doReEnroll = async () => {
    setModalErr('')
    setActionLoading(true)
    try {
      const r = await apiPost<{ secret: string }>('/auth/totp/enroll', {
        password: adminPassword.trim(),
        totp: currentTotp.trim(),
      })
      const sec = r.secret
      setEnrollSecret(sec)
      setActivationTotp('')
      setActivationErr('')

      const totpUri = `otpauth://totp/AIEmployee:admin?secret=${sec}&issuer=AIEmployee&period=30&digits=6`
      const dataUrl = await QRCode.toDataURL(totpUri, {
        width: 220,
        margin: 2,
        color: { dark: '#0f172a', light: '#ffffff' },
      })
      setQrCodeDataUrl(dataUrl)
      setMsg('身份核验通过，已生成新秘钥与二维码。请使用手机 App 扫码并输入新口令完成激活')
      setReEnrollModalOpen(false)
      setAdminPassword('')
      setCurrentTotp('')
    } catch (e) {
      setModalErr((e as Error).message)
    } finally {
      setActionLoading(false)
    }
  }

  // 4. 关闭 TOTP：同时输入密码与当前动态码
  const doDisable = async () => {
    setModalErr('')
    setActionLoading(true)
    try {
      await apiPost('/auth/totp/disable', {
        password: adminPassword.trim(),
        totp: currentTotp.trim(),
      })
      setMsg('TOTP 双因子认证已成功安全关闭')
      setDisableModalOpen(false)
      setAdminPassword('')
      setCurrentTotp('')
      setEnrollSecret(null)
      setQrCodeDataUrl('')
      reload()
    } catch (e) {
      setModalErr((e as Error).message)
    } finally {
      setActionLoading(false)
    }
  }

  const act = async (id: string, kind: 'approve' | 'reject') => {
    setErr('')
    setMsg('')
    try {
      if (kind === 'approve') {
        await apiPost(`/approvals/${id}/approve`, { totp })
        setMsg('审批单已同意通过，任务将恢复执行')
      } else {
        await apiPost(`/approvals/${id}/reject`)
        setMsg('审批单已驳回，任务将终止执行')
      }
      setTotp('')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const copySecret = () => {
    if (!enrollSecret) return
    navigator.clipboard.writeText(enrollSecret).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>人工审批管控中心 (Approvals)</h1>
          <p>针对命中 ASK 策略、高风险高危指令及生产变更触发严格人工双因子鉴权审批</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => reload()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新单据</span>
        </button>
      </header>

      {/* 顶部功能描述与业务架构指引卡片 */}
      <PageFeatureGuide
        title="人工审批管控中心业务功能与风控流转架构指引"
        summary="AI 员工在自主执行复杂任务时，遇到高风险指令、越权操作或破坏性变更将自动被挂起（PAUSED / BLOCKED），转入此人工管控中心等待授权通过。"
        steps={[
          {
            step: '1',
            title: '动态风控触发 (ASK 策略)',
            desc: '权限引擎检测到危险命令（如删除数据库、推送生产仓库、调用大额资金接口）时，自动阻断下发并在此生成待审批工单。',
            tag: '自动挂起',
          },
          {
            step: '2',
            title: '双因子鉴权 (TOTP 动态口令)',
            desc: '关键或高危操作（CRITICAL）需验证 Google Authenticator / 腾讯身份验证器等动态口令，杜绝越权误批。',
            tag: '双因子安全',
          },
          {
            step: '3',
            title: '审批单审核流转决策',
            desc: '点击「批准放行」将向工作站下发继续执行信号；点击「驳回阻断」将直接终止任务并向飞书协同群回传驳回报告。',
            tag: '闭环反馈',
          },
          {
            step: '4',
            title: '审计存证与合规可溯',
            desc: '所有审批人的决策时间、客户端 IP、放行理由和 TOTP 认证结果均被写入不可篡改的操作审计日志。',
            tag: '审计追溯',
          },
        ]}
      />

      {err ? <div className="error">{err}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      {/* 内存 TOTP 绑定向导弹窗（内存渲染二维码，不缓存图片文件） */}
      {enrollSecret ? (
        <div
          className="panel"
          style={{
            border: '2px solid var(--brand-500)',
            background: '#ffffff',
            boxShadow: 'var(--shadow-lg)',
            marginBottom: '1.5rem',
            padding: '1.25rem 1.5rem',
            borderRadius: '8px',
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', borderBottom: '1px solid #e2e8f0', paddingBottom: '0.75rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <IconKey size={20} style={{ color: 'var(--brand-600)' }} />
              <h3 style={{ margin: 0, fontSize: '1.1rem', color: 'var(--text-primary)' }}>
                绑定双因子身份验证器 (TOTP)
              </h3>
            </div>
            <button
              type="button"
              className="btn-ghost btn-sm"
              onClick={() => setEnrollSecret(null)}
              title="关闭向导"
            >
              关闭 ✕
            </button>
          </div>

          <div style={{ display: 'flex', gap: '2rem', flexWrap: 'wrap', alignItems: 'center' }}>
            {/* 纯内存 Canvas 生成的二维码，绝不缓存落盘 */}
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                background: '#f8fafc',
                padding: '1rem',
                borderRadius: '8px',
                border: '1px solid #e2e8f0',
              }}
            >
              {qrCodeDataUrl ? (
                <img
                  src={qrCodeDataUrl}
                  alt="TOTP 二维码"
                  style={{ width: '200px', height: '200px', display: 'block', borderRadius: '4px' }}
                />
              ) : (
                <div style={{ width: '200px', height: '200px', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#94a3b8' }}>
                  生成二维码中...
                </div>
              )}
              <span style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.5rem' }}>
                （内存动态渲染 · 不落盘、无文件缓存）
              </span>
            </div>

            {/* 密钥与操作说明 */}
            <div style={{ flex: 1, minWidth: '280px' }}>
              <h4 style={{ margin: '0 0 0.5rem', fontSize: '0.95rem', color: 'var(--text-primary)' }}>
                请打开手机 Authenticator App 扫码绑定
              </h4>
              <p style={{ fontSize: '0.84rem', color: 'var(--text-secondary)', margin: '0 0 0.8rem', lineHeight: 1.6 }}>
                支持 <strong>Google Authenticator</strong>、<strong>Microsoft Authenticator</strong>、<strong>腾讯身份验证器</strong> 或任何标准 RFC 6238 TOTP 应用。
              </p>

              <div style={{ background: '#f1f5f9', padding: '0.75rem 1rem', borderRadius: '6px', marginBottom: '0.8rem', border: '1px solid #cbd5e1' }}>
                <div style={{ fontSize: '0.78rem', color: '#64748b', marginBottom: '0.3rem' }}>无法扫码？可手动输入 Secret 密钥：</div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <code style={{ fontSize: '0.95rem', fontWeight: 700, letterSpacing: '0.08em', color: 'var(--brand-700)', flex: 1 }}>
                    {enrollSecret}
                  </code>
                  <button
                    type="button"
                    className="btn-sm"
                    onClick={copySecret}
                    style={{ fontSize: '0.75rem', padding: '0.25rem 0.6rem' }}
                  >
                    {copied ? '已复制 ✓' : '复制密钥'}
                  </button>
                </div>
              </div>

              <div style={{ fontSize: '0.8rem', color: '#ef4444', lineHeight: 1.5 }}>
                ⚠️ 提示：密钥和二维码仅在此次展示，关闭后将不再回显。
              </div>

              {/* 激活确认输入区域：输入扫码后的 TOTP 动态口令以正式激活 */}
              <div style={{ marginTop: '1.25rem', paddingTop: '1rem', borderTop: '1px dashed #cbd5e1' }}>
                <h4 style={{ margin: '0 0 0.4rem', fontSize: '0.92rem', color: 'var(--text-primary)' }}>
                  扫码完成？请输入手机 App 生成的 6 位动态口令进行首次激活：
                </h4>
                <p style={{ fontSize: '0.82rem', color: 'var(--text-secondary)', margin: '0 0 0.6rem', lineHeight: 1.5 }}>
                  确认手机时间准确与 App 绑定成功后，输入口令并点击“验证口令并完成开通”，TOTP 双因子认证即刻正式生效。
                </p>
                {activationErr ? (
                  <div
                    style={{
                      background: '#fef2f2',
                      border: '1px solid #fca5a5',
                      color: '#dc2626',
                      padding: '0.5rem 0.75rem',
                      borderRadius: '6px',
                      fontSize: '0.82rem',
                      fontWeight: 600,
                      marginBottom: '0.6rem',
                      display: 'flex',
                      alignItems: 'center',
                      gap: '0.4rem',
                    }}
                  >
                    <IconAlertTriangle size={16} />
                    <span>{activationErr}</span>
                  </div>
                ) : null}
                <div style={{ display: 'flex', gap: '0.6rem', alignItems: 'center', flexWrap: 'wrap' }}>
                  <input
                    type="text"
                    placeholder="输入 6 位动态口令"
                    value={activationTotp}
                    onChange={(e) => setActivationTotp(e.target.value)}
                    maxLength={6}
                    style={{ width: '180px', textAlign: 'center', fontSize: '1.05rem', letterSpacing: '2px', fontWeight: 700 }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && activationTotp.trim().length === 6) void doConfirmActivation()
                    }}
                  />
                  <button
                    type="button"
                    className="btn-primary"
                    disabled={actionLoading || activationTotp.trim().length !== 6}
                    onClick={() => void doConfirmActivation()}
                  >
                    {actionLoading ? '正在校验激活...' : '验证口令并完成开通'}
                  </button>
                  <button
                    type="button"
                    className="btn-ghost btn-sm"
                    onClick={() => {
                      setEnrollSecret(null)
                      setQrCodeDataUrl('')
                      setActivationTotp('')
                      setActivationErr('')
                    }}
                  >
                    稍后激活
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>双因子安全认证 (TOTP) 状态</h2>
            <p>高风险指令批准需强制输入 6 位动态口令，防止未授权越权操作</p>
          </div>
          <div>
            {totpEnabled ? (
              <div style={{ display: 'flex', gap: '0.6rem', alignItems: 'center' }}>
                <span className="badge badge-ok">TOTP 双因子已生效</span>
                <button
                  type="button"
                  className="btn-ghost btn-sm"
                  onClick={() => {
                    setAdminPassword('')
                    setCurrentTotp('')
                    setModalErr('')
                    setReEnrollModalOpen(true)
                  }}
                >
                  <IconKey size={14} />
                  <span>重新生成绑定二维码</span>
                </button>
                <button
                  type="button"
                  className="btn-ghost btn-sm"
                  style={{ color: '#dc2626', borderColor: '#fca5a5' }}
                  onClick={() => {
                    setAdminPassword('')
                    setCurrentTotp('')
                    setModalErr('')
                    setDisableModalOpen(true)
                  }}
                >
                  <IconShield size={14} />
                  <span>关闭 TOTP 认证</span>
                </button>
              </div>
            ) : (
              <button
                type="button"
                className="btn-ghost"
                onClick={() => {
                  setAdminPassword('')
                  setModalErr('')
                  setEnrollModalOpen(true)
                }}
              >
                <IconKey size={15} />
                <span>立即开通并生成 TOTP 绑定二维码</span>
              </button>
            )}
          </div>
        </div>

        {/* 首次开通 TOTP 身份验证弹窗 */}
        {enrollModalOpen ? (
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
                width: '460px',
                maxWidth: '92vw',
                boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1)',
                border: '1px solid #e2e8f0',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', marginBottom: '1rem' }}>
                <IconKey size={22} style={{ color: 'var(--brand-600)' }} />
                <h3 style={{ margin: 0, fontSize: '1.15rem', color: 'var(--text-primary)' }}>
                  安全核验：开通 TOTP 双因子认证
                </h3>
              </div>
              <p style={{ fontSize: '0.86rem', color: 'var(--text-secondary)', lineHeight: 1.6, margin: '0 0 1rem' }}>
                开通 TOTP 双因子认证前，请首先输入当前管理员登录密码进行身份核验：
              </p>
              {modalErr ? (
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
                  <span>{modalErr}</span>
                </div>
              ) : null}
              <div style={{ marginBottom: '1.25rem' }}>
                <label style={{ display: 'block', fontSize: '0.84rem', fontWeight: 600, marginBottom: '0.4rem', color: 'var(--text-primary)' }}>
                  管理员登录密码 (必填)
                </label>
                <input
                  autoFocus
                  type="password"
                  placeholder="请输入当前管理员登录密码"
                  value={adminPassword}
                  onChange={(e) => setAdminPassword(e.target.value)}
                  style={{ width: '100%', fontSize: '0.95rem', padding: '0.55rem' }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && adminPassword.trim().length > 0) void doEnrollInitial()
                  }}
                />
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
                <button
                  type="button"
                  className="btn-ghost"
                  onClick={() => {
                    setEnrollModalOpen(false)
                    setModalErr('')
                    setAdminPassword('')
                  }}
                  disabled={actionLoading}
                >
                  取消
                </button>
                <button
                  type="button"
                  style={{
                    background: 'var(--brand-600)',
                    color: '#ffffff',
                    borderRadius: '6px',
                    padding: '0.5rem 1rem',
                    border: 'none',
                    cursor: 'pointer',
                  }}
                  disabled={actionLoading || adminPassword.trim().length === 0}
                  onClick={() => void doEnrollInitial()}
                >
                  {actionLoading ? '正在核验...' : '验证密码并生成二维码'}
                </button>
              </div>
            </div>
          </div>
        ) : null}

        {/* 重新生成 TOTP 二次鉴权弹窗（同时输入密码与当前动态口令） */}
        {reEnrollModalOpen ? (
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
                width: '460px',
                maxWidth: '92vw',
                boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1)',
                border: '1px solid #e2e8f0',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', marginBottom: '1rem' }}>
                <IconKey size={22} style={{ color: 'var(--brand-600)' }} />
                <h3 style={{ margin: 0, fontSize: '1.15rem', color: 'var(--text-primary)' }}>
                  安全核验：重新生成绑定二维码
                </h3>
              </div>
              <p style={{ fontSize: '0.86rem', color: 'var(--text-secondary)', lineHeight: 1.6, margin: '0 0 1rem' }}>
                重新生成后，当前已绑定的 Authenticator 令牌将<strong>立即作废</strong>。为防止越权与误触，必须<strong>同时输入管理员登录密码与当前 6 位动态口令</strong>进行双重身份核验：
              </p>
              {modalErr ? (
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
                  <span>{modalErr}</span>
                </div>
              ) : null}
              <div style={{ marginBottom: '1rem' }}>
                <label style={{ display: 'block', fontSize: '0.84rem', fontWeight: 600, marginBottom: '0.4rem', color: 'var(--text-primary)' }}>
                  1. 管理员登录密码 (必填)
                </label>
                <input
                  autoFocus
                  type="password"
                  placeholder="请输入当前管理员登录密码"
                  value={adminPassword}
                  onChange={(e) => setAdminPassword(e.target.value)}
                  style={{ width: '100%', fontSize: '0.95rem', padding: '0.55rem' }}
                />
              </div>
              <div style={{ marginBottom: '1.25rem' }}>
                <label style={{ display: 'block', fontSize: '0.84rem', fontWeight: 600, marginBottom: '0.4rem', color: 'var(--text-primary)' }}>
                  2. 当前 6 位 TOTP 动态口令 (必填)
                </label>
                <input
                  type="text"
                  placeholder="输入当前手机 Authenticator 中的 6 位口令"
                  value={currentTotp}
                  onChange={(e) => setCurrentTotp(e.target.value)}
                  maxLength={6}
                  style={{ width: '100%', fontSize: '1rem', textAlign: 'center', letterSpacing: '2px', fontWeight: 600, padding: '0.55rem' }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && adminPassword.trim() && currentTotp.trim().length === 6) void doReEnroll()
                  }}
                />
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
                <button
                  type="button"
                  className="btn-ghost"
                  onClick={() => {
                    setReEnrollModalOpen(false)
                    setModalErr('')
                    setAdminPassword('')
                    setCurrentTotp('')
                  }}
                  disabled={actionLoading}
                >
                  取消
                </button>
                <button
                  type="button"
                  style={{
                    background: 'var(--brand-600)',
                    color: '#ffffff',
                    borderRadius: '6px',
                    padding: '0.5rem 1rem',
                    border: 'none',
                    cursor: 'pointer',
                  }}
                  disabled={actionLoading || !adminPassword.trim() || currentTotp.trim().length !== 6}
                  onClick={() => void doReEnroll()}
                >
                  {actionLoading ? '正在核验...' : '双重核验并重新生成'}
                </button>
              </div>
            </div>
          </div>
        ) : null}

        {/* 关闭 TOTP 二次确认弹窗（同时输入密码与当前动态口令） */}
        {disableModalOpen ? (
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
                width: '460px',
                maxWidth: '92vw',
                boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1)',
                border: '1px solid #fee2e2',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', marginBottom: '1rem' }}>
                <IconAlertTriangle size={22} style={{ color: '#ef4444' }} />
                <h3 style={{ margin: 0, fontSize: '1.15rem', color: '#b91c1c' }}>
                  安全告警：关闭 TOTP 双因子认证
                </h3>
              </div>
              <p style={{ fontSize: '0.86rem', color: 'var(--text-secondary)', lineHeight: 1.6, margin: '0 0 1rem' }}>
                关闭 TOTP 双因子认证后，高危指令与生产变更审批将<strong>降级为单因子授权</strong>。为确认是管理员本人操作，必须<strong>同时输入管理员登录密码与当前 6 位动态口令</strong>确认关闭：
              </p>
              {modalErr ? (
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
                  <span>{modalErr}</span>
                </div>
              ) : null}
              <div style={{ marginBottom: '1rem' }}>
                <label style={{ display: 'block', fontSize: '0.84rem', fontWeight: 600, marginBottom: '0.4rem', color: 'var(--text-primary)' }}>
                  1. 管理员登录密码 (必填)
                </label>
                <input
                  autoFocus
                  type="password"
                  placeholder="请输入当前管理员登录密码"
                  value={adminPassword}
                  onChange={(e) => setAdminPassword(e.target.value)}
                  style={{ width: '100%', fontSize: '0.95rem', padding: '0.55rem' }}
                />
              </div>
              <div style={{ marginBottom: '1.25rem' }}>
                <label style={{ display: 'block', fontSize: '0.84rem', fontWeight: 600, marginBottom: '0.4rem', color: 'var(--text-primary)' }}>
                  2. 当前 6 位 TOTP 动态口令 (必填)
                </label>
                <input
                  type="text"
                  placeholder="输入当前手机 Authenticator 中的 6 位口令"
                  value={currentTotp}
                  onChange={(e) => setCurrentTotp(e.target.value)}
                  maxLength={6}
                  style={{ width: '100%', fontSize: '1rem', textAlign: 'center', letterSpacing: '2px', fontWeight: 600, padding: '0.55rem' }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && adminPassword.trim() && currentTotp.trim().length === 6) void doDisable()
                  }}
                />
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
                <button
                  type="button"
                  className="btn-ghost"
                  onClick={() => {
                    setDisableModalOpen(false)
                    setModalErr('')
                    setAdminPassword('')
                    setCurrentTotp('')
                  }}
                  disabled={actionLoading}
                >
                  取消
                </button>
                <button
                  type="button"
                  style={{
                    background: '#dc2626',
                    color: '#ffffff',
                    borderRadius: '6px',
                    padding: '0.5rem 1rem',
                    border: 'none',
                    cursor: 'pointer',
                  }}
                  disabled={actionLoading || !adminPassword.trim() || currentTotp.trim().length !== 6}
                  onClick={() => void doDisable()}
                >
                  {actionLoading ? '正在关闭...' : '双重核验并确认关闭'}
                </button>
              </div>
            </div>
          </div>
        ) : null}

        <div className="inline-form" style={{ background: '#f8fafc', padding: '1rem', borderRadius: '8px', border: '1px solid var(--border-subtle)' }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', fontSize: '0.86rem', fontWeight: 600 }}>
            <span>当前审批动态验证码 (TOTP):</span>
            <input
              placeholder="请输入 6 位动态口令"
              value={totp}
              onChange={(e) => setTotp(e.target.value)}
              style={{ width: '200px' }}
            />
          </label>
          <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>（若为普通非高危单据或未强制 TOTP，可留空直接批准）</span>
        </div>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>待办与历史审批工单</h2>
            <p>共查出 {items.length} 笔审批流记录</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>审批单据</th>
                <th>关联任务需求</th>
                <th>触发动作 (Action)</th>
                <th>风险等级</th>
                <th>申请原因与上下文</th>
                <th>申请时间</th>
                <th>审核决策操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.id}>
                  <td>
                    <EntityName name={`审批单 #${a.id.slice(0, 8)}`} id={a.id} />
                  </td>
                  <td style={{ maxWidth: '240px' }}>
                    <EntityName
                      name={jobMap[a.job_id] || `任务 #${a.job_id.slice(0, 8)}`}
                      id={a.job_id}
                      to={`/jobs/${a.job_id}`}
                    />
                  </td>
                  <td><span className="mono">{a.action}</span></td>
                  <td>
                    {a.critical ? <StatusBadge status="CRITICAL" /> : <span style={{ color: 'var(--text-muted)' }}>常规</span>}
                  </td>
                  <td style={{ maxWidth: '300px', color: 'var(--text-secondary)' }}>{a.reason || '无说明'}</td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {new Date(a.created_at).toLocaleString()}
                  </td>
                  <td>
                    {a.status === 'PENDING' ? (
                      <div style={{ display: 'flex', gap: '0.4rem' }}>
                        <button type="button" className="btn-sm" onClick={() => void act(a.id, 'approve')}>
                          <IconCheckCircle size={14} />
                          <span>批准放行</span>
                        </button>
                        <button type="button" className="btn-danger btn-sm" onClick={() => void act(a.id, 'reject')}>
                          驳回阻断
                        </button>
                      </div>
                    ) : (
                      <StatusBadge status={a.status} />
                    )}
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={7} className="empty-tip">当前暂无待处理审批单据</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
