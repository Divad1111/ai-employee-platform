/**
 * Jobs 任务列表与详情时间线 (Timeline)。
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { IconJobs, IconPlus, IconRefresh } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { PageFeatureGuide } from '../components/PageFeatureGuide'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { SearchableSelect } from '../components/SearchableSelect'
import { usePerm } from '../stores/permissions'
import { JobTimeline } from '../components/JobTimeline'

type Job = {
  id: string
  employee_id: string
  workspace_id?: string
  session_id?: string
  status: string
  prompt: string
  result?: string
  input_tokens?: number
  output_tokens?: number
  agent?: string
  token_source?: string
  source?: string
  idempotency_key: string
  timeout_sec: number
}

type JobEvent = {
  id: number
  event_type: string
  payload: Record<string, string>
  created_at: string
}

export function JobSourceBadge({ source }: { source?: string }) {
  const s = (source || 'web').toLowerCase()
  switch (s) {
    case 'feishu':
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            fontSize: '0.75rem',
            fontWeight: 600,
            padding: '2px 8px',
            borderRadius: '6px',
            background: '#e0f2fe',
            color: '#0369a1',
            border: '1px solid #bae6fd',
          }}
        >
          飞书
        </span>
      )
    case 'cron':
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            fontSize: '0.75rem',
            fontWeight: 600,
            padding: '2px 8px',
            borderRadius: '6px',
            background: '#f3e8ff',
            color: '#7e22ce',
            border: '1px solid #e9d5ff',
          }}
        >
          定时任务
        </span>
      )
    case 'calendar':
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            fontSize: '0.75rem',
            fontWeight: 600,
            padding: '2px 8px',
            borderRadius: '6px',
            background: '#fef3c7',
            color: '#b45309',
            border: '1px solid #fde68a',
          }}
        >
          日历任务
        </span>
      )
    case 'webhook':
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            fontSize: '0.75rem',
            fontWeight: 600,
            padding: '2px 8px',
            borderRadius: '6px',
            background: '#dcfce7',
            color: '#15803d',
            border: '1px solid #bbf7d0',
          }}
        >
          Webhook
        </span>
      )
    case 'api':
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            fontSize: '0.75rem',
            fontWeight: 600,
            padding: '2px 8px',
            borderRadius: '6px',
            background: '#f1f5f9',
            color: '#475569',
            border: '1px solid #e2e8f0',
          }}
        >
          API
        </span>
      )
    case 'system':
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            fontSize: '0.75rem',
            fontWeight: 600,
            padding: '2px 8px',
            borderRadius: '6px',
            background: '#f1f5f9',
            color: '#64748b',
            border: '1px solid #cbd5e1',
          }}
        >
          系统
        </span>
      )
    case 'web':
    default:
      return (
        <span
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            fontSize: '0.75rem',
            fontWeight: 600,
            padding: '2px 8px',
            borderRadius: '6px',
            background: '#f8fafc',
            color: '#64748b',
            border: '1px solid #e2e8f0',
          }}
        >
          {s === 'web' ? '控制台' : s}
        </span>
      )
  }
}

function formatJobTokens(job: Job) {
  const total = (job.input_tokens || 0) + (job.output_tokens || 0)
  if (!total && !job.token_source) return '—'
  const source = job.token_source === 'agent' ? 'Agent 上报' : job.token_source === 'estimate' ? '按文本估算' : ''
  const agent = job.agent ? ` · ${job.agent}` : ''
  return `输入 ${job.input_tokens || 0} / 输出 ${job.output_tokens || 0}${agent}${source ? `（${source}）` : ''}`
}

function isTerminal(status: string) {
  return ['SUCCESS', 'FAILED', 'CANCELLED', 'TIMEOUT'].includes(status)
}

function isActiveStatus(status: string) {
  return !isTerminal(status)
}

function isErrorStatus(status: string) {
  return ['FAILED', 'TIMEOUT', 'UNKNOWN'].includes(status)
}

export function JobsPage() {
  const { can } = usePerm()
  const canWrite = can('job.write')
  const canCancel = can('job.cancel')
  const [searchParams, setSearchParams] = useSearchParams()
  const filter = searchParams.get('filter') || 'all'
  const [items, setItems] = useState<Job[]>([])
  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [workspaces, setWorkspaces] = useState<Array<{ id: string; path?: string; repository?: string }>>([])
  const [employeeId, setEmployeeId] = useState('')
  const [workspaceId, setWorkspaceId] = useState('')
  const [prompt, setPrompt] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [cancelId, setCancelId] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const [rerunningId, setRerunningId] = useState('')
  const [notice, setNotice] = useState('')

  async function load() {
    setLoading(true)
    try {
      const [jobsData, empData, wsData] = await Promise.all([
        apiGet<{ items: Job[] }>('/jobs'),
        apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
        apiGet<{ items: Array<{ id: string; path?: string; repository?: string }> }>('/workspaces').catch(() => ({ items: [] })),
      ])
      setItems(jobsData.items ?? [])
      setEmployees(empData.items ?? [])
      setWorkspaces(wsData.items ?? [])
    } finally {
      setLoading(false)
    }
  }

  const empMap = Object.fromEntries(employees.map((e) => [e.id, e.name]))
  const empOptions = useMemo(
    () => employees.map((e) => ({ value: e.id, label: e.name, keywords: e.id })),
    [employees],
  )
  const wsOptions = useMemo(
    () =>
      workspaces.map((ws) => ({
        value: ws.id,
        label: ws.repository || ws.path || ws.id,
        keywords: `${ws.id} ${ws.path || ''} ${ws.repository || ''}`,
      })),
    [workspaces],
  )

  const filteredItems = useMemo(() => {
    if (filter === 'active') return items.filter((j) => isActiveStatus(j.status))
    if (filter === 'errors') return items.filter((j) => isErrorStatus(j.status))
    return items
  }, [items, filter])

  const filterLabel =
    filter === 'active' ? '当前活跃任务' : filter === 'errors' ? '异常与错误任务' : '全部任务'

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setError('')
    try {
      await apiPost('/jobs', {
        employee_id: employeeId,
        workspace_id: workspaceId || undefined,
        prompt,
        idempotency_key: `admin-${Date.now()}`,
        timeout_sec: 600,
        source: 'web',
      })
      setPrompt('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建任务失败')
    }
  }

  async function rerun(job: Job) {
    if (!job.employee_id || !job.prompt) {
      setError('原任务缺少员工或指令，无法重新执行')
      return
    }
    setError('')
    setNotice('')
    setRerunningId(job.id)
    try {
      const res = await apiPost<{ job?: { id: string } }>('/jobs', {
        employee_id: job.employee_id,
        workspace_id: job.workspace_id || undefined,
        prompt: job.prompt,
        timeout_sec: job.timeout_sec || 600,
        idempotency_key: `rerun-${job.id}-${Date.now()}`,
        source: job.source || 'web',
      })
      setNotice(res.job?.id ? `已按原参数新建任务 ${res.job.id}` : '已按原参数新建任务')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '重新执行失败')
    } finally {
      setRerunningId('')
    }
  }

  async function confirmCancel() {
    if (!cancelId) return
    setCancelling(true)
    try {
      await apiPost(`/jobs/${cancelId}/cancel`, {})
      setCancelId(null)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '取消任务失败')
    } finally {
      setCancelling(false)
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>任务流转中心 (Jobs)</h1>
          <p>任务由飞书消息或管理后台触发，经由调度器分发至对应的工作站与数字员工执行</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => void load()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新列表</span>
        </button>
      </header>

      <PageFeatureGuide
        title="任务流转中心全生命周期调度指引"
        summary="平台核心调度中心，记录从飞书群聊 @触发、创建分配、工作站接单、Agent 推理执行到产物交付的完整链路。"
        steps={[
          {
            step: '1',
            title: '触发接入 (Created)',
            desc: '支持通过飞书长连接 @机器人 派发任务，或在下方控制台手动输入 Prompt 指令一键下发。',
            tag: '任务入口',
          },
          {
            step: '2',
            title: '原子调度与锁竞争 (Assigned)',
            desc: '调度引擎根据员工绑定的工作站与工作区自动选路，完成节点分配与独占槽位锁定。',
            tag: '并发调度',
          },
          {
            step: '3',
            title: '执行监控与时间线 (Timeline)',
            desc: '点击任意任务进入详情，可查看包含每一轮状态迁移、ACP 会话事件与 Agent 真实输出的完整可溯时间线。',
            tag: '全轨复盘',
          },
        ]}
      />

      {canWrite ? (
      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>手动派发新任务</h2>
            <p>可搜索选择已有数字员工与工作区，也可直接输入 ID</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onCreate}>
          <SearchableSelect
            value={employeeId}
            onChange={setEmployeeId}
            options={empOptions}
            placeholder="选择或搜索员工…"
            allowCustom
            required
            style={{ minWidth: 220 }}
          />
          <SearchableSelect
            value={workspaceId}
            onChange={setWorkspaceId}
            options={wsOptions}
            placeholder="工作区（可选）"
            allowCustom
            style={{ minWidth: 200 }}
          />
          <input
            placeholder="任务指令内容 Prompt (例如: 检查工程并修复单元测试)"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            style={{ flex: 1, minWidth: '280px' }}
            required
          />
          <button type="submit">
            <IconPlus size={15} />
            <span>发布任务</span>
          </button>
        </form>
        {error ? <div className="error">{error}</div> : null}
      {notice ? <p className="muted">{notice}</p> : null}
      </div>
      ) : (
        error ? <div className="error">{error}</div> : null
      )}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>任务列表 · {filterLabel}</h2>
            <p>
              共 {filteredItems.length} 条
              {filter !== 'all' ? `（已从 ${items.length} 条中筛选）` : ''}
            </p>
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
            {(
              [
                ['all', '全部'],
                ['active', '活跃'],
                ['errors', '异常'],
              ] as const
            ).map(([key, label]) => (
              <button
                key={key}
                type="button"
                className={filter === key ? 'btn-sm' : 'btn-ghost btn-sm'}
                onClick={() => {
                  if (key === 'all') setSearchParams({})
                  else setSearchParams({ filter: key })
                }}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>任务需求描述 (Prompt)</th>
                <th>责任数字员工</th>
                <th>任务来源</th>
                <th>当前流转状态</th>
                <th>Token（输入 / 输出）</th>
                <th>超时设置</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {filteredItems.map((j) => (
                <tr key={j.id}>
                  <td style={{ maxWidth: '400px' }}>
                    <EntityName
                      name={j.prompt || `任务 #${j.id.slice(0, 8)}`}
                      id={j.id}
                      to={`/jobs/${j.id}`}
                      icon={<IconJobs size={15} style={{ color: 'var(--brand-600)' }} />}
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
                    <JobSourceBadge source={j.source} />
                  </td>
                  <td>
                    <StatusBadge status={j.status} />
                  </td>
                  <td style={{ fontSize: '0.82rem' }}>
                    {formatJobTokens(j)}
                  </td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {j.timeout_sec} 秒
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                      {canWrite ? (
                        <button
                          type="button"
                          className="btn-ghost btn-sm"
                          disabled={rerunningId === j.id}
                          onClick={() => void rerun(j)}
                        >
                          {rerunningId === j.id ? '创建中…' : '重新执行'}
                        </button>
                      ) : null}
                      <Link to={`/jobs/${j.id}`} className="btn-ghost btn-sm" style={{ display: 'inline-flex' }}>
                        时间线详情 →
                      </Link>
                      {!isTerminal(j.status) && canCancel ? (
                        <button
                          type="button"
                          className="btn-danger btn-sm"
                          onClick={() => setCancelId(j.id)}
                          style={{ padding: '0.2rem 0.6rem', fontSize: '0.8rem' }}
                        >
                          取消
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
              {filteredItems.length === 0 ? (
                <tr>
                  <td colSpan={7} className="empty-tip">
                    {filter === 'active'
                      ? '暂无活跃任务'
                      : filter === 'errors'
                        ? '暂无异常或失败任务'
                        : '暂无任务记录'}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      <ConfirmDialog
        open={!!cancelId}
        title="确认取消任务"
        description="取消后工作站将停止继续执行该任务（若已在执行中，以节点侧实际响应为准）。"
        targetLabel={cancelId ? `任务 #${cancelId.slice(0, 8)}` : undefined}
        targetMeta={cancelId || undefined}
        confirmText="确认取消"
        busy={cancelling}
        onCancel={() => !cancelling && setCancelId(null)}
        onConfirm={() => void confirmCancel()}
      />
    </section>
  )
}

export function JobDetailPage() {
  const { id } = useParams()
  const [job, setJob] = useState<Job | null>(null)
  const [empName, setEmpName] = useState('')
  const [events, setEvents] = useState<JobEvent[]>([])
  const [error, setError] = useState('')
  const [confirmCancel, setConfirmCancel] = useState(false)
  const [cancelling, setCancelling] = useState(false)

  async function doCancel() {
    if (!id) return
    setCancelling(true)
    try {
      await apiPost(`/jobs/${id}/cancel`, {})
      setConfirmCancel(false)
      const jb = await apiGet<Job>(`/jobs/${id}`)
      setJob(jb)
      const ev = await apiGet<{ items: JobEvent[] }>(`/jobs/${id}/events`)
      setEvents(ev.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : '取消任务失败')
    } finally {
      setCancelling(false)
    }
  }

  useEffect(() => {
    if (!id) return
    void apiGet<Job>(`/jobs/${id}`)
      .then((jb) => {
        setJob(jb)
        if (jb.employee_id) {
          void apiGet<{ name: string }>(`/employees/${jb.employee_id}`)
            .then((e) => setEmpName(e.name))
            .catch(() => undefined)
        }
      })
      .catch((e: Error) => setError(e.message))
    void apiGet<{ items: JobEvent[] }>(`/jobs/${id}/events`)
      .then((d) => setEvents(d.items ?? []))
      .catch(() => undefined)
  }, [id])

  if (!job) {
    return (
      <section>
        {error ? <div className="error">{error}</div> : <div className="empty-tip">正在查询任务状态详情...</div>}
      </section>
    )
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <Link to="/jobs" style={{ fontSize: '0.88rem' }}>← 返回任务列表</Link>
            <span style={{ color: 'var(--text-muted)' }}>/</span>
            <EntityName
              name={job.prompt ? (job.prompt.length > 25 ? job.prompt.slice(0, 25) + '...' : job.prompt) : `任务 #${job.id.slice(0, 8)}`}
              id={job.id}
            />
          </div>
          <h1 style={{ marginTop: '0.4rem', display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <span>任务详情</span>
            <StatusBadge status={job.status} />
            {!isTerminal(job.status) && (
              <button
                type="button"
                className="btn-danger btn-sm"
                onClick={() => setConfirmCancel(true)}
                style={{ padding: '0.25rem 0.75rem', fontSize: '0.85rem' }}
              >
                取消任务
              </button>
            )}
          </h1>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.35rem', fontSize: '0.88rem' }}>
            <span>指派员工:</span>
            <EntityName
              name={empName || job.employee_id}
              id={job.employee_id}
              to={`/employees/${job.employee_id}`}
            />
            <span style={{ color: 'var(--text-muted)', margin: '0 0.3rem' }}>·</span>
            <span style={{ color: 'var(--text-muted)' }}>超时限制: {job.timeout_sec} 秒</span>
            <span style={{ color: 'var(--text-muted)', margin: '0 0.3rem' }}>·</span>
            <span style={{ color: 'var(--text-muted)' }}>{formatJobTokens(job)}</span>
            <span style={{ color: 'var(--text-muted)', margin: '0 0.3rem' }}>·</span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem' }}>
              <span style={{ color: 'var(--text-muted)' }}>任务来源:</span>
              <JobSourceBadge source={job.source} />
            </span>
          </div>
        </div>
      </header>

      <div className="panel">
        <h2>任务指令 (Prompt)</h2>
        <div style={{ background: '#f8fafc', padding: '1rem', borderRadius: '8px', border: '1px solid var(--border-subtle)', marginTop: '0.5rem', whiteSpace: 'pre-wrap', lineHeight: 1.6 }}>
          {job.prompt}
        </div>
      </div>

      <div className="panel">
        <h2>Agent 回复 (Result)</h2>
        {job.result ? (
          <div style={{ background: '#f0fdf4', padding: '1rem', borderRadius: '8px', border: '1px solid #bbf7d0', marginTop: '0.5rem', whiteSpace: 'pre-wrap', lineHeight: 1.6 }}>
            {job.result}
          </div>
        ) : (
          <div className="empty-tip" style={{ marginTop: '0.5rem' }}>
            {isTerminal(job.status) ? '任务已结束，但未收到 Agent 文本回复' : '等待 Agent 执行并返回结果…'}
          </div>
        )}
        {job.session_id ? (
          <p style={{ marginTop: '0.75rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
            关联会话: <Link to="/sessions">{job.session_id}</Link>
          </p>
        ) : null}
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>执行轨迹与生命周期时间线 (Timeline)</h2>
            <p>记录状态迁移、Session 生命周期、Agent 回复与执行输出</p>
          </div>
        </div>

        <JobTimeline events={events ?? []} />
      </div>

      <ConfirmDialog
        open={confirmCancel}
        title="确认取消任务"
        description="取消后工作站将停止继续执行该任务。"
        targetLabel={job.prompt ? (job.prompt.length > 40 ? `${job.prompt.slice(0, 40)}…` : job.prompt) : `任务 #${job.id.slice(0, 8)}`}
        targetMeta={job.id}
        confirmText="确认取消"
        busy={cancelling}
        onCancel={() => !cancelling && setConfirmCancel(false)}
        onConfirm={() => void doCancel()}
      />
    </section>
  )
}
