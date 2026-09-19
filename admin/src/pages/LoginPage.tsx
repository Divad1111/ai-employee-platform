/**
 * 登录页：现代化全屏科技质感设计。
 */
import { FormEvent, useEffect, useState } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { isAuthenticated, setSession } from '../stores/session'

type LoginResp = {
  token: string
  user: { id: string; username: string; roles: string[] }
}

export function LoginPage() {
  const nav = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [totp, setTotp] = useState('')
  const [showTotp, setShowTotp] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (isAuthenticated()) return
    let active = true
    apiGet<{ needs_setup?: boolean }>('/setup/status')
      .then((data) => {
        if (active && data.needs_setup) {
          nav('/setup', { replace: true })
        }
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [nav])

  if (isAuthenticated()) {
    return <Navigate to="/" replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      const data = await apiPost<LoginResp>('/auth/login', {
        username,
        password,
        ...(totp ? { totp } : {}),
      })
      setSession(data.token, data.user)
      nav('/', { replace: true })
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : '登录认证失败，请检查账号密码'
      setError(errMsg)
      if (errMsg.includes('TOTP') || errMsg.includes('动态验证码') || errMsg.includes('双因子')) {
        setShowTotp(true)
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={onSubmit}>
        <div className="login-header">
          <div className="login-badge">
            <span className="status-dot" style={{ background: '#10b981' }} />
            <span>AI Employee Platform · Control Plane</span>
          </div>
          <h1>系统管理员登录</h1>
          <p>数字员工集中管控中心 · 安全鉴权入口</p>
        </div>

        <label style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
          <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#334155' }}>管理员账号</span>
          <input
            placeholder="请输入管理员账号"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            required
          />
        </label>

        <label style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
          <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#334155' }}>登录密码</span>
          <input
            type="password"
            placeholder="请输入管理员密码"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#334155' }}>动态安全验证码 (TOTP)</span>
            {!showTotp && (
              <button
                type="button"
                className="btn-ghost btn-sm"
                style={{ padding: 0, fontSize: '0.78rem', color: 'var(--brand-600)', height: 'auto' }}
                onClick={() => setShowTotp(true)}
              >
                + 展开 TOTP 验证码
              </button>
            )}
          </div>
          {showTotp && (
            <input
              placeholder="请输入 6 位动态验证码 (Authenticator)"
              value={totp}
              onChange={(e) => setTotp(e.target.value)}
              maxLength={6}
              autoFocus
              style={{ letterSpacing: '0.15em', fontWeight: 600 }}
            />
          )}
        </div>

        {error ? <div className="error">{error}</div> : null}

        <button type="submit" disabled={loading} style={{ marginTop: '0.5rem', width: '100%', padding: '0.7rem' }}>
          {loading ? '正在鉴权登录中…' : '立即登录'}
        </button>

        <div style={{ textAlign: 'center', fontSize: '0.82rem', color: '#94a3b8', marginTop: '0.5rem' }}>
          首次部署系统？ <Link to="/setup" style={{ color: 'var(--brand-600)', textDecoration: 'none' }}>前往首次设置向导</Link>
        </div>
      </form>
    </div>
  )
}
