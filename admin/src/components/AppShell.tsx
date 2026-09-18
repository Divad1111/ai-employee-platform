/**
 * 管理后台布局壳：侧栏导航 + 内容区。
 */
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { apiPost } from '../api/client'
import { clearSession } from '../stores/session'

/** 侧栏顺序对齐设计文档 §8；Permissions 在 Approvals 前 */
const navItems = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/employees', label: 'Employees' },
  { to: '/workstations', label: 'Workstations' },
  { to: '/jobs', label: 'Jobs' },
  { to: '/sessions', label: 'Sessions' },
  { to: '/feishu', label: 'Feishu' },
  { to: '/skills', label: 'Skills' },
  { to: '/knowledge', label: 'Knowledge' },
  { to: '/permissions', label: 'Permissions' },
  { to: '/approvals', label: 'Approvals' },
  { to: '/secrets', label: 'Secrets' },
  { to: '/artifacts', label: 'Artifacts' },
  { to: '/audit', label: 'Audit' },
  { to: '/settings', label: 'Settings' },
]

export function AppShell() {
  const nav = useNavigate()
  async function logout() {
    try {
      await apiPost('/auth/logout')
    } catch {
      /* ignore */
    }
    clearSession()
    nav('/login', { replace: true })
  }
  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">AI Employee</div>
        <nav>
          {navItems.map((item) => (
            <NavLink key={item.to} to={item.to} end={item.end} className="nav-item">
              {item.label}
            </NavLink>
          ))}
        </nav>
        <button type="button" className="logout" onClick={() => void logout()}>
          退出
        </button>
      </aside>
      <main className="content">
        <Outlet />
      </main>
    </div>
  )
}
