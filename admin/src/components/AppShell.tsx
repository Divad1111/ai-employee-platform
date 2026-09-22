/**
 * 管理后台布局壳：深色侧栏 + 顶部面包屑状态栏 + 弹性自适应内容区。
 */
import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { clearSession, getUser } from '../stores/session'
import {
  IconDashboard,
  IconUsers,
  IconServer,
  IconJobs,
  IconTerminal,
  IconMessage,
  IconZap,
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
      { to: '/workflows', label: '工作流管理', icon: IconZap },
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

type FeishuStatus = {
  configured: boolean
  enabled: boolean
  connected: boolean
  app_id?: string
  bot_name?: string
  bot_open_id?: string
  activate_status?: number
  latency_ms?: number
  error?: string
  ws_state?: string
  ws_error?: string
  last_checked_at?: string
}

export function AppShell() {
  const nav = useNavigate()
  const location = useLocation()
  const user = getUser()
  const [feishuStatus, setFeishuStatus] = useState<FeishuStatus | null>(null)

  useEffect(() => {
    let active = true
    const checkStatus = () => {
      apiGet<FeishuStatus>('/integrations/feishu/status')
        .then((res) => {
          if (active) setFeishuStatus(res)
        })
        .catch(() => {
          if (active) setFeishuStatus(null)
        })
    }
    checkStatus()
    const timer = setInterval(checkStatus, 20000)
    return () => {
      active = false
      clearInterval(timer)
    }
  }, [location.pathname])

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
            <span className="status-pill status-success" title="平台微服务与数据库运行中">
              <span className="status-dot" />
              平台服务在线
            </span>

            {/* 飞书应用协同状态 */}
            {feishuStatus === null ? (
              <span className="status-pill status-muted" title="正在探测飞书开放平台网关...">
                <span className="status-dot" />
                飞书检测中...
              </span>
            ) : !feishuStatus.configured ? (
              <Link to="/feishu" style={{ textDecoration: 'none' }} title="未配置 App ID / Secret，点击前往配置">
                <span className="status-pill status-neutral">
                  <span className="status-dot" />
                  飞书未配置
                </span>
              </Link>
            ) : !feishuStatus.enabled ? (
              <Link to="/feishu" style={{ textDecoration: 'none' }} title="已配置凭据但未勾选启用协同，点击开启">
                <span className="status-pill status-muted">
                  <span className="status-dot" />
                  飞书未启用
                </span>
              </Link>
            ) : feishuStatus.connected ? (
              <Link
                to="/feishu"
                style={{ textDecoration: 'none' }}
                title={`飞书通信正常${feishuStatus.ws_state === 'CONNECTED' ? ' (长连接网关已就绪，免公网IP)' : ''} · 机器人: ${feishuStatus.bot_name || '已就绪'} · 响应时延 ${feishuStatus.latency_ms ?? 0}ms · 群内 @机器人 实时响应`}
              >
                <span className="status-pill status-success">
                  <span className="status-dot" />
                  {feishuStatus.ws_state === 'CONNECTED' ? '飞书长连接在线' : '飞书在线'}
                  {feishuStatus.bot_name ? ` (${feishuStatus.bot_name})` : ''}
                </span>
              </Link>
            ) : (
              <Link
                to="/feishu"
                style={{ textDecoration: 'none' }}
                title={`飞书连接失败: ${feishuStatus.error || '通信异常'}，点击排查`}
              >
                <span className="status-pill status-danger">
                  <span className="status-dot" />
                  飞书连接异常
                </span>
              </Link>
            )}

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
