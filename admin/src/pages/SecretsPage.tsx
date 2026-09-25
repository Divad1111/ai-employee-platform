import { useEffect, useState } from 'react'
import { apiGet, apiPost, apiDelete } from '../api/client'
import { IconKey, IconPlus, IconRefresh, IconAlertTriangle } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { PageFeatureGuide } from '../components/PageFeatureGuide'
import { formatDateTime } from '../lib/time'

type SecretMeta = {
  id: string
  name: string
  masked: string
  description?: string
  created_at?: string
}

export function SecretsPage() {
  const [items, setItems] = useState<SecretMeta[]>([])
  const [name, setName] = useState('')
  const [value, setValue] = useState('')
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [loading, setLoading] = useState(false)
  const [totpEnabled, setTotpEnabled] = useState(false)

  // 销毁凭证高危操作模态弹窗状态
  const [deleteTarget, setDeleteTarget] = useState<SecretMeta | null>(null)
  const [adminPassword, setAdminPassword] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [modalErr, setModalErr] = useState('')
  const [actionLoading, setActionLoading] = useState(false)

  const reload = () => {
    setLoading(true)
    Promise.all([
      apiGet<{ items: SecretMeta[] }>('/secrets').catch((e: Error) => {
        setErr(e.message)
        return { items: [] as SecretMeta[] }
      }),
      apiGet<{ enabled: boolean }>('/auth/totp').catch(() => ({ enabled: false })),
    ])
      .then(([secretsData, totpData]) => {
        setItems(secretsData.items ?? [])
        setTotpEnabled(!!totpData.enabled)
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    reload()
  }, [])

  const create = async () => {
    setErr('')
    setMsg('')
    try {
      await apiPost('/secrets', { name, value })
      setName('')
      setValue('')
      setMsg('机密凭证已加密存入保管箱（明文仅提交一次，列表严禁明文回显）')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const openDeleteModal = (secret: SecretMeta) => {
    setDeleteTarget(secret)
    setAdminPassword('')
    setTotpCode('')
    setModalErr('')
  }

  const doDelete = async () => {
    if (!deleteTarget) return
    setModalErr('')
    setActionLoading(true)
    try {
      const payload: { password: string; totp?: string } = {
        password: adminPassword.trim(),
      }
      if (totpEnabled || totpCode.trim()) {
        payload.totp = totpCode.trim()
      }
      // 1. 先进行 step-up 二次身份核验提权
      await apiPost('/auth/step-up', payload)
      // 2. 提权成功后，执行凭证物理销毁
      await apiDelete(`/secrets/${deleteTarget.id}`)
      setMsg(`机密凭证 [${deleteTarget.name}] (${deleteTarget.id}) 已成功安全销毁并物理抹除`)
      setDeleteTarget(null)
      setAdminPassword('')
      setTotpCode('')
      reload()
    } catch (e) {
      setModalErr((e as Error).message)
    } finally {
      setActionLoading(false)
    }
  }

  const renderBadge = (secretName: string) => {
    if (secretName.startsWith('feishu.')) {
      return (
        <span
          className="badge badge-info"
          style={{ marginLeft: '0.45rem', fontSize: '0.72rem', padding: '0.15rem 0.45rem' }}
          title="系统飞书集成模块自动托管与维护的凭证"
        >
          飞书集成
        </span>
      )
    }
    if (secretName.startsWith('totp.')) {
      return (
        <span
          className="badge badge-muted"
          style={{ marginLeft: '0.45rem', fontSize: '0.72rem', padding: '0.15rem 0.45rem' }}
          title="历史 TOTP 密钥引用（现已升级至 Postgres AES-256-GCM 独立持久化存储）"
        >
          历史 TOTP
        </span>
      )
    }
    return (
      <span
        className="badge badge-ok"
        style={{ marginLeft: '0.45rem', fontSize: '0.72rem', padding: '0.15rem 0.45rem' }}
        title="用户自定义业务凭证"
      >
        业务凭证
      </span>
    )
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>机密凭证保管箱 (Secret Vault)</h1>
          <p>存储 API Key、Token 与第三方凭证 · 采用引用绑定，明文严格不出管控面</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => reload()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新保管箱</span>
        </button>
      </header>

      <PageFeatureGuide
        title="机密保管箱凭证脱敏与零泄露架构指引"
        summary="为数字员工对接大模型 API、飞书凭据、Git 访问 Token 提供统一机密保险库，实现明文严密物理隔离。"
        steps={[
          {
            step: '1',
            title: '单向写入与硬件加密落盘',
            desc: '录入密钥时直接采用 AES-GCM 高强度信封加密存储，明文仅接收一次，随后立即从内存销毁。',
            tag: '信封加密',
          },
          {
            step: '2',
            title: '引用绑定 (Reference Only)',
            desc: '对外全部暴露抽象引用 ID（如 sec_ref_01），工作站与日志中绝不传输真实明文口令。',
            tag: '引用穿透',
          },
          {
            step: '3',
            title: '关键动作二次密码校验 (Step-Up)',
            desc: '敏感操作（如删除/销毁凭证）强制弹窗二次校验管理员密码，防止误触与越权破坏。',
            tag: '二次提权',
          },
        ]}
      />

      {err ? <div className="error">{err}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>录入新加密凭证</h2>
            <p>凭证保存后将自动脱敏掩码，任何终端均无法通过 API 获取明文</p>
          </div>
        </div>
        <form
          className="inline-form"
          onSubmit={(e) => {
            e.preventDefault()
            void create()
          }}
        >
          <input
            placeholder="凭证键名 (例: FEISHU_APP_SECRET)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            style={{ width: '260px' }}
            required
          />
          <input
            type="password"
            placeholder="机密明文内容"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            style={{ width: '320px' }}
            required
          />
          <button type="submit">
            <IconPlus size={15} />
            <span>安全加密存入</span>
          </button>
        </form>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>当前托管凭证列表</h2>
            <p>共保全 {items.length} 组机密凭证</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>凭证引用名称 (Name)</th>
                <th>凭证类别</th>
                <th>掩码显示内容 (Masked)</th>
                <th>录入时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id}>
                  <td>
                    <EntityName
                      name={s.name}
                      id={s.id}
                      icon={<IconKey size={15} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td>{renderBadge(s.name)}</td>
                  <td>
                    <span className="mono" style={{ background: '#fef3c7', color: '#92400e' }}>
                      {s.masked}
                    </span>
                  </td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {s.created_at ? formatDateTime(s.created_at) : '—'}
                  </td>
                  <td>
                    <button
                      type="button"
                      className="btn-danger btn-sm"
                      onClick={() => openDeleteModal(s)}
                    >
                      销毁凭证
                    </button>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={5} className="empty-tip">
                    保管箱内暂无凭证记录
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      {/* 销毁机密凭证高危安全核验弹窗（对标 TOTP 弹窗质感） */}
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
                安全告警：销毁机密凭证
              </h3>
            </div>

            <p style={{ fontSize: '0.86rem', color: 'var(--text-secondary)', lineHeight: 1.6, margin: '0 0 1rem' }}>
              销毁机密凭证是<strong>不可逆高危操作</strong>，凭证内容将从加密保管箱中<strong>永久物理抹除</strong>。任何已绑定该凭证的数字员工或集成模块将无法再解密使用。为确认是管理员本人操作，请输入二次身份核验凭据：
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
                <strong>待销毁凭证：</strong>
                <code style={{ color: '#b91c1c', fontWeight: 700 }}>{deleteTarget.name}</code>
              </div>
              <div style={{ color: '#7f1d1d', fontSize: '0.78rem', fontFamily: 'monospace' }}>
                引用 ID: {deleteTarget.id}
              </div>
            </div>

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
              <label
                style={{
                  display: 'block',
                  fontSize: '0.84rem',
                  fontWeight: 600,
                  marginBottom: '0.4rem',
                  color: 'var(--text-primary)',
                }}
              >
                1. 管理员登录密码 (必填)
              </label>
              <input
                autoFocus
                type="password"
                placeholder="请输入当前管理员登录密码"
                value={adminPassword}
                onChange={(e) => setAdminPassword(e.target.value)}
                style={{ width: '100%', fontSize: '0.95rem', padding: '0.55rem' }}
                onKeyDown={(e) => {
                  if (
                    e.key === 'Enter' &&
                    adminPassword.trim() &&
                    (!totpEnabled || totpCode.trim().length === 6)
                  ) {
                    void doDelete()
                  }
                }}
              />
            </div>

            {totpEnabled ? (
              <div style={{ marginBottom: '1.25rem' }}>
                <label
                  style={{
                    display: 'block',
                    fontSize: '0.84rem',
                    fontWeight: 600,
                    marginBottom: '0.4rem',
                    color: 'var(--text-primary)',
                  }}
                >
                  2. 当前 6 位 TOTP 动态口令 (必填)
                </label>
                <input
                  type="text"
                  placeholder="输入当前手机 Authenticator 中的 6 位口令"
                  value={totpCode}
                  onChange={(e) => setTotpCode(e.target.value)}
                  maxLength={6}
                  style={{
                    width: '100%',
                    fontSize: '1rem',
                    textAlign: 'center',
                    letterSpacing: '2px',
                    fontWeight: 600,
                    padding: '0.55rem',
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && adminPassword.trim() && totpCode.trim().length === 6) {
                      void doDelete()
                    }
                  }}
                />
              </div>
            ) : null}

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
              <button
                type="button"
                className="btn-ghost"
                onClick={() => {
                  setDeleteTarget(null)
                  setModalErr('')
                  setAdminPassword('')
                  setTotpCode('')
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
                disabled={
                  actionLoading ||
                  adminPassword.trim().length === 0 ||
                  (totpEnabled && totpCode.trim().length !== 6)
                }
                onClick={() => void doDelete()}
              >
                {actionLoading ? '正在销毁凭证...' : '确认安全销毁'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </section>
  )
}
