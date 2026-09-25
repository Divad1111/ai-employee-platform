/**
 * 管理后台布局壳：深色侧栏 + 顶部面包屑状态栏 + 弹性自适应内容区。
 * 侧栏按权限过滤：无权限的子页签不显示；分组下无可见子项则隐藏整组。
 */
import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { roleDisplayName } from '../lib/rbacLabels'
import { formatTime, localTimeZone, timeZoneLabel } from '../lib/time'
import { clearSession, getUser } from '../stores/session'
import { usePerm } from '../stores/permissions'
import {
  IconDashboard,
  IconUsers,
  IconServer,
  IconJobs,
  IconTerminal,
  IconMessage,
  IconShield,
  IconCheckCircle,
  IconKey,
  IconPackage,
  IconFileText,
  IconSettings,
  IconLogOut,
  IconFolder,
  IconClock,
  IconPlug,
} from './Icons'

type NavItem = {
  to: string
  label: string
  icon: React.ComponentType<{ size?: number; className?: string; style?: React.CSSProperties }>
  end?: boolean
  /** 查看该页所需权限码；无则登录即可 */
  perm?: string
}

type NavSection = {
  title: string
  items: NavItem[]
}

/** 侧栏导航：perm 对齐后端 requirePerm */
const navSections: NavSection[] = [
  {
    title: '全景与监控',
    items: [
      { to: '/', label: '监控控制台', icon: IconDashboard, end: true, perm: 'employee.read' },
      { to: '/sessions', label: '运行会话', icon: IconTerminal, perm: 'session.read' },
    ],
  },
  {
    title: '员工与计算算力',
    items: [
      { to: '/employees', label: '数字员工', icon: IconUsers, perm: 'employee.read' },
      { to: '/workstations', label: '工作站节点', icon: IconServer, perm: 'workstation.read' },
      { to: '/workspaces', label: '项目工作区', icon: IconFolder, perm: 'workspace.read' },
    ],
  },
  {
    title: '任务调度流转',
    items: [
      { to: '/jobs', label: '任务流转中心', icon: IconJobs, perm: 'job.read' },
      { to: '/automations', label: '自动化任务', icon: IconClock, perm: 'automation.read' },
      { to: '/artifacts', label: '任务制品产物', icon: IconPackage, perm: 'job.read' },
      { to: '/approvals', label: '人工审批中心', icon: IconCheckCircle, perm: 'approval.read' },
    ],
  },
  {
    title: '能力与协同扩展',
    items: [
      { to: '/mcp-servers', label: 'MCP 服务管理', icon: IconPlug, perm: 'workflow.read' },
      // 飞书应用配置属系统集成，需 system.write
      { to: '/feishu', label: '飞书应用协同', icon: IconMessage, perm: 'system.write' },
    ],
  },
  {
    title: '用户与权限',
    items: [
      { to: '/users', label: '用户管理', icon: IconUsers, perm: 'user.read' },
      { to: '/roles', label: '角色与权限', icon: IconShield, perm: 'role.read' },
      { to: '/quotas', label: 'Token / 配额', icon: IconPackage, perm: 'quota.read' },
    ],
  },
  {
    title: '安全治理与系统',
    items: [
      // 策略引擎 / 系统配置属于运维写操作，需 system.write（VIEWER 仅有 system.read 不应进入）
      { to: '/permissions', label: '权限策略引擎', icon: IconShield, perm: 'system.write' },
      { to: '/secrets', label: '机密凭证保管箱', icon: IconKey, perm: 'secret.read' },
      { to: '/audit', label: '操作审计日志', icon: IconFileText, perm: 'audit.read' },
      { to: '/settings', label: '系统架构配置', icon: IconSettings, perm: 'system.write' },
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
  const { ready, can: canPerm } = usePerm()
  const [feishuStatus, setFeishuStatus] = useState<FeishuStatus | null>(null)
  /** 飞书配置页需 system.write；VIEWER 仅可看状态不可跳转 */
  const canFeishuConfig = ready && canPerm('system.write')

  const can = (perm?: string) => {
    if (!perm) return true
    if (!ready) return false
    return canPerm(perm)
  }

  const roleLabel = roleDisplayName(user?.roles?.[0] || '')

  // 过滤子项；分组下无可见子项则整组不显示
  const visibleSections = navSections
    .map((sec) => ({
      ...sec,
      items: sec.items.filter((it) => can(it.perm)),
    }))
    .filter((sec) => sec.items.length > 0)

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
          {visibleSections.map((sec) => (
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
              <div className="name">{user?.username || '未登录'}</div>
              <div className="role">{roleLabel || user?.roles?.[0] || '—'} · 系统在线</div>
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
            <TopbarClock />
            <span className="status-pill status-success" title="平台微服务与数据库运行中">
              <span className="status-dot" />
              平台服务在线
            </span>

            <FeishuStatusPill status={feishuStatus} canConfig={canFeishuConfig} />

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

/** 顶栏左侧时钟：本机时区，读不到时用上海。 */
function TopbarClock() {
  const tz = localTimeZone()
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), 1000)
    return () => window.clearInterval(id)
  }, [])
  return (
    <span className="topbar-clock" title={tz}>
      {timeZoneLabel(tz)} {formatTime(now)}
    </span>
  )
}

/** 顶栏飞书状态：有 system.write 才可跳转配置页，否则仅展示 */
function FeishuStatusPill({
  status,
  canConfig,
}: {
  status: FeishuStatus | null
  canConfig: boolean
}) {
  let className = 'status-pill status-muted'
  let label = '飞书检测中...'
  let title = '正在探测飞书开放平台网关...'

  if (status !== null) {
    if (!status.configured) {
      className = 'status-pill status-neutral'
      label = '飞书未配置'
      title = canConfig
        ? '未配置 App ID / Secret，点击前往配置'
        : '飞书未配置（需管理员权限才能进入配置页）'
    } else if (!status.enabled) {
      className = 'status-pill status-muted'
      label = '飞书未启用'
      title = canConfig
        ? '已配置凭据但未勾选启用协同，点击开启'
        : '飞书未启用（需管理员权限才能进入配置页）'
    } else if (status.connected) {
      className = 'status-pill status-success'
      label = `${status.ws_state === 'CONNECTED' ? '飞书长连接在线' : '飞书在线'}${
        status.bot_name ? ` (${status.bot_name})` : ''
      }`
      title = `飞书通信正常${status.ws_state === 'CONNECTED' ? ' (长连接网关已就绪，免公网IP)' : ''} · 机器人: ${status.bot_name || '已就绪'} · 响应时延 ${status.latency_ms ?? 0}ms`
    } else {
      className = 'status-pill status-danger'
      label = '飞书连接异常'
      title = canConfig
        ? `飞书连接失败: ${status.error || '通信异常'}，点击排查`
        : `飞书连接失败: ${status.error || '通信异常'}（需管理员权限才能进入配置页）`
    }
  }

  const pill = (
    <span className={className} title={title}>
      <span className="status-dot" />
      {label}
    </span>
  )

  if (!canConfig || status === null) {
    return pill
  }
  return (
    <Link to="/feishu" style={{ textDecoration: 'none' }} title={title}>
      {pill}
    </Link>
  )
}
