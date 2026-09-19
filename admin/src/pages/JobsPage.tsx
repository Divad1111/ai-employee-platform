/**
 * Jobs 任务列表与详情时间线 (Timeline)。
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { IconJobs, IconPlus, IconRefresh } from '../components/Icons'
import { EntityName } from '../components/EntityName'

type Job = {
  id: string
  employee_id: string
  session_id?: string
  status: string
  prompt: string
  result?: string
  idempotency_key: string
  timeout_sec: number
}

type JobEvent = {
  id: number
  event_type: string
  payload: Record<string, string>
  created_at: string
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
  const [searchParams, setSearchParams] = useSearchParams()
  const filter = searchParams.get('filter') || 'all'
  const [items, setItems] = useState<Job[]>([])
  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [workspaces, setWorkspaces] = useState<Array<{ id: string; path?: string }>>([])
  const [employeeId, setEmployeeId] = useState('')
  const [workspaceId, setWorkspaceId] = useState('')
  const [prompt, setPrompt] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const [jobsData, empData, wsData] = await Promise.all([
        apiGet<{ items: Job[] }>('/jobs'),
        apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
        apiGet<{ items: Array<{ id: string; path?: string }> }>('/workspaces').catch(() => ({ items: [] })),
      ])
      setItems(jobsData.items ?? [])
      setEmployees(empData.items ?? [])
      setWorkspaces(wsData.items ?? [])
    } finally {
      setLoading(false)
    }
  }

  const empMap = Object.fromEntries(employees.map((e) => [e.id, e.name]))

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
      })
      setPrompt('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建任务失败')
    }
  }

  async function onCancel(id: string) {
    if (!confirm(`确定要取消任务 #${id.slice(0, 8)} 吗？`)) return
    try {
      await apiPost(`/jobs/${id}/cancel`, {})
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '取消任务失败')
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

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>手动派发新任务</h2>
            <p>可直接下拉选择已有数字员工，或手动输入员工编号 ID 与 Prompt 指令</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onCreate}>
          <input
            list="jobs-emp-options"
            placeholder="选择已有员工或输入 ID"
            value={employeeId}
            onChange={(e) => setEmployeeId(e.target.value)}
            style={{ width: '250px' }}
            required
          />
          <datalist id="jobs-emp-options">
            {employees.map((e) => (
              <option key={e.id} value={e.id}>
                {e.name} ({e.id})
              </option>
            ))}
          </datalist>
          <input
            list="jobs-ws-options"
            placeholder="工作区 (可选，默认继承员工)"
            value={workspaceId}
            onChange={(e) => setWorkspaceId(e.target.value)}
            style={{ width: '220px' }}
          />
          <datalist id="jobs-ws-options">
            {workspaces.map((ws) => (
              <option key={ws.id} value={ws.id}>
                {ws.path ? `${ws.path} (${ws.id})` : ws.id}
              </option>
            ))}
          </datalist>
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
      </div>

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
                <th>当前流转状态</th>
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
                    <StatusBadge status={j.status} />
                  </td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {j.timeout_sec} 秒
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                      <Link to={`/jobs/${j.id}`} className="btn-ghost btn-sm" style={{ display: 'inline-flex' }}>
                        时间线详情 →
                      </Link>
                      {!isTerminal(j.status) && (
                        <button
                          type="button"
                          className="btn-danger btn-sm"
                          onClick={() => void onCancel(j.id)}
                          style={{ padding: '0.2rem 0.6rem', fontSize: '0.8rem' }}
                        >
                          取消
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
              {filteredItems.length === 0 ? (
                <tr>
                  <td colSpan={5} className="empty-tip">
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
    </section>
  )
}

export function JobDetailPage() {
  const { id } = useParams()
  const [job, setJob] = useState<Job | null>(null)
  const [empName, setEmpName] = useState('')
  const [events, setEvents] = useState<JobEvent[]>([])
  const [error, setError] = useState('')

  async function onCancel() {
    if (!id || !confirm(`确定要取消此任务吗？`)) return
    try {
      await apiPost(`/jobs/${id}/cancel`, {})
      const jb = await apiGet<Job>(`/jobs/${id}`)
      setJob(jb)
      const ev = await apiGet<{ items: JobEvent[] }>(`/jobs/${id}/events`)
      setEvents(ev.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : '取消任务失败')
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
                onClick={() => void onCancel()}
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

        {events && events.length > 0 ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', marginTop: '0.5rem' }}>
            {events.map((ev) => {
              const isReply = ev.event_type === 'AGENT_REPLY'
              return (
              <div
                key={ev.id}
                style={{
                  display: 'flex',
                  gap: '1rem',
                  padding: '0.85rem 1rem',
                  background: isReply ? '#f0fdf4' : '#f8fafc',
                  border: isReply ? '1px solid #bbf7d0' : '1px solid var(--border-subtle)',
                  borderRadius: '8px',
                  alignItems: 'flex-start',
                }}
              >
                <div style={{ fontSize: '0.82rem', color: 'var(--text-muted)', minWidth: '150px' }}>
                  {new Date(ev.created_at).toLocaleString()}
                </div>
                <div style={{ flex: 1 }}>
                  <div style={{ fontWeight: 600, fontSize: '0.9rem', color: 'var(--text-primary)' }}>
                    {ev.event_type}
                  </div>
                  {isReply && ev.payload?.reply ? (
                    <div style={{ marginTop: '0.5rem', whiteSpace: 'pre-wrap', lineHeight: 1.6 }}>{ev.payload.reply}</div>
                  ) : ev.payload && Object.keys(ev.payload).length > 0 ? (
                    <pre style={{ margin: '0.35rem 0 0', fontSize: '0.8rem', background: '#ffffff', padding: '0.5rem', borderRadius: '4px', border: '1px solid #e2e8f0', whiteSpace: 'pre-wrap' }}>
                      {JSON.stringify(ev.payload, null, 2)}
                    </pre>
                  ) : null}
                </div>
              </div>
              )
            })}
          </div>
        ) : (
          <div className="empty-tip">暂无事件轨迹记录</div>
        )}
      </div>
    </section>
  )
}
