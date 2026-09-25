/**
 * 自动化任务：定时 / 日历链 / Webhook。
 * - 定时：周期类型 + 自选时刻（非写死 09:00）
 * - 日历：月视图带任务数圆点，年月切换
 * - Webhook：创建后密钥卡片；列表展示鉴权/限流等
 */
import { FormEvent, Fragment, type ReactNode, useEffect, useMemo, useState } from 'react'
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
import { IconAlertTriangle, IconClock, IconMaximize, IconMinimize, IconPlus, IconRefresh, IconTrash } from '../components/Icons'
import { PageFeatureGuide } from '../components/PageFeatureGuide'
import { SearchableSelect } from '../components/SearchableSelect'
import { getUser, setSession } from '../stores/session'
import { formatDateTime, localTimeZone, timeZoneLabel, zonedYMD } from '../lib/time'

type Tab = 'cron' | 'calendar' | 'webhook'

/** 必填标签：文字与红星同一行 */
function FieldLabel(props: { children: ReactNode; required?: boolean }) {
  return (
    <span className="field-caption">
      {props.children}
      {props.required ? (
        <span className="req-star" title="必填" aria-label="必填">
          *
        </span>
      ) : null}
    </span>
  )
}

type DeleteTarget = { id: string; name: string; kind: string }

/** 删除确认弹窗（替代浏览器 confirm） */
function DeleteConfirmModal(props: {
  target: DeleteTarget
  busy?: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  const { target, busy, onCancel, onConfirm } = props
  return (
    <div className="auto-modal-backdrop" role="presentation" onClick={onCancel}>
      <div
        className="auto-modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="auto-del-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="auto-modal-head">
          <IconAlertTriangle size={22} style={{ color: '#ef4444', flexShrink: 0 }} />
          <h3 id="auto-del-title">确认删除{target.kind}</h3>
        </div>
        <p className="auto-modal-desc">
          删除后不可恢复，关联的投递记录将一并失去引用。请确认要删除：
        </p>
        <div className="auto-modal-target">
          <strong>{target.name || '（未命名）'}</strong>
          <code className="mono">{target.id}</code>
        </div>
        <div className="auto-modal-actions">
          <button type="button" className="btn-ghost" onClick={onCancel} disabled={busy}>
            取消
          </button>
          <button type="button" className="btn-danger" onClick={onConfirm} disabled={busy}>
            {busy ? '删除中…' : '确认删除'}
          </button>
        </div>
      </div>
    </div>
  )
}

const TAB_LABEL: Record<Tab, string> = {
  cron: '定时任务',
  calendar: '日历任务',
  webhook: 'Webhook',
}

const WEEKDAYS = [
  { v: 1, label: '周一' },
  { v: 2, label: '周二' },
  { v: 3, label: '周三' },
  { v: 4, label: '周四' },
  { v: 5, label: '周五' },
  { v: 6, label: '周六' },
  { v: 0, label: '周日' },
]

const MONTHS = Array.from({ length: 12 }, (_, i) => ({ v: i + 1, label: `${i + 1} 月` }))

/** 常用时刻快捷选项（仅填入时/分，不强制 09:00） */
const TIME_QUICK = [
  { h: 0, m: 0, label: '00:00' },
  { h: 8, m: 0, label: '08:00' },
  { h: 9, m: 0, label: '09:00' },
  { h: 12, m: 0, label: '12:00' },
  { h: 14, m: 30, label: '14:30' },
  { h: 18, m: 0, label: '18:00' },
  { h: 22, m: 0, label: '22:00' },
]

function daysInMonth(year: number, month: number) {
  return new Date(year, month + 1, 0).getDate()
}

function pad2(n: number) {
  return n < 10 ? `0${n}` : String(n)
}

function formatDate(y: number, m: number, d: number) {
  return `${y}-${pad2(m + 1)}-${pad2(d)}`
}

/** 统一为 YYYY-MM-DD，兼容后端偶发带时间的返回 */
function normalizeRunDate(raw: string | undefined | null): string {
  if (!raw) return ''
  const s = String(raw).trim()
  if (/^\d{4}-\d{2}-\d{2}/.test(s)) return s.slice(0, 10)
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s.slice(0, 10)
  return formatDate(d.getFullYear(), d.getMonth(), d.getDate())
}

function rolesCanWrite(roles: string[] | undefined): boolean {
  const r = roles ?? []
  return r.includes('ADMIN') || r.includes('SUPER_ADMIN')
}

function buildCronExpr(opts: {
  preset: string
  hour: number
  minute: number
  weekday: number
  day: number
  month: number
  customExpr: string
}): string {
  if (opts.preset === 'custom') return opts.customExpr.trim()
  const h = Math.min(23, Math.max(0, opts.hour))
  const m = Math.min(59, Math.max(0, opts.minute))
  switch (opts.preset) {
    case 'daily':
      return `${m} ${h} * * *`
    case 'weekly':
      return `${m} ${h} * * ${opts.weekday}`
    case 'monthly':
      return `${m} ${h} ${opts.day} * *`
    case 'yearly':
      return `${m} ${h} ${opts.day} ${opts.month} *`
    default:
      return `${m} ${h} * * *`
  }
}

function describeCron(cfg: Record<string, unknown>): string {
  const expr = String(cfg.expr || '')
  const preset = String(cfg.preset || '')
  const hour = cfg.hour != null ? Number(cfg.hour) : null
  const minute = cfg.minute != null ? Number(cfg.minute) : null
  const timeStr =
    hour != null && minute != null
      ? `${pad2(hour)}:${pad2(minute)}`
      : expr
        ? (() => {
            const p = expr.split(/\s+/)
            return p.length >= 2 ? `${pad2(Number(p[1]) || 0)}:${pad2(Number(p[0]) || 0)}` : ''
          })()
        : ''

  const wd = WEEKDAYS.find((w) => w.v === Number(cfg.weekday))?.label
  switch (preset) {
    case 'daily':
      return `每日 ${timeStr || '—'}`.trim()
    case 'weekly':
      return `每${wd || '周'} ${timeStr || '—'}`.trim()
    case 'monthly':
      return `每月 ${cfg.day || 1} 日 ${timeStr || '—'}`.trim()
    case 'yearly':
      return `每年 ${cfg.month || 1} 月 ${cfg.day || 1} 日 ${timeStr || '—'}`.trim()
    case 'custom':
      return expr || '自定义'
    default:
      return expr || preset || '—'
  }
}

function copyText(text: string) {
  return navigator.clipboard.writeText(text)
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
    // 切 Tab 先清空，避免日历 Tab 短暂复用定时/Webhook 列表 ID
    setItems([])
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
        summary="周期可自选时刻；日历按日挂链；Webhook 创建后请立即保存密钥。"
        steps={[
          { step: '01', title: '定时任务', desc: '选择日/周/月/年，再自选执行时刻；也可写自定义 cron。' },
          { step: '02', title: '日历任务', desc: '月视图圆点表示有任务；点日期编辑当日链，成功后自动下一条。' },
          { step: '03', title: 'Webhook', desc: '创建后展示 HMAC/Bearer（仅一次）；列表可见鉴权方式、限流与 Endpoint。' },
        ]}
      />

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
        <CronTab items={items} employees={employees} empMap={empMap} writable={writable} onReload={() => void load('cron')} onMsg={setMsg} onError={setError} />
      )}
      {tab === 'calendar' && (
        <CalendarTab items={items} employees={employees} empMap={empMap} writable={writable} onReload={() => void load('calendar')} onMsg={setMsg} onError={setError} />
      )}
      {tab === 'webhook' && (
        <WebhookTab items={items} employees={employees} empMap={empMap} writable={writable} onReload={() => void load('webhook')} onMsg={setMsg} onError={setError} />
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
  const [hour, setHour] = useState(9)
  const [minute, setMinute] = useState(0)
  const [weekday, setWeekday] = useState(1)
  const [day, setDay] = useState(1)
  const [month, setMonth] = useState(1)
  const [expr, setExpr] = useState('0 9 * * *')
  const [chatId, setChatId] = useState('')
  const [runsOf, setRunsOf] = useState<string | null>(null)
  const [runs, setRuns] = useState<AutomationRun[]>([])
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)
  const [deleting, setDeleting] = useState(false)

  const previewExpr = buildCronExpr({ preset, hour, minute, weekday, day, month, customExpr: expr })

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    props.onError('')
    const built = buildCronExpr({ preset, hour, minute, weekday, day, month, customExpr: expr })
    if (!built) {
      props.onError('请填写有效的调度表达式')
      return
    }
    try {
      await createAutomation({
        name,
        trigger_type: 'cron',
        employee_id: employeeId,
        prompt,
        notify_chat_id: chatId,
        timezone: localTimeZone(),
        trigger_config: {
          preset,
          expr: built,
          hour,
          minute,
          weekday: preset === 'weekly' ? weekday : undefined,
          day: preset === 'monthly' || preset === 'yearly' ? day : undefined,
          month: preset === 'yearly' ? month : undefined,
        },
      })
      props.onMsg(`定时任务已创建（${describeCron({ preset, hour, minute, weekday, day, month, expr: built })}）`)
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
              <p>周期与时刻分开选：周期只管「哪天」，时刻任意 00:00–23:59</p>
            </div>
          </div>
          <form onSubmit={(e) => void onCreate(e)} className="stack-form">
            <label>
              <FieldLabel required>名称</FieldLabel>
              <input value={name} onChange={(e) => setName(e.target.value)} required />
            </label>
            <label>
              <FieldLabel required>数字员工</FieldLabel>
              <SearchableSelect
                value={employeeId}
                onChange={setEmployeeId}
                options={props.employees.map((em) => ({
                  value: em.id,
                  label: em.name,
                  keywords: em.id,
                }))}
                placeholder="选择或搜索员工…"
                required
              />
            </label>
            <label>
              <FieldLabel required>Prompt</FieldLabel>
              <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} required />
            </label>

            <label>
              <FieldLabel required>周期类型</FieldLabel>
              <select value={preset} onChange={(e) => setPreset(e.target.value)}>
                <option value="daily">每日</option>
                <option value="weekly">每周</option>
                <option value="monthly">每月</option>
                <option value="yearly">每年</option>
                <option value="custom">自定义 cron</option>
              </select>
            </label>

            {preset !== 'custom' && (
              <>
                <div>
                  <div className="muted" style={{ fontSize: 12, marginBottom: 6 }}>
                    快捷时刻（点一下即可，也可在下方精确选）
                  </div>
                  <div className="auto-chip-row">
                    {TIME_QUICK.map((t) => {
                      const active = hour === t.h && minute === t.m
                      return (
                        <button
                          key={t.label}
                          type="button"
                          className={active ? 'auto-chip is-active' : 'auto-chip'}
                          onClick={() => {
                            setHour(t.h)
                            setMinute(t.m)
                          }}
                        >
                          {t.label}
                        </button>
                      )
                    })}
                  </div>
                </div>
                <div className="auto-time-row">
                  <label>
                    时（0–23，{timeZoneLabel()}）
                    <select value={hour} onChange={(e) => setHour(Number(e.target.value))}>
                      {Array.from({ length: 24 }, (_, i) => (
                        <option key={i} value={i}>
                          {pad2(i)}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    分（0–59）
                    <select value={minute} onChange={(e) => setMinute(Number(e.target.value))}>
                      {Array.from({ length: 60 }, (_, i) => (
                        <option key={i} value={i}>
                          {pad2(i)}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
              </>
            )}

            {preset === 'weekly' && (
              <label>
                星期
                <select value={weekday} onChange={(e) => setWeekday(Number(e.target.value))}>
                  {WEEKDAYS.map((w) => (
                    <option key={w.v} value={w.v}>
                      {w.label}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {preset === 'monthly' && (
              <label>
                每月几号
                <select value={day} onChange={(e) => setDay(Number(e.target.value))}>
                  {Array.from({ length: 31 }, (_, i) => (
                    <option key={i + 1} value={i + 1}>
                      {i + 1} 日
                    </option>
                  ))}
                </select>
              </label>
            )}
            {preset === 'yearly' && (
              <div className="auto-time-row">
                <label>
                  月份
                  <select value={month} onChange={(e) => setMonth(Number(e.target.value))}>
                    {MONTHS.map((m) => (
                      <option key={m.v} value={m.v}>
                        {m.label}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  日期
                  <select value={day} onChange={(e) => setDay(Number(e.target.value))}>
                    {Array.from({ length: 31 }, (_, i) => (
                      <option key={i + 1} value={i + 1}>
                        {i + 1} 日
                      </option>
                    ))}
                  </select>
                </label>
              </div>
            )}
            {preset === 'custom' && (
              <label>
                <FieldLabel required>Cron（分 时 日 月 周）</FieldLabel>
                <input value={expr} onChange={(e) => setExpr(e.target.value)} placeholder="30 14 * * 1" required />
              </label>
            )}

            <div className="auto-cron-preview">
              将按 {timeZoneLabel()}：<strong>{describeCron({ preset, hour, minute, weekday, day, month, expr: previewExpr })}</strong>
              <span className="mono" style={{ marginLeft: 8 }}>
                {previewExpr}
              </span>
            </div>

            <label>
              飞书通知 Chat ID
              <input value={chatId} onChange={(e) => setChatId(e.target.value)} placeholder="例如 oc_xxx，留空则默认使用数字员工绑定的飞书 OpenID 或群聊" />
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
                <th>调度说明</th>
                <th>状态</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {props.items.map((a) => {
                const cfg = (a.trigger_config || {}) as Record<string, unknown>
                return (
                  <tr key={a.id} className={a.enabled ? undefined : 'auto-row-off'}>
                    <td>{a.name}</td>
                    <td>{props.empMap[a.employee_id] || a.employee_id}</td>
                    <td>
                      <div>{describeCron(cfg)} · {timeZoneLabel(a.timezone || localTimeZone())}</div>
                      <span className="mono" style={{ fontSize: 11 }}>
                        {String(cfg.expr || '')}
                      </span>
                    </td>
                    <td>
                      <span className={a.enabled ? 'auto-status is-on' : 'auto-status is-off'}>
                        {a.enabled ? '启用中' : '已停用'}
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
                              className={a.enabled ? 'btn-warn btn-sm' : 'btn-success btn-sm'}
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
                              onClick={() => setDeleteTarget({ id: a.id, name: a.name, kind: '定时任务' })}
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
                  {formatDateTime(r.created_at)} · {r.status} · job={r.job_id || '-'} · {r.trigger_source}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
      {deleteTarget && (
        <DeleteConfirmModal
          target={deleteTarget}
          busy={deleting}
          onCancel={() => setDeleteTarget(null)}
          onConfirm={() => {
            setDeleting(true)
            void deleteAutomation(deleteTarget.id)
              .then(() => {
                setDeleteTarget(null)
                props.onReload()
              })
              .catch((e) => props.onError(e instanceof Error ? e.message : '删除失败'))
              .finally(() => setDeleting(false))
          }}
        />
      )}
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
  const today = zonedYMD()
  const [year, setYear] = useState(today.year)
  const [month, setMonth] = useState(today.month)
  const [selected, setSelected] = useState(formatDate(today.year, today.month, today.day))
  const [autoId, setAutoId] = useState('')
  const [dayItems, setDayItems] = useState<CalendarItem[]>([])
  const [monthCounts, setMonthCounts] = useState<Record<string, number>>({})
  const [drafts, setDrafts] = useState<Array<{ employee_id: string; prompt: string }>>([{ employee_id: '', prompt: '' }])
  const [runClock, setRunClock] = useState('09:00')
  const [newName, setNewName] = useState('默认日历')
  const [chatId, setChatId] = useState('')

  // 只认日历类型，避免切 Tab 瞬间复用定时/Webhook 列表 ID
  const calendarAutos = useMemo(
    () => props.items.filter((a) => a.trigger_type === 'calendar'),
    [props.items],
  )
  const dim = daysInMonth(year, month)
  const firstDow = new Date(year, month, 1).getDay()
  const todayParts = zonedYMD()
  const todayStr = formatDate(todayParts.year, todayParts.month, todayParts.day)

  useEffect(() => {
    if (autoId && !calendarAutos.some((a) => a.id === autoId)) {
      setAutoId(calendarAutos[0]?.id ?? '')
      return
    }
    if (!autoId && calendarAutos.length > 0) setAutoId(calendarAutos[0].id)
  }, [calendarAutos, autoId])

  async function refreshMonthDots(id: string, y: number, m: number) {
    try {
      const r = await listCalendarItems(id)
      const prefix = `${y}-${pad2(m + 1)}-`
      const counts: Record<string, number> = {}
      for (const it of r.items ?? []) {
        const rd = normalizeRunDate(it.run_date)
        if (!rd.startsWith(prefix)) continue
        counts[rd] = (counts[rd] || 0) + 1
      }
      setMonthCounts(counts)
    } catch {
      setMonthCounts({})
    }
  }

  useEffect(() => {
    if (!autoId) {
      setMonthCounts({})
      return
    }
    void refreshMonthDots(autoId, year, month)
  }, [autoId, year, month])

  useEffect(() => {
    if (!autoId || !selected) return
    void listCalendarItems(autoId, selected)
      .then((r) => {
        const list = r.items ?? []
        setDayItems(list)
        setRunClock(list[0]?.run_clock || '09:00')
        setDrafts(list.length ? list.map((i) => ({ employee_id: i.employee_id, prompt: i.prompt })) : [{ employee_id: '', prompt: '' }])
      })
      .catch((e) => props.onError(e instanceof Error ? e.message : '加载日历失败'))
  }, [autoId, selected])

  function shiftMonth(delta: number) {
    const d = new Date(year, month + delta, 1)
    setYear(d.getFullYear())
    setMonth(d.getMonth())
  }

  async function ensureCalendar() {
    const hit = calendarAutos.find((a) => a.id === autoId)
    if (hit) return hit.id
    const res = await createAutomation({
      name: newName || '默认日历',
      trigger_type: 'calendar',
      notify_chat_id: chatId,
      timezone: localTimeZone(),
      trigger_config: {},
    })
    props.onReload()
    setAutoId(res.item.id)
    return res.item.id
  }

  async function saveDay() {
    props.onError('')
    try {
      const clean = drafts.filter((d) => d.employee_id && d.prompt)
      if (clean.length === 0) {
        props.onError('请至少填写一步：数字员工与 Prompt')
        return
      }
      if (!/^\d{2}:\d{2}$/.test(runClock)) {
        props.onError('请填写当天执行时间，格式 HH:MM')
        return
      }
      const id = await ensureCalendar()
      await putCalendarItems(id, selected, clean, runClock)
      props.onMsg(`${selected} ${runClock} 已保存 ${clean.length} 条`)
      const r = await listCalendarItems(id, selected)
      setDayItems(r.items ?? [])
      await refreshMonthDots(id, year, month)
    } catch (e) {
      props.onError(e instanceof Error ? e.message : '保存失败')
    }
  }

  return (
    <div className="detail-grid">
      <div className="panel">
        <div className="auto-cal-nav">
          <button type="button" className="btn-ghost btn-sm" onClick={() => shiftMonth(-1)} aria-label="上一月">
            ‹
          </button>
          <div className="auto-cal-nav-title">
            <select
              value={year}
              onChange={(e) => setYear(Number(e.target.value))}
              aria-label="年份"
            >
              {Array.from({ length: 11 }, (_, i) => zonedYMD().year - 5 + i).map((y) => (
                <option key={y} value={y}>
                  {y} 年
                </option>
              ))}
            </select>
            <select value={month} onChange={(e) => setMonth(Number(e.target.value))} aria-label="月份">
              {MONTHS.map((m) => (
                <option key={m.v} value={m.v - 1}>
                  {m.label}
                </option>
              ))}
            </select>
          </div>
          <button type="button" className="btn-ghost btn-sm" onClick={() => shiftMonth(1)} aria-label="下一月">
            ›
          </button>
          <button
            type="button"
            className="btn-ghost btn-sm"
            onClick={() => {
              const t = zonedYMD()
              setYear(t.year)
              setMonth(t.month)
              setSelected(formatDate(t.year, t.month, t.day))
            }}
          >
            今天
          </button>
          <span className="muted" style={{ fontSize: 12 }}>{timeZoneLabel()}</span>
        </div>

        <div className="stack-form" style={{ marginBottom: '0.75rem' }}>
          <label>
            日历规则
            <SearchableSelect
              value={autoId}
              onChange={setAutoId}
              options={[
                { value: '', label: '（新建）' },
                ...calendarAutos.map((a) => ({
                  value: a.id,
                  label: a.name,
                  keywords: a.id,
                })),
              ]}
              placeholder="选择日历规则…"
            />
          </label>
          {!autoId && props.writable && (
            <>
              <label>
                新日历名称
                <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="默认日历" />
              </label>
              <label>
                飞书 Chat ID
                <input value={chatId} onChange={(e) => setChatId(e.target.value)} placeholder="例如 oc_xxx，留空则默认使用数字员工绑定的飞书 OpenID 或群聊" />
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
            <div key={`e${i}`} className="auto-cal-pad" />
          ))}
          {Array.from({ length: dim }).map((_, i) => {
            const dayNum = i + 1
            const ds = formatDate(year, month, dayNum)
            const active = ds === selected
            const isToday = ds === todayStr
            const count = monthCounts[ds] || 0
            return (
              <button
                key={ds}
                type="button"
                aria-pressed={active}
                className={[
                  'auto-cal-cell',
                  active ? 'is-selected' : '',
                  isToday ? 'is-today' : '',
                  count > 0 ? 'has-tasks' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={() => setSelected(ds)}
              >
                <span className="auto-cal-num">{dayNum}</span>
                {count > 0 ? (
                  <span className="auto-cal-dots" title={`${count} 条任务`}>
                    {count > 3 ? (
                      <span className="auto-cal-count">{count}</span>
                    ) : (
                      Array.from({ length: Math.min(count, 3) }).map((_, di) => (
                        <i key={di} className="auto-cal-dot" />
                      ))
                    )}
                  </span>
                ) : (
                  <span className="auto-cal-dots muted-slot" />
                )}
              </button>
            )
          })}
        </div>
        <p className="muted" style={{ marginTop: 10, marginBottom: 0, fontSize: 12 }}>
          有圆点 / 数字的日期已挂任务；选中后在右侧编辑当日链。
        </p>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>{selected} 任务链</h2>
            <p>已存 {dayItems.length} 条 · 到达当天时刻后启动第 1 步，上一条成功后再触发下一步 · {timeZoneLabel()}</p>
          </div>
        </div>
        <div className="auto-time-row" style={{ marginBottom: '0.75rem' }}>
          <label>
            当天执行时间（{timeZoneLabel()}）
            <input type="time" value={runClock} onChange={(e) => setRunClock(e.target.value)} required />
          </label>
        </div>

        {drafts.map((d, idx) => (
          <div key={idx} className="stack-form auto-step-block">
            <div className="muted" style={{ fontWeight: 600 }}>
              第 {idx + 1} 步
            </div>
            <label>
              <FieldLabel required>数字员工</FieldLabel>
              <SearchableSelect
                value={d.employee_id}
                onChange={(val) => {
                  const next = [...drafts]
                  next[idx] = { ...next[idx], employee_id: val }
                  setDrafts(next)
                }}
                options={props.employees.map((em) => ({
                  value: em.id,
                  label: em.name,
                  keywords: em.id,
                }))}
                placeholder="选择员工…"
                disabled={!props.writable}
                required
              />
            </label>
            <label>
              <FieldLabel required>Prompt</FieldLabel>
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
  const [secretBanner, setSecretBanner] = useState<{
    id: string
    name: string
    pathToken: string
    secrets: Record<string, string>
  } | null>(null)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [runsOf, setRunsOf] = useState<string | null>(null)
  const [runs, setRuns] = useState<AutomationRun[]>([])
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [listExpanded, setListExpanded] = useState(false)

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
        timezone: localTimeZone(),
        trigger_config: {
          auth_modes: ['hmac', 'bearer'],
          ip_allowlist: ipList
            .split(/[\s,]+/)
            .map((s) => s.trim())
            .filter(Boolean),
          rate_limit_per_min: rate,
        },
      })
      const cfg = (res.item.trigger_config || {}) as Record<string, unknown>
      setSecretBanner({
        id: res.item.id,
        name: res.item.name,
        pathToken: String(cfg.path_token || ''),
        secrets: res.secrets ?? {},
      })
      setExpandedId(res.item.id)
      props.onMsg('Webhook 已创建，请立即复制下方密钥（刷新后不可再查看明文）')
      setName('')
      setPrompt('')
      props.onReload()
    } catch (err) {
      props.onError(err instanceof Error ? err.message : '创建失败')
    }
  }

  const endpointOf = (pathToken: string) => `${window.location.origin}/api/integrations/automation/hooks/${pathToken}`

  const secretPanel = secretBanner ? (
    <div className="auto-secret-banner" role="status">
      <div className="auto-secret-banner-head">
        <strong>密钥（仅此一次）· {secretBanner.name}</strong>
        <button type="button" className="btn-ghost btn-sm" onClick={() => setSecretBanner(null)}>
          关闭
        </button>
      </div>
      <p className="muted" style={{ margin: '0 0 0.75rem', fontSize: 12 }}>
        明文只在此刻可见。请立刻复制 HMAC / Bearer；关闭或刷新后只能点「轮换密钥」重新生成。列表里只会长期显示 Endpoint 与鉴权方式。
      </p>
      <div className="stack-form">
        <label>
          Endpoint
          <div className="auto-copy-row">
            <code className="mono">{endpointOf(secretBanner.pathToken)}</code>
            <button
              type="button"
              className="btn-ghost btn-sm"
              onClick={() => {
                void copyText(endpointOf(secretBanner.pathToken)).then(() => props.onMsg('已复制 Endpoint'))
              }}
            >
              复制
            </button>
          </div>
        </label>
        {secretBanner.secrets.hmac_secret && (
          <label>
            HMAC Secret（签名密钥）
            <div className="auto-copy-row">
              <code className="mono">{secretBanner.secrets.hmac_secret}</code>
              <button
                type="button"
                className="btn-ghost btn-sm"
                onClick={() => {
                  void copyText(secretBanner.secrets.hmac_secret).then(() => props.onMsg('已复制 HMAC'))
                }}
              >
                复制
              </button>
            </div>
          </label>
        )}
        {secretBanner.secrets.bearer_token && (
          <label>
            Bearer Token
            <div className="auto-copy-row">
              <code className="mono">{secretBanner.secrets.bearer_token}</code>
              <button
                type="button"
                className="btn-ghost btn-sm"
                onClick={() => {
                  void copyText(secretBanner.secrets.bearer_token).then(() => props.onMsg('已复制 Bearer'))
                }}
              >
                复制
              </button>
            </div>
          </label>
        )}
      </div>
    </div>
  ) : null

  return (
    <div className="auto-webhook-wrap">
      {secretPanel}
      <div className={listExpanded ? 'detail-grid is-list-expanded' : 'detail-grid'}>
      {props.writable && !listExpanded && (
        <div className="panel">
          <div className="panel-header">
            <div>
              <h2>
                <IconPlus size={16} /> 新建 Webhook
              </h2>
              <p>创建成功后，本页最上方会出现黄色密钥条（HMAC + Bearer，仅一次）</p>
            </div>
          </div>
          <form onSubmit={(e) => void onCreate(e)} className="stack-form">
            <label>
              <FieldLabel required>名称</FieldLabel>
              <input value={name} onChange={(e) => setName(e.target.value)} required />
            </label>
            <label>
              <FieldLabel required>数字员工</FieldLabel>
              <SearchableSelect
                value={employeeId}
                onChange={setEmployeeId}
                options={props.employees.map((em) => ({
                  value: em.id,
                  label: em.name,
                  keywords: em.id,
                }))}
                placeholder="选择或搜索员工…"
                required
              />
            </label>
            <label>
              <FieldLabel required>Prompt</FieldLabel>
              <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} required />
            </label>
            <label>
              飞书 Chat ID
              <input value={chatId} onChange={(e) => setChatId(e.target.value)} placeholder="例如 oc_xxx，留空则默认使用数字员工绑定的飞书 OpenID 或群聊" />
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
        </div>
      )}

      <div className="panel" style={props.writable && !listExpanded ? undefined : { gridColumn: '1 / -1' }}>
        <div className="panel-header">
          <div>
            <h2>Webhook 列表</h2>
            <p>共 {props.items.length} 条 · 鉴权/限流/Endpoint 常驻；明文密钥仅创建或轮换时出现在上方</p>
          </div>
          {props.writable && (
            <button
              type="button"
              className="btn-ghost btn-sm auto-expand-btn"
              title={listExpanded ? '缩小列表' : '放大列表'}
              aria-label={listExpanded ? '缩小列表' : '放大列表'}
              onClick={() => setListExpanded((v) => !v)}
            >
              {listExpanded ? <IconMinimize size={16} /> : <IconMaximize size={16} />}
            </button>
          )}
        </div>
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>名称</th>
                <th>员工</th>
                <th>状态</th>
                <th>鉴权</th>
                <th>限流</th>
                <th>Endpoint</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {props.items.map((a) => {
                const cfg = (a.trigger_config || {}) as Record<string, unknown>
                const token = String(cfg.path_token || '')
                const modes = Array.isArray(cfg.auth_modes) ? (cfg.auth_modes as string[]) : []
                const allow = Array.isArray(cfg.ip_allowlist) ? (cfg.ip_allowlist as string[]) : []
                const rateLim = Number(cfg.rate_limit_per_min || 60)
                const url = endpointOf(token)
                const open = expandedId === a.id
                return (
                  <Fragment key={a.id}>
                    <tr className={a.enabled ? undefined : 'auto-row-off'}>
                      <td>{a.name}</td>
                      <td>{props.empMap[a.employee_id] || a.employee_id || '—'}</td>
                      <td>
                        <span className={a.enabled ? 'auto-status is-on' : 'auto-status is-off'}>
                          {a.enabled ? '启用中' : '已停用'}
                        </span>
                      </td>
                      <td>
                        <div className="auto-chip-row">
                          {modes.length ? (
                            modes.map((m) => (
                              <span key={m} className="auto-chip">
                                {String(m).toUpperCase()}
                              </span>
                            ))
                          ) : (
                            <span className="muted">—</span>
                          )}
                        </div>
                        {allow.length > 0 && (
                          <div className="muted" style={{ fontSize: 11, marginTop: 4 }}>
                            IP: {allow.join(', ')}
                          </div>
                        )}
                      </td>
                      <td>{rateLim}/分</td>
                      <td>
                        <button
                          type="button"
                          className="btn-ghost btn-sm"
                          onClick={() => {
                            void copyText(url).then(() => props.onMsg('已复制 URL'))
                          }}
                        >
                          复制 URL
                        </button>
                        <div className="mono" style={{ fontSize: 11, marginTop: 4, wordBreak: 'break-all' }}>
                          {url}
                        </div>
                      </td>
                      <td className="col-actions">
                        <div className="table-actions">
                          {props.writable && (
                            <button
                              type="button"
                              className={a.enabled ? 'btn-warn btn-sm' : 'btn-success btn-sm'}
                              onClick={() => {
                                void updateAutomation(a.id, { enabled: !a.enabled })
                                  .then(() => props.onReload())
                                  .catch((e) => props.onError(e instanceof Error ? e.message : '更新失败'))
                              }}
                            >
                              {a.enabled ? '停用' : '启用'}
                            </button>
                          )}
                          <button type="button" className="btn-ghost btn-sm" onClick={() => setExpandedId(open ? null : a.id)}>
                            {open ? '收起' : '详情'}
                          </button>
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
                                      setSecretBanner({
                                        id: a.id,
                                        name: a.name,
                                        pathToken: token,
                                        secrets: r.secrets,
                                      })
                                      props.onMsg('密钥已轮换，请立即保存上方明文')
                                    })
                                    .catch((e) => props.onError(e instanceof Error ? e.message : '轮换失败'))
                                }}
                              >
                                轮换密钥
                              </button>
                              <button
                                type="button"
                                className="btn-danger btn-sm"
                                onClick={() => setDeleteTarget({ id: a.id, name: a.name, kind: 'Webhook' })}
                              >
                                <IconTrash size={14} />
                              </button>
                            </>
                          )}
                        </div>
                      </td>
                    </tr>
                    {open && (
                      <tr>
                        <td colSpan={7}>
                          <div className="auto-hook-detail">
                            <div>
                              <strong>Prompt</strong>
                              <pre className="mono">{a.prompt || '—'}</pre>
                            </div>
                            <div>
                              <strong>飞书 Chat</strong> <span className="mono">{a.notify_chat_id || '默认使用关联员工 (OpenID / 群聊)'}</span>
                            </div>
                            <div>
                              <strong>调用示例</strong>
                              <pre className="mono">{`# Bearer
curl -X POST '${url}' \\
  -H 'Authorization: Bearer <token>' \\
  -H 'X-Idempotency-Key: unique-1' \\
  -d '{}'

# HMAC：Signature = hex(hmac_sha256(secret, timestamp + "." + body))
BODY='{}'
TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
SIG=$(printf '%s' "$TS.$BODY" | openssl dgst -sha256 -hmac '<hmac_secret>' | awk '{print $2}')
curl -X POST '${url}' \\
  -H "X-AIE-Timestamp: $TS" \\
  -H "X-AIE-Signature: $SIG" \\
  -H 'X-Idempotency-Key: unique-1' \\
  -H 'Content-Type: application/json' \\
  -d "$BODY"`}</pre>
                            </div>
                          </div>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
              {props.items.length === 0 && (
                <tr>
                  <td colSpan={7} className="empty-tip">
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
                  {formatDateTime(r.created_at)} · {r.status} · {r.error || 'ok'}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
      </div>
      {deleteTarget && (
        <DeleteConfirmModal
          target={deleteTarget}
          busy={deleting}
          onCancel={() => setDeleteTarget(null)}
          onConfirm={() => {
            setDeleting(true)
            void deleteAutomation(deleteTarget.id)
              .then(() => {
                if (secretBanner?.id === deleteTarget.id) setSecretBanner(null)
                setDeleteTarget(null)
                props.onReload()
              })
              .catch((e) => props.onError(e instanceof Error ? e.message : '删除失败'))
              .finally(() => setDeleting(false))
          }}
        />
      )}
    </div>
  )
}
