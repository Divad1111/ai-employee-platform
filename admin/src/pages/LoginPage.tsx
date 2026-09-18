/**
 * 登录页。
 */
import { FormEvent, useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { apiPost } from '../api/client'
import { isAuthenticated, setSession } from '../stores/session'

type LoginResp = {
  token: string
  user: { id: string; username: string; roles: string[] }
}

export function LoginPage() {
  const nav = useNavigate()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('admin123')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  if (isAuthenticated()) {
    return <Navigate to="/" replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      const data = await apiPost<LoginResp>('/auth/login', { username, password })
      setSession(data.token, data.user)
      nav('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={onSubmit}>
        <h1>AI Employee</h1>
        <p className="muted">Control Plane Admin</p>
        <label>
          用户名
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
        </label>
        <label>
          密码
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </label>
        {error ? <p className="error">{error}</p> : null}
        <button type="submit" disabled={loading}>
          {loading ? '登录中…' : '登录'}
        </button>
      </form>
    </div>
  )
}
