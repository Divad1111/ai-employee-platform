/**
 * Dashboard：对齐设计文档 §9 — 统计卡 + Active Jobs + Workstations 资源监控 + 当前用户 Token 用量。
 */
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { IconRefresh } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { PageFeatureGuide } from '../components/PageFeatureGuide'
import { roleDisplayName } from '../lib/rbacLabels'
import { formatTime } from '../lib/time'
import { usePerm } from '../stores/permissions'

type ActiveJob = {
  id: string
  employee_id: string
  status: string
  prompt: string
}

type WsRow = {
  id: string
  name: string
  status: string
  cpu_percent?: number
  memory_percent?: number
  disk_percent?: number
}

type MyQuota = {
  user_id: string
  period_type: string
  period_key?: string
  tokens_used: number
  requests_used: number
  token_limit: number
  request_limit: number
  input_tokens?: number
  output_tokens?: number
  role_token_limit?: number
  exception_token_limit?: number
  extra_token_limit?: number
  has_exception?: boolean
  has_extra?: boolean
  unlimited: boolean
  source: string
  source_role?: string
  usage_percent: number
  remaining: number
  by_workstation?: UsageBucket[]
  by_employee?: UsageBucket[]
  by_agent?: UsageBucket[]
}

type UsageBucket = {
  id?: string
  name: string
  input_tokens: number
  output_tokens: number
  total_tokens: number
  jobs: number
}

type Dash = {
  employees: number
  workstations: number
  workstations_online: number
  jobs: number
  active_jobs: number
  busy: number
  errors: number
  recent_active_jobs: ActiveJob[]
  workstations_detail: WsRow[]
  my_quota?: MyQuota
}

function formatTokens(n: number) {
  if (!Number.isFinite(n)) return '—'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1)}K`
  return String(Math.round(n))
}

export function DashboardPage() {
  const { can } = usePerm()
  const canQuotaAdmin = can('quota.read')
  const [data, setData] = useState<Dash | null>(null)
  const [empMap, setEmpMap] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [updatedAt, setUpdatedAt] = useState('')

  async function refresh() {
    try {
      const [d, empData] = await Promise.all([
        apiGet<Dash>('/dashboard'),
        apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
      ])
      setData(d)
      setEmpMap(Object.fromEntries((empData.items ?? []).map((e) => [e.id, e.name])))
      setUpdatedAt(formatTime())
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
    }
  }

  useEffect(() => {
    void refresh()
    const poll = setInterval(() => void refresh(), 5000)
    return () => clearInterval(poll)
  }, [])

  const q = data?.my_quota
  const quotaPct = q ? Math.min(Math.max(q.usage_percent || 0, 0), 100) : 0
  const quotaBarColor =
    quotaPct >= 90 ? 'var(--danger)' : quotaPct >= 70 ? '#d97706' : 'var(--brand-500)'
  const quotaSourceLabel = q
    ? q.unlimited || q.token_limit <= 0
      ? q.source === 'none'
        ? '未配置策略（不限）'
        : '不限'
      : [
          q.has_exception
            ? '用户例外（已替换角色基数）'
            : q.source === 'ROLE'
              ? `角色预设 · ${roleDisplayName(q.source_role || '')}`
              : '未配置角色基数',
          q.has_extra ? '含用户额外' : '',
        ]
          .filter(Boolean)
          .join(' + ')
    : '—'

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>数字员工协同控制台</h1>
          <p>全域系统运行指标监控 · 自动同步心跳{updatedAt ? ` · 上次更新: ${updatedAt}` : ''}</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => void refresh()}>
          <IconRefresh size={15} />
          <span>立即刷新</span>
        </button>
      </header>

      <PageFeatureGuide
        title="监控控制台运行指标与链路架构指引"
        summary="汇聚全域计算算力、网络心跳、数字员工注册状态及实时任务流转的关键大盘，支持秒级自动轮询探活。"
        steps={[
          {
            step: '1',
            title: '全量资产与健康指标大盘',
            desc: '实时聚合数字员工数、工作站节点数、在线运行节点及历史任务总量，快速洞察集群全局承载水位。',
            tag: '指标聚合',
          },
          {
            step: '2',
            title: '本人 Token 配额水位',
            desc: '展示当前账号本月 Token 已用量。有效限额 =（用户例外或角色预设）+ 用户额外，并分项列出便于核对。',
            tag: '配额监控',
          },
          {
            step: '3',
            title: '正在执行的任务 (Active Jobs)',
            desc: '实时呈现处于 CREATED / QUEUED / ASSIGNED / STARTING / RUNNING 的活跃任务流转状态，支持一键穿梭至任务详情追踪执行 Timeline。',
            tag: '流转追踪',
          },
        ]}
      />

      {error ? <div className="error">{error}</div> : null}

      {data ? (
        <>
          <div className="stat-grid">
            <Link to="/employees" className="stat-card" title="查看数字员工">
              <div className="stat-label">数字员工总数</div>
              <div className="stat-value">{data.employees}</div>
              <div className="stat-sub">注册生效的 AI 员工 →</div>
            </Link>
            <Link to="/workstations" className="stat-card" title="查看工作站节点">
              <div className="stat-label">工作站节点</div>
              <div className="stat-value">{data.workstations}</div>
              <div className="stat-sub">在线: {data.workstations_online} 台 →</div>
            </Link>
            <Link to="/workstations" className="stat-card" title="查看在线运行节点">
              <div className="stat-label">在线运行节点</div>
              <div className="stat-value" style={{ color: 'var(--brand-600)' }}>
                {data.workstations_online}
              </div>
              <div className="stat-sub">mTLS 长连握手正常 →</div>
            </Link>
            <Link to="/jobs" className="stat-card" title="查看全部任务">
              <div className="stat-label">历史任务总数</div>
              <div className="stat-value">{data.jobs}</div>
              <div className="stat-sub">全生命周期调度 →</div>
            </Link>
            <Link to="/jobs?filter=active" className="stat-card" title="查看当前活跃任务">
              <div className="stat-label">当前活跃任务</div>
              <div className="stat-value" style={{ color: 'var(--info)' }}>
                {data.active_jobs}
              </div>
              <div className="stat-sub">执行或启动排队中 →</div>
            </Link>
            <Link to="/jobs?filter=errors" className="stat-card" title="查看异常与错误任务">
              <div className="stat-label">异常与错误</div>
              <div className="stat-value" style={{ color: data.errors > 0 ? 'var(--danger)' : 'var(--text-muted)' }}>
                {data.errors}
              </div>
              <div className="stat-sub">失败或超时的任务 →</div>
            </Link>
          </div>

          <div className="panel">
            <div className="panel-header" style={{ alignItems: 'flex-start' }}>
              <div>
                <h2>我的 Token 用量（本月）</h2>
                <p>
                  限额来源：{quotaSourceLabel}
                  {q?.period_key ? ` · 周期 ${q.period_key}` : ''}
                </p>
              </div>
              {canQuotaAdmin ? <Link to="/quotas">配额策略 →</Link> : null}
            </div>
            <div style={{ padding: '0 1.1rem 1.25rem' }}>
              {q ? (
                <>
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))',
                      gap: '1rem',
                      marginBottom: '0.85rem',
                    }}
                  >
                    <div>
                      <div className="stat-label">已使用</div>
                      <div className="stat-value" style={{ fontSize: '1.55rem' }}>
                        {formatTokens(q.tokens_used)}
                      </div>
                    </div>
                    <div>
                      <div className="stat-label">有效限额</div>
                      <div className="stat-value" style={{ fontSize: '1.55rem' }}>
                        {q.unlimited || q.token_limit <= 0 ? '不限' : formatTokens(q.token_limit)}
                      </div>
                    </div>
                    <div>
                      <div className="stat-label">剩余</div>
                      <div
                        className="stat-value"
                        style={{ fontSize: '1.55rem', color: quotaPct >= 90 ? 'var(--danger)' : undefined }}
                      >
                        {q.unlimited || q.token_limit <= 0 ? '—' : formatTokens(q.remaining)}
                      </div>
                    </div>
                    <div>
                      <div className="stat-label">使用率</div>
                      <div className="stat-value" style={{ fontSize: '1.55rem', color: quotaBarColor }}>
                        {q.unlimited || q.token_limit <= 0 ? '—' : `${quotaPct.toFixed(1)}%`}
                      </div>
                    </div>
                  </div>
                  <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
                    输入 {formatTokens(q.input_tokens || 0)} · 输出 {formatTokens(q.output_tokens || 0)} · 合计 {formatTokens(q.tokens_used)}
                  </p>
                  <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: '0.85rem' }}>
                    角色配额 {q.role_token_limit && q.role_token_limit > 0 ? formatTokens(q.role_token_limit) : '不限或不适用'}
                    {' · '}
                    用户例外 {q.has_exception ? formatTokens(q.exception_token_limit || 0) : '未设置（沿用角色）'}
                    {' · '}
                    用户额外 {q.has_extra ? formatTokens(q.extra_token_limit || 0) : '未设置'}
                  </p>
                  {q.unlimited || q.token_limit <= 0 ? (
                    <p className="muted" style={{ margin: 0, fontSize: '0.85rem' }}>
                      当前账号未设置 Token 上限（或限额为 0 表示不限制）。
                    </p>
                  ) : (
                    <>
                      <div
                        style={{
                          display: 'flex',
                          justifyContent: 'space-between',
                          fontSize: '0.78rem',
                          marginBottom: '0.35rem',
                        }}
                      >
                        <span>
                          {formatTokens(q.tokens_used)} / {formatTokens(q.token_limit)} Token
                        </span>
                        <span style={{ color: quotaBarColor }}>{quotaPct.toFixed(1)}%</span>
                      </div>
                      <div className="progress-bar-wrap">
                        <div
                          className="progress-bar-fill"
                          style={{ width: `${quotaPct}%`, background: quotaBarColor }}
                        />
                      </div>
                    </>
                  )}
                  <UsageBreakdown
                    byWorkstation={q.by_workstation}
                    byEmployee={q.by_employee}
                    byAgent={q.by_agent}
                  />
                </>
              ) : (
                <p className="muted" style={{ margin: 0 }}>
                  暂无配额数据
                </p>
              )}
            </div>
          </div>

          <div className="panel">
            <div className="panel-header">
              <div>
                <h2>当前正在执行的任务 (Active Jobs)</h2>
                <p>实时下发与状态流转</p>
              </div>
              <Link to="/jobs">查看全部任务 →</Link>
            </div>

            {data.recent_active_jobs && data.recent_active_jobs.length > 0 ? (
              <div className="table-wrapper">
                <table className="table">
                  <thead>
                    <tr>
                      <th>任务需求描述 (Prompt)</th>
                      <th>所属数字员工</th>
                      <th>当前状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.recent_active_jobs.map((j) => (
                      <tr key={j.id}>
                        <td style={{ maxWidth: '400px' }}>
                          <EntityName
                            name={j.prompt || `任务 #${j.id.slice(0, 8)}`}
                            id={j.id}
                            to={`/jobs/${j.id}`}
                          />
                        </td>
                        <td>
                          <EntityName
                            name={empMap[j.employee_id] || j.employee_id}
                            id={j.employee_id}
                            to={`/employees/${j.employee_id}`}
                          />
                        </td>
                        <td>
                          <StatusBadge status={j.status} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="empty-tip">暂无正在执行的活跃任务，系统负载平稳</div>
            )}
          </div>

          <div className="panel">
            <div className="panel-header">
              <div>
                <h2>工作站节点与实时硬件负载</h2>
                <p>来自工作站心跳 · CPU、内存与磁盘均为本机占用</p>
              </div>
              <Link to="/workstations">节点列表 →</Link>
            </div>

            {data.workstations_detail && data.workstations_detail.length > 0 ? (
              <div className="table-wrapper">
                <table className="table">
                  <thead>
                    <tr>
                      <th>计算节点名称</th>
                      <th>节点状态</th>
                      <th style={{ width: '180px' }}>CPU</th>
                      <th style={{ width: '180px' }}>内存</th>
                      <th style={{ width: '180px' }}>磁盘</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.workstations_detail.map((w) => (
                      <tr key={w.id}>
                        <td>
                          <EntityName
                            name={w.name && w.name !== w.id ? w.name : `工作站-${w.id.slice(-6)}`}
                            id={w.id}
                            to="/workstations"
                          />
                        </td>
                        <td>
                          <StatusBadge status={w.status} />
                        </td>
                        <td>{loadBar(w.cpu_percent)}</td>
                        <td>{loadBar(w.memory_percent)}</td>
                        <td>{loadBar(w.disk_percent)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="empty-tip">暂无接入的工作站节点</div>
            )}
          </div>
        </>
      ) : (
        <div className="empty-tip">正在获取系统监控数据...</div>
      )}
    </section>
  )
}

function loadBar(value?: number) {
  const n = Number.isFinite(value) ? Math.min(Math.max(value || 0, 0), 100) : 0
  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.78rem' }}>
        <span>{n.toFixed(1)}%</span>
      </div>
      <div className="progress-bar-wrap">
        <div
          className="progress-bar-fill"
          style={{
            width: `${n}%`,
            background: n > 85 ? 'var(--danger)' : 'var(--brand-500)',
          }}
        />
      </div>
    </>
  )
}

function UsageBreakdown({
  byWorkstation,
  byEmployee,
  byAgent,
}: {
  byWorkstation?: UsageBucket[]
  byEmployee?: UsageBucket[]
  byAgent?: UsageBucket[]
}) {
  const [open, setOpen] = useState(false)
  return (
    <div
      style={{
        marginTop: '1rem',
        borderTop: '1px solid var(--border-subtle)',
        paddingTop: '0.75rem',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.75rem' }}>
        <div>
          <div className="stat-label" style={{ margin: 0 }}>用量分布</div>
          <p className="muted" style={{ margin: '0.2rem 0 0', fontSize: '0.82rem' }}>
            按工作站、数字员工与 Agent 汇总本月消耗
          </p>
        </div>
        <button type="button" className="btn-ghost btn-sm" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
          {open ? '收起' : '展开'}
        </button>
      </div>
      {open ? (
        <div>
          <UsageSplit title="按工作站" rows={byWorkstation} />
          <UsageSplit title="按数字员工" rows={byEmployee} />
          <UsageSplit title="按 Agent" rows={byAgent} />
        </div>
      ) : null}
    </div>
  )
}

function UsageSplit({ title, rows }: { title: string; rows?: UsageBucket[] }) {
  const items = rows ?? []
  return (
    <div style={{ marginTop: '1rem' }}>
      <div className="stat-label" style={{ marginBottom: '0.4rem' }}>{title}</div>
      {items.length === 0 ? (
        <p className="muted" style={{ margin: 0, fontSize: '0.84rem' }}>本月还没有计入用量的任务</p>
      ) : (
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>名称</th>
                <th>任务数</th>
                <th>输入</th>
                <th>输出</th>
                <th>合计</th>
              </tr>
            </thead>
            <tbody>
              {items.map((row) => (
                <tr key={row.id || row.name}>
                  <td>{row.name}</td>
                  <td>{row.jobs}</td>
                  <td>{formatTokens(row.input_tokens)}</td>
                  <td>{formatTokens(row.output_tokens)}</td>
                  <td>{formatTokens(row.total_tokens)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
