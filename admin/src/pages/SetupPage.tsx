/**
 * 首次部署设置向导：现代化科技风格全中文初始化页面。
 * 仅在空库首次安装时可用，创建超级管理员后永久关闭。
 */
import { FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { isAuthenticated, setSession } from '../stores/session'

type SetupStatusResp = {
  initialized: boolean
  needs_setup: boolean
  version?: string
}

type SetupInitResp = {
  status: string
  token: string
  expires_at: string
  system_name?: string
  user: {
    id: string
    username: string
    display_name?: string
    roles: string[]
  }
}

export function SetupPage() {
  const nav = useNavigate()
  const [checking, setChecking] = useState(true)
  const [alreadyInitialized, setAlreadyInitialized] = useState(false)

  const [systemName, setSystemName] = useState('AI Employee Platform')
  const [username, setUsername] = useState('admin')
  const [displayName, setDisplayName] = useState('系统超级管理员')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')

  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (isAuthenticated()) {
      nav('/', { replace: true })
      return
    }

    let active = true
    async function checkStatus() {
      try {
        const data = await apiGet<SetupStatusResp>('/setup/status')
        if (!active) return
        if (data.initialized && !data.needs_setup) {
          setAlreadyInitialized(true)
        }
      } catch {
        /* API 暂不可达时仍允许尝试初始化 */
      } finally {
        if (active) setChecking(false)
      }
    }
    checkStatus()
    return () => {
      active = false
    }
  }, [nav])

  const isLengthValid = password.length >= 8
  const isMatch = password.length > 0 && password === confirmPassword
  const hasMixedChars = /[A-Za-z]/.test(password) && /[0-9]/.test(password)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setSuccess('')

    if (username.trim().length < 3) {
      setError('管理员账号长度至少需 3 个字符')
      return
    }
    if (!isLengthValid) {
      setError('密码长度至少需 8 个字符')
      return
    }
    if (!isMatch) {
      setError('两次输入的密码不一致，请核对')
      return
    }

    setSubmitting(true)
    try {
      const res = await apiPost<SetupInitResp>('/setup/init', {
        username: username.trim(),
        password,
        display_name: displayName.trim() || '系统管理员',
        system_name: systemName.trim() || 'AI Employee Platform',
      })

      setSuccess('首次部署初始化成功！正在自动登录并进入管控中心…')
      setSession(res.token, res.user)

      setTimeout(() => {
        nav('/', { replace: true })
      }, 1200)
    } catch (err) {
      setError(err instanceof Error ? err.message : '首次部署初始化失败，请稍后重试')
      setSubmitting(false)
    }
  }

  if (checking) {
    return (
      <div className="login-page">
        <div className="login-card" style={{ textAlign: 'center', color: '#64748b' }}>
          <div className="login-badge">
            <span className="status-dot" style={{ background: '#3b82f6' }} />
            <span>AI Employee Platform</span>
          </div>
          <h2>正在检测系统部署状态…</h2>
        </div>
      </div>
    )
  }

  if (alreadyInitialized) {
    return (
      <div className="login-page">
        <div className="login-card" style={{ maxWidth: '440px', textAlign: 'center' }}>
          <div className="login-header">
            <div className="login-badge" style={{ background: '#ecfdf5', color: '#047857' }}>
              <span className="status-dot" style={{ background: '#10b981' }} />
              <span>系统已就绪 · 已完成首次部署</span>
            </div>
            <h1>无需重复初始化</h1>
            <p style={{ marginTop: '0.6rem' }}>
              本系统已配置超级管理员账号，首次部署向导通道已永久锁定。请直接使用管理员凭证登录。
            </p>
          </div>

          <div style={{ marginTop: '1rem' }}>
            <Link to="/login" style={{ textDecoration: 'none' }}>
              <button type="button" style={{ width: '100%', padding: '0.75rem' }}>
                前往登录中心
              </button>
            </Link>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="login-page" style={{ padding: '2rem 1rem' }}>
      <form
        className="login-card"
        style={{ maxWidth: '500px', padding: '2.5rem 2.2rem' }}
        onSubmit={onSubmit}
      >
        <div className="login-header">
          <div className="login-badge">
            <span className="status-dot" style={{ background: '#10b981' }} />
            <span>首次部署向导 · Setup Wizard</span>
          </div>
          <h1>系统初始化设置</h1>
          <p>
            欢迎部署 AI Employee 数字员工管控中心。检测到系统尚未配置超级管理员，请完成首次设置。
          </p>
        </div>

        <div
          style={{
            background: '#f8fafc',
            border: '1px solid var(--border-subtle)',
            borderRadius: '8px',
            padding: '0.75rem 1rem',
            fontSize: '0.8rem',
            color: '#475569',
            lineHeight: 1.5,
          }}
        >
          <strong style={{ color: '#0f172a' }}>安全提示：</strong>
          此向导仅在空库初次部署时开放一次，创建成功后将永久关闭该接口以保障平台安全。
        </div>

        <label style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
          <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#334155' }}>
            平台/实例名称
          </span>
          <input
            placeholder="例如：AI Employee 数字员工平台"
            value={systemName}
            onChange={(e) => setSystemName(e.target.value)}
          />
        </label>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
            <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#334155' }}>
              超级管理员账号 <span style={{ color: '#ef4444' }}>*</span>
            </span>
            <input
              placeholder="如 admin"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              required
              minLength={3}
            />
          </label>

          <label style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
            <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#334155' }}>
              管理员昵称/姓名 <span style={{ color: '#ef4444' }}>*</span>
            </span>
            <input
              placeholder="如 系统管理员"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              required
            />
          </label>
        </div>

        <label style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
          <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#334155' }}>
            设置管理员登录密码 <span style={{ color: '#ef4444' }}>*</span>
          </span>
          <input
            type="password"
            placeholder="请输入高强度密码 (至少8位)"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
          />
        </label>

        <label style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
          <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#334155' }}>
            确认登录密码 <span style={{ color: '#ef4444' }}>*</span>
          </span>
          <input
            type="password"
            placeholder="请再次输入上方设定的密码"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            autoComplete="new-password"
            required
          />
        </label>

        {password ? (
          <div
            style={{
              display: 'flex',
              gap: '1rem',
              fontSize: '0.76rem',
              color: '#64748b',
              marginTop: '-0.4rem',
            }}
          >
            <span style={{ color: isLengthValid ? '#10b981' : '#94a3b8' }}>
              {isLengthValid ? '✓' : '○'} 长度至少 8 位
            </span>
            <span style={{ color: hasMixedChars ? '#10b981' : '#94a3b8' }}>
              {hasMixedChars ? '✓' : '○'} 包含字母与数字
            </span>
            {confirmPassword ? (
              <span style={{ color: isMatch ? '#10b981' : '#ef4444' }}>
                {isMatch ? '✓ 密码一致' : '✕ 密码不一致'}
              </span>
            ) : null}
          </div>
        ) : null}

        {error ? <div className="error">{error}</div> : null}
        {success ? (
          <div
            style={{
              background: '#ecfdf5',
              border: '1px solid #a7f3d0',
              color: '#065f46',
              padding: '0.75rem',
              borderRadius: '6px',
              fontSize: '0.85rem',
              textAlign: 'center',
            }}
          >
            {success}
          </div>
        ) : null}

        <button
          type="submit"
          disabled={submitting || !isLengthValid || !isMatch}
          style={{
            marginTop: '0.5rem',
            width: '100%',
            padding: '0.8rem',
            fontWeight: 700,
            fontSize: '0.95rem',
          }}
        >
          {submitting ? '正在完成系统初始化…' : '完成初始化并进入控制台'}
        </button>

        <div style={{ textAlign: 'center', fontSize: '0.82rem', color: '#64748b' }}>
          已有已初始化的账号？ <Link to="/login" style={{ color: 'var(--brand-600)' }}>直接登录</Link>
        </div>
      </form>
    </div>
  )
}
