/**
 * 自动化任务：定时 / 日历链 / Webhook 三 Tab。
 * 样式对齐工作流管理页（wf-tabs + panel + table），选中态用 btn-sm / btn-ghost。
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { apiGet, getToken } from '../api/client'
import {
  Automation,
  AutomationRun,
  CalendarItem,
  createAutomation,
  deleteAutomation,
  listAutomations,
  listCalendarItems,
  listRuns,
  putCalendarItems,
  rotateSecrets,
  updateAutomation,
} from '../api/automation'
import { IconClock, IconPlus, IconRefresh, IconTrash } from '../components/Icons'
import { PageFeatureGuide } from '../components/PageFeatureGuide'
import { getUser, setSession } from '../stores/session'

type Tab = 'cron' | 'calendar' | 'webhook'

const TAB_LABEL: Record<Tab, string> = {
  cron: '定时任务',
  calendar: '日历任务',
  webhook: 'Webhook',
}

function daysInMonth(year: number, month: number) {
  return new Date(year, month + 1, 0).getDate()
}

function pad2(n: number) {
  return n < 10 ? `0${n}` : String(n)
}

function formatDate(y: number, m: number, d: number) {
  return `${y}-${pad2(m + 1)}-${pad2(d)}`
}

function rolesCanWrite(roles: string[] | undefined): boolean {
  const r = roles ?? []
  return r.includes('ADMIN') || r.includes('SUPER_ADMIN')
}

export function AutomationsPage() {
  const [tab, setTab] = useState<Tab>('cron')
  const [items, setItems] = useState<Automation[]>([])
  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [loading, setLoading] = useState(false)
  const [writable, setWritable] = useState(() => rolesCanWrite(getUser()?.roles))

  async function ensureMe() {
    try {
      const me = await apiGet<{ id: string; username: string; roles: string[] }>('/auth/me')
      const token = getToken()
      if (token) setSession(token, { id: me.id, username: me.username, roles: me.roles ?? [] })
      setWritable(rolesCanWrite(me.roles))
    } catch {
      setWritable(rolesCanWrite(getUser()?.roles))
    }
  }

  async function load(type?: Tab) {
    setLoading(true)
    setError('')
    try {
      const t = type ?? tab
      const [autoData, empData] = await Promise.all([
        listAutomations(t),
        apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
      ])
      setItems(autoData.items ?? [])
      setEmployees(empData.items ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void ensureMe()
  }, [])

  useEffect(() => {
    void load(tab)
  }, [tab])

  const empMap = useMemo(() => Object.fromEntries(employees.map((e) => [e.id, e.name])), [employees])

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1>
            <IconClock size={22} style={{ marginRight: 8, verticalAlign: -4 }} />
            自动化任务
          </h1>
          <p className="muted">周期调度、日历链式任务、入站 Webhook — 触发后创建固定员工 Job，结果回传飞书与审计。</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => void load()} disabled={loading}>
          <IconRefresh size={16} /> 刷新
        </button>
      </div>

      <PageFeatureGuide
        title="自动化任务"
        summary="周期调度、日历链式任务与入站 Webhook：触发后创建固定员工 + Prompt 的 Job，结果回传飞书与审计日志。"
        steps={[
          {
            step: '01',
            title: '定时任务',
            desc: '按日/周/月/年或自定义 cron 触发；同一 Workstation 排队，不同节点可并行。',
          },
          {
            step: '02',
            title: '日历任务',
            desc: '同一天可挂多条；上一条 Job 成功后才触发下一条，失败则中断当日链。',
          },
          {
            step: '03',
            title: 'Webhook',
            desc: 'HMAC + Bearer + IP 白名单 + 限流 + 幂等；密钥仅创建/轮换时展示一次。需管理员配置。',
          },
        ]}
      />

      {/* 与工作流管理页一致：选中 = 实心 btn-sm，未选中 = btn-ghost */}
      <div className="wf-tabs" role="tablist" aria-label="自动化任务类型">
        {(['cron', 'calendar', 'webhook'] as const).map((k) => (
          <button
            key={k}
            type="button"
            role="tab"
            aria-selected={tab === k}
            className={tab === k ? 'btn-sm' : 'btn-ghost btn-sm'}
            onClick={() => {
              setMsg('')
              setError('')
              setTab(k)
            }}
          >
            {TAB_LABEL[k]}
          </button>
        ))}
      </div>

      {error ? <div className="error">{error}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}
      {!writable ? (
        <div className="error" style={{ borderLeftColor: 'var(--border-subtle)', background: '#f8fafc', color: 'var(--text-secondary)' }}>
          当前账号仅可查看。新建与修改自动化需 ADMIN 权限。
        </div>
      ) : null}

      {tab === 'cron' && (
        <CronTab
          items={items}
          employees={employees}
          empMap={empMap}
          writable={writable}
          onReload={() => void load('cron')}
          onMsg={setMsg}
          onError={setError}
        />
      )}
      {tab === 'calendar' && (
        <CalendarTab
          items={items}
          employees={employees}
          empMap={empMap}
          writable={writable}
          onReload={() => void load('calendar')}
          onMsg={setMsg}
          onError={setError}
        />
      )}
      {tab === 'webhook' && (
        <WebhookTab
          items={items}
          employees={employees}
          empMap={empMap}
          writable={writable}
          onReload={() => void load('webhook')}
          onMsg={setMsg}
          onError={setError}
        />
      )}
    </div>
  )
}

function CronTab(props: {
  items: Automation[]
  employees: Array<{ id: string; name: string }>
  empMap: Record<string, string>
  writable: boolean
  onReload: () => void
  onMsg: (s: string) => void
  onError: (s: string) => void
}) {
  const [name, setName] = useState('')
  const [employeeId, setEmployeeId] = useState('')
  const [prompt, setPrompt] = useState('')
  const [preset, setPreset] = useState('daily')
  const [expr, setExpr] = useState('')
  const [chatId, setChatId] = useState('')
  const [runsOf, setRunsOf] = useState<string | null>(null)
  const [runs, setRuns] = useState<AutomationRun[]>([])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    props.onError('')
    try {
      const trigger_config =
        preset === 'custom' ? { expr, preset: 'custom' } : { preset, expr: '' }
      await createAutomation({
        name,
        trigger_type: 'cron',
        employee_id: employeeId,
        prompt,
        notify_chat_id: chatId,
        timezone: 'Asia/Shanghai',
        trigger_config,
      })
      props.onMsg('定时任务已创建')
      setName('')
      setPrompt('')
      props.onReload()
    } catch (err) {
      props.onError(err instanceof Error ? err.message : '创建失败')
    }
  }

  return (
    <div className="detail-grid">
      {props.writable && (
        <div className="panel">
          <div className="panel-header">
            <div>
              <h2>
                <IconPlus size={16} /> 新建定时任务
              </h2>
              <p>绑定员工与 Prompt，按周期自动建 Job</p>
            </div>
          </div>
          <form onSubmit={(e) => void onCreate(e)} className="stack-form">
            <label>
              名称
              <input value={name} onChange={(e) => setName(e.target.value)} required />
            </label>
            <label>
              数字员工
              <select value={employeeId} onChange={(e) => setEmployeeId(e.target.value)} required>
                <option value="">选择…</option>
                {props.employees.map((em) => (
                  <option key={em.id} value={em.id}>
                    {em.name} ({em.id})
                  </option>
                ))}
              </select>
            </label>
            <label>
              Prompt
              <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} required />
            </label>
            <label>
              周期预设
              <select value={preset} onChange={(e) => setPreset(e.target.value)}>
                <option value="daily">每日 09:00</option>
                <option value="weekly">每周一 09:00</option>
                <option value="monthly">每月 1 日 09:00</option>
                <option value="yearly">每年 1 月 1 日 09:00</option>
                <option value="custom">自定义 cron</option>
              </select>
            </label>
            {preset === 'custom' && (
              <label>
                Cron（分 时 日 月 周）
                <input value={expr} onChange={(e) => setExpr(e.target.value)} placeholder="0 9 * * *" required />
              </label>
            )}
            <label>
              飞书通知 Chat ID
              <input value={chatId} onChange={(e) => setChatId(e.target.value)} placeholder="oc_xxx" />
            </label>
            <button type="submit">创建</button>
          </form>
        </div>
      )}

      <div className="panel" style={props.writable ? undefined : { gridColumn: '1 / -1' }}>
        <div className="panel-header">
          <div>
            <h2>定时规则列表</h2>
            <p>共 {props.items.length} 条</p>
          </div>
        </div>
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>名称</th>
                <th>员工</th>
                <th>调度</th>
                <th>状态</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {props.items.map((a) => {
                const cfg = (a.trigger_config || {}) as Record<string, unknown>
                const sched = String(cfg.expr || cfg.preset || '-')
                return (
                  <tr key={a.id}>
                    <td>{a.name}</td>
                    <td>{props.empMap[a.employee_id] || a.employee_id}</td>
                    <td>
                      <span className="mono">{sched}</span>
                    </td>
                    <td>{a.enabled ? '启用' : '停用'}</td>
                    <td className="col-actions">
                      <div className="table-actions">
                        <button
                          type="button"
                          className="btn-ghost btn-sm"
                          onClick={() => {
                            void listRuns(a.id).then((r) => {
                              setRunsOf(a.id)
                              setRuns(r.items ?? [])
                            })
                          }}
                        >
                          记录
                        </button>
                        {props.writable && (
                          <>
                            <button
                              type="button"
                              className="btn-ghost btn-sm"
                              onClick={() => {
                                void updateAutomation(a.id, { enabled: !a.enabled })
                                  .then(() => props.onReload())
                                  .catch((e) => props.onError(e instanceof Error ? e.message : '更新失败'))
                              }}
                            >
                              {a.enabled ? '停用' : '启用'}
                            </button>
                            <button
                              type="button"
                              className="btn-danger btn-sm"
                              onClick={() => {
                                if (!confirm('确认删除？')) return
                                void deleteAutomation(a.id)
                                  .then(() => props.onReload())
                                  .catch((e) => props.onError(e instanceof Error ? e.message : '删除失败'))
                              }}
                            >
                              <IconTrash size={14} />
                            </button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                )
              })}
              {props.items.length === 0 && (
                <tr>
                  <td colSpan={5} className="empty-tip">
                    暂无定时任务
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {runsOf && (
          <div style={{ marginTop: '1rem' }}>
            <h3 style={{ margin: '0 0 0.5rem', fontSize: '0.95rem' }}>执行记录 · {runsOf}</h3>
            <ul className="mono" style={{ fontSize: 12, margin: 0, paddingLeft: '1.2rem' }}>
              {runs.map((r) => (
                <li key={r.id}>
                  {r.created_at} · {r.status} · job={r.job_id || '-'} · {r.trigger_source}
                </li>
              ))}
              {runs.length === 0 && <li className="muted">暂无记录</li>}
            </ul>
          </div>
        )}
      </div>
    </div>
  )
}

function CalendarTab(props: {
  items: Automation[]
  employees: Array<{ id: string; name: string }>
  empMap: Record<string, string>
  writable: boolean
  onReload: () => void
  onMsg: (s: string) => void
  onError: (s: string) => void
}) {
  const now = new Date()
  const [year, setYear] = useState(now.getFullYear())
  const [month, setMonth] = useState(now.getMonth())
  const [selected, setSelected] = useState(formatDate(now.getFullYear(), now.getMonth(), now.getDate()))
  const [autoId, setAutoId] = useState('')
  const [dayItems, setDayItems] = useState<CalendarItem[]>([])
  const [drafts, setDrafts] = useState<Array<{ employee_id: string; prompt: string }>>([
    { employee_id: '', prompt: '' },
  ])
  const [newName, setNewName] = useState('默认日历')
  const [chatId, setChatId] = useState('')

  const calendarAutos = props.items

  useEffect(() => {
    if (!autoId && calendarAutos.length > 0) {
      setAutoId(calendarAutos[0].id)
    }
  }, [calendarAutos, autoId])

  useEffect(() => {
    if (!autoId || !selected) return
    void listCalendarItems(autoId, selected)
      .then((r) => {
        const list = r.items ?? []
        setDayItems(list)
        setDrafts(
          list.length
            ? list.map((i) => ({ employee_id: i.employee_id, prompt: i.prompt }))
            : [{ employee_id: '', prompt: '' }],
        )
      })
      .catch((e) => props.onError(e instanceof Error ? e.message : '加载日历失败'))
  }, [autoId, selected])

  const dim = daysInMonth(year, month)
  const firstDow = new Date(year, month, 1).getDay()

  async function ensureCalendar() {
    if (autoId) return autoId
    const res = await createAutomation({
      name: newName || '默认日历',
      trigger_type: 'calendar',
      notify_chat_id: chatId,
      timezone: 'Asia/Shanghai',
      trigger_config: {},
    })
    props.onReload()
    setAutoId(res.item.id)
    return res.item.id
  }

  async function saveDay() {
    props.onError('')
    try {
      const id = await ensureCalendar()
      const clean = drafts.filter((d) => d.employee_id && d.prompt)
      await putCalendarItems(id, selected, clean)
      props.onMsg(`${selected} 已保存 ${clean.length} 条（成功后自动链式触发下一条）`)
      const r = await listCalendarItems(id, selected)
      setDayItems(r.items ?? [])
    } catch (e) {
      props.onError(e instanceof Error ? e.message : '保存失败')
    }
  }

  return (
    <div className="detail-grid">
      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>月视图</h2>
            <p>
              {year} 年 {month + 1} 月 · 选中 {selected}
            </p>
          </div>
          <div className="table-actions">
            <button
              type="button"
              className="btn-ghost btn-sm"
              onClick={() => {
                if (month === 0) {
                  setYear(year - 1)
                  setMonth(11)
                } else setMonth(month - 1)
              }}
            >
              上月
            </button>
            <button
              type="button"
              className="btn-ghost btn-sm"
              onClick={() => {
                if (month === 11) {
                  setYear(year + 1)
                  setMonth(0)
                } else setMonth(month + 1)
              }}
            >
              下月
            </button>
          </div>
        </div>

        <div className="stack-form" style={{ marginBottom: '0.75rem' }}>
          <label>
            日历规则
            <select value={autoId} onChange={(e) => setAutoId(e.target.value)}>
              <option value="">（新建）</option>
              {calendarAutos.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          </label>
          {!autoId && props.writable && (
            <>
              <label>
                新日历名称
                <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="默认日历" />
              </label>
              <label>
                飞书 Chat ID
                <input value={chatId} onChange={(e) => setChatId(e.target.value)} placeholder="oc_xxx" />
              </label>
            </>
          )}
        </div>

        <div className="auto-cal-grid">
          {['日', '一', '二', '三', '四', '五', '六'].map((d) => (
            <div key={d} className="auto-cal-dow">
              {d}
            </div>
          ))}
          {Array.from({ length: firstDow }).map((_, i) => (
            <div key={`e${i}`} />
          ))}
          {Array.from({ length: dim }).map((_, i) => {
            const day = i + 1
            const ds = formatDate(year, month, day)
            const active = ds === selected
            return (
              <button
                key={ds}
                type="button"
                aria-pressed={active}
                className={active ? 'btn-sm auto-cal-day' : 'btn-ghost btn-sm auto-cal-day'}
                onClick={() => setSelected(ds)}
              >
                {day}
              </button>
            )
          })}
        </div>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>{selected} 任务链</h2>
            <p>已存 {dayItems.length} 条 · 上一条 SUCCESS 后自动触发下一条</p>
          </div>
        </div>

        {drafts.map((d, idx) => (
          <div key={idx} className="stack-form auto-step-block">
            <div className="muted" style={{ fontWeight: 600 }}>
              第 {idx + 1} 步
            </div>
            <label>
              数字员工
              <select
                value={d.employee_id}
                onChange={(e) => {
                  const next = [...drafts]
                  next[idx] = { ...next[idx], employee_id: e.target.value }
                  setDrafts(next)
                }}
                disabled={!props.writable}
              >
                <option value="">选择员工…</option>
                {props.employees.map((em) => (
                  <option key={em.id} value={em.id}>
                    {em.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Prompt
              <textarea
                rows={2}
                value={d.prompt}
                disabled={!props.writable}
                onChange={(e) => {
                  const next = [...drafts]
                  next[idx] = { ...next[idx], prompt: e.target.value }
                  setDrafts(next)
                }}
                placeholder="Prompt"
              />
            </label>
            {props.writable && drafts.length > 1 && (
              <button type="button" className="btn-ghost btn-sm" onClick={() => setDrafts(drafts.filter((_, i) => i !== idx))}>
                移除此步
              </button>
            )}
          </div>
        ))}

        {props.writable && (
          <div className="table-actions" style={{ marginTop: '0.75rem', justifyContent: 'flex-start' }}>
            <button type="button" className="btn-ghost btn-sm" onClick={() => setDrafts([...drafts, { employee_id: '', prompt: '' }])}>
              <IconPlus size={14} /> 添加一步
            </button>
            <button type="button" className="btn-sm" onClick={() => void saveDay()}>
              保存当日链
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

function WebhookTab(props: {
  items: Automation[]
  employees: Array<{ id: string; name: string }>
  empMap: Record<string, string>
  writable: boolean
  onReload: () => void
  onMsg: (s: string) => void
  onError: (s: string) => void
}) {
  const [name, setName] = useState('')
  const [employeeId, setEmployeeId] = useState('')
  const [prompt, setPrompt] = useState('')
  const [chatId, setChatId] = useState('')
  const [ipList, setIpList] = useState('')
  const [rate, setRate] = useState(60)
  const [lastSecrets, setLastSecrets] = useState<Record<string, string> | null>(null)
  const [runsOf, setRunsOf] = useState<string | null>(null)
  const [runs, setRuns] = useState<AutomationRun[]>([])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    props.onError('')
    try {
      const res = await createAutomation({
        name,
        trigger_type: 'webhook',
        employee_id: employeeId,
        prompt,
        notify_chat_id: chatId,
        timezone: 'Asia/Shanghai',
        trigger_config: {
          auth_modes: ['hmac', 'bearer'],
          ip_allowlist: ipList
            .split(/[\s,]+/)
            .map((s) => s.trim())
            .filter(Boolean),
          rate_limit_per_min: rate,
        },
      })
      setLastSecrets(res.secrets ?? null)
      props.onMsg('Webhook 已创建；请立即保存下方密钥（仅显示一次）')
      setName('')
      setPrompt('')
      props.onReload()
    } catch (err) {
      props.onError(err instanceof Error ? err.message : '创建失败')
    }
  }

  return (
    <div className="detail-grid">
      {props.writable && (
        <div className="panel">
          <div className="panel-header">
            <div>
              <h2>
                <IconPlus size={16} /> 新建 Webhook
              </h2>
              <p>HMAC + Bearer 鉴权，密钥仅返回一次</p>
            </div>
          </div>
          <form onSubmit={(e) => void onCreate(e)} className="stack-form">
            <label>
              名称
              <input value={name} onChange={(e) => setName(e.target.value)} required />
            </label>
            <label>
              数字员工
              <select value={employeeId} onChange={(e) => setEmployeeId(e.target.value)} required>
                <option value="">选择…</option>
                {props.employees.map((em) => (
                  <option key={em.id} value={em.id}>
                    {em.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Prompt
              <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} required />
            </label>
            <label>
              飞书 Chat ID
              <input value={chatId} onChange={(e) => setChatId(e.target.value)} />
            </label>
            <label>
              IP 白名单（逗号分隔，可留空）
              <input value={ipList} onChange={(e) => setIpList(e.target.value)} placeholder="203.0.113.0/24" />
            </label>
            <label>
              每分钟限流
              <input type="number" value={rate} onChange={(e) => setRate(Number(e.target.value) || 60)} min={1} />
            </label>
            <button type="submit">创建（HMAC + Bearer）</button>
          </form>
          {lastSecrets && (
            <pre className="mono" style={{ marginTop: 12, fontSize: 12, whiteSpace: 'pre-wrap' }}>
              {JSON.stringify(lastSecrets, null, 2)}
            </pre>
          )}
        </div>
      )}

      <div className="panel" style={props.writable ? undefined : { gridColumn: '1 / -1' }}>
        <div className="panel-header">
          <div>
            <h2>Webhook 列表</h2>
            <p>共 {props.items.length} 条</p>
          </div>
        </div>
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>名称</th>
                <th>员工</th>
                <th>Endpoint</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {props.items.map((a) => {
                const cfg = (a.trigger_config || {}) as Record<string, unknown>
                const token = String(cfg.path_token || '')
                const url = `/api/integrations/automation/hooks/${token}`
                return (
                  <tr key={a.id}>
                    <td>{a.name}</td>
                    <td>{props.empMap[a.employee_id] || a.employee_id}</td>
                    <td>
                      <div className="table-actions" style={{ justifyContent: 'flex-start', marginBottom: 4 }}>
                        <button
                          type="button"
                          className="btn-ghost btn-sm"
                          onClick={() => {
                            void navigator.clipboard.writeText(window.location.origin + url)
                            props.onMsg('已复制 URL')
                          }}
                        >
                          复制 URL
                        </button>
                      </div>
                      <span className="mono" style={{ fontSize: 11 }}>
                        {url}
                      </span>
                    </td>
                    <td className="col-actions">
                      <div className="table-actions">
                        <button
                          type="button"
                          className="btn-ghost btn-sm"
                          onClick={() => {
                            void listRuns(a.id).then((r) => {
                              setRunsOf(a.id)
                              setRuns(r.items ?? [])
                            })
                          }}
                        >
                          记录
                        </button>
                        {props.writable && (
                          <>
                            <button
                              type="button"
                              className="btn-ghost btn-sm"
                              onClick={() => {
                                void rotateSecrets(a.id)
                                  .then((r) => {
                                    setLastSecrets(r.secrets)
                                    props.onMsg('密钥已轮换，请立即保存')
                                  })
                                  .catch((e) => props.onError(e instanceof Error ? e.message : '轮换失败'))
                              }}
                            >
                              轮换密钥
                            </button>
                            <button
                              type="button"
                              className="btn-danger btn-sm"
                              onClick={() => {
                                if (!confirm('确认删除？')) return
                                void deleteAutomation(a.id)
                                  .then(() => props.onReload())
                                  .catch((e) => props.onError(e instanceof Error ? e.message : '删除失败'))
                              }}
                            >
                              <IconTrash size={14} />
                            </button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                )
              })}
              {props.items.length === 0 && (
                <tr>
                  <td colSpan={4} className="empty-tip">
                    暂无 Webhook
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {runsOf && (
          <div style={{ marginTop: '1rem' }}>
            <h3 style={{ margin: '0 0 0.5rem', fontSize: '0.95rem' }}>投递记录 · {runsOf}</h3>
            <ul className="mono" style={{ fontSize: 12, margin: 0, paddingLeft: '1.2rem' }}>
              {runs.map((r) => (
                <li key={r.id}>
                  {r.created_at} · {r.status} · {r.error || 'ok'}
                </li>
              ))}
            </ul>
          </div>
        )}
        <p className="muted" style={{ marginTop: 12, fontSize: 12 }}>
          请求头：X-AIE-Timestamp（RFC3339）+ X-AIE-Signature（HMAC-SHA256 hex of timestamp.body），或 Authorization: Bearer
          token；可选 X-Idempotency-Key。
        </p>
      </div>
    </div>
  )
}
