/**
 * 管理后台布局壳：深色侧栏 + 顶部面包屑状态栏 + 弹性自适应内容区。
 */
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { apiPost } from '../api/client'
import { clearSession, getUser } from '../stores/session'
import {
  IconDashboard,
  IconUsers,
  IconServer,
  IconJobs,
  IconTerminal,
  IconMessage,
  IconZap,
  IconBook,
  IconShield,
  IconCheckCircle,
  IconKey,
  IconPackage,
  IconFileText,
  IconSettings,
  IconLogOut,
  IconFolder,
} from './Icons'

type NavItem = {
  to: string
  label: string
  icon: React.ComponentType<{ size?: number; className?: string; style?: React.CSSProperties }>
  end?: boolean
}

type NavSection = {
  title: string
  items: NavItem[]
}

/** 侧栏导航分类与结构化映射 */
const navSections: NavSection[] = [
  {
    title: '全景与监控',
    items: [
      { to: '/', label: '监控控制台', icon: IconDashboard, end: true },
      { to: '/sessions', label: '运行会话', icon: IconTerminal },
    ],
  },
  {
    title: '员工与计算算力',
    items: [
      { to: '/employees', label: '数字员工', icon: IconUsers },
      { to: '/workstations', label: '工作站节点', icon: IconServer },
      { to: '/workspaces', label: '项目工作区', icon: IconFolder },
    ],
  },
  {
    title: '任务调度流转',
    items: [
      { to: '/jobs', label: '任务流转中心', icon: IconJobs },
      { to: '/artifacts', label: '任务制品产物', icon: IconPackage },
      { to: '/approvals', label: '人工审批中心', icon: IconCheckCircle },
    ],
  },
  {
    title: '能力与协同扩展',
    items: [
      { to: '/skills', label: '技能库目录', icon: IconZap },
      { to: '/knowledge', label: '知识库文档', icon: IconBook },
      { to: '/feishu', label: '飞书应用协同', icon: IconMessage },
    ],
  },
  {
    title: '安全治理与系统',
    items: [
      { to: '/permissions', label: '权限策略引擎', icon: IconShield },
      { to: '/secrets', label: '机密凭证保管箱', icon: IconKey },
      { to: '/audit', label: '操作审计日志', icon: IconFileText },
      { to: '/settings', label: '系统架构配置', icon: IconSettings },
    ],
  },
]

const allNavItems = navSections.flatMap((s) => s.items)

export function AppShell() {
  const nav = useNavigate()
  const location = useLocation()
  const user = getUser()

  async function logout() {
    try {
      await apiPost('/auth/logout')
    } catch {
      /* ignore */
    }
    clearSession()
    nav('/login', { replace: true })
  }

  // 计算当前面包屑与页面标题
  const activeNav = allNavItems.find((n) =>
    n.end ? location.pathname === n.to : location.pathname.startsWith(n.to) && n.to !== '/'
  )
  const currentTitle = activeNav?.label || '控制中心'

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand-area">
          <div className="brand-icon">AI</div>
          <div className="brand-titles">
            <h2>AI Employee</h2>
            <p>数字员工智能管控中心</p>
          </div>
        </div>

        <nav className="nav-group">
          {navSections.map((sec) => (
            <div key={sec.title} className="nav-section">
              <div className="nav-section-title">{sec.title}</div>
              <div className="nav-section-items">
                {sec.items.map((item) => {
                  const Icon = item.icon
                  return (
                    <NavLink key={item.to} to={item.to} end={item.end} className="nav-item">
                      <Icon size={17} />
                      <span>{item.label}</span>
                    </NavLink>
                  )
                })}
              </div>
            </div>
          ))}
        </nav>

        <div className="sidebar-footer">
          <div className="user-profile">
            <div className="user-avatar">
              {(user?.username || 'A').slice(0, 1).toUpperCase()}
            </div>
            <div className="user-meta">
              <div className="name">{user?.username || '管理员'}</div>
              <div className="role">{user?.roles?.[0] || 'ADMIN'} · 系统在线</div>
            </div>
          </div>
          <button type="button" className="logout-btn" onClick={() => void logout()}>
            <IconLogOut size={15} />
            <span>退出登录</span>
          </button>
        </div>
      </aside>

      <div className="main-wrapper">
        <header className="topbar">
          <div className="topbar-breadcrumb">
            <span>平台主页</span>
            <span>/</span>
            <span className="current">{currentTitle}</span>
          </div>

          <div className="topbar-status">
            <span className="status-pill status-success">
              <span className="status-dot" />
              平台服务在线
            </span>
            <span className="badge" style={{ background: '#e0f2fe', color: '#0369a1', border: '1px solid #bae6fd' }}>
              环境: 本地 Docker
            </span>
          </div>
        </header>

        <main className="content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
