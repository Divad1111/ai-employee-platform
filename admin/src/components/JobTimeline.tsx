/**
 * 任务时间线：竖向节点连线，把状态迁移、会话和失败原因写成可读说明。
 */
import { useState } from 'react'
import { formatDateTime } from '../lib/time'
import { StatusBadge } from './StatusBadge'

type JobEvent = {
  id: number
  event_type: string
  payload: Record<string, string>
  created_at: string
}

const STATUS_LABEL: Record<string, string> = {
  CREATED: '已创建',
  QUEUED: '排队中',
  ASSIGNED: '已分配节点',
  STARTING: '启动中',
  RUNNING: '运行中',
  SUCCESS: '执行成功',
  FAILED: '执行失败',
  CANCELLED: '已取消',
  TIMEOUT: '执行超时',
  BLOCKED: '已受阻',
  WAITING_APPROVAL: '待审批',
  READY: '就绪',
  ERROR: '出错',
}

const SESSION_LABEL: Record<string, string> = {
  EVENT_TYPE_SESSION_STARTED: '会话开始启动',
  EVENT_TYPE_SESSION_READY: '会话已就绪',
  EVENT_TYPE_SESSION_ERROR: '会话出错',
  EVENT_TYPE_SESSION_STOPPED: '会话已停止',
}

function statusLabel(code: string) {
  return STATUS_LABEL[code] || code
}

function eventTone(ev: JobEvent): 'danger' | 'success' | 'warning' | 'info' | 'neutral' {
  const payload = ev.payload || {}
  const target = (payload.to || payload.status || '').toUpperCase()
  if (payload.error || payload.reason || target === 'FAILED' || target === 'TIMEOUT' || target === 'ERROR') {
    return 'danger'
  }
  if (target === 'SUCCESS' || target === 'READY' || ev.event_type === 'AGENT_REPLY') return 'success'
  if (target === 'STARTING' || target === 'RUNNING') return 'warning'
  if (ev.event_type === 'SESSION') return 'info'
  return 'neutral'
}

function eventTitle(ev: JobEvent) {
  const payload = ev.payload || {}
  if (ev.event_type === 'CREATED') return '任务已创建'
  if (ev.event_type === 'AGENT_REPLY') return 'Agent 回复'
  if (ev.event_type === 'SESSION') {
    return SESSION_LABEL[payload.event] || '会话事件'
  }
  if (ev.event_type === 'STATUS' && payload.from && payload.to) {
    return `${statusLabel(payload.from)} → ${statusLabel(payload.to)}`
  }
  return ev.event_type
}

function tokenLine(payload: Record<string, string>) {
  const input = payload.input_tokens
  const output = payload.output_tokens
  if (!input && !output) return ''
  const source = payload.token_source === 'agent' ? 'Agent 上报' : payload.token_source === 'estimate' ? '按文本估算' : payload.token_source
  const agent = payload.agent ? ` · ${payload.agent}` : ''
  return `输入 ${input || 0} / 输出 ${output || 0}${agent}${source ? `（${source}）` : ''}`
}

export function JobTimeline({ events }: { events: JobEvent[] }) {
  if (!events.length) {
    return <div className="empty-tip">暂无事件轨迹记录</div>
  }
  return (
    <ol className="job-timeline">
      {events.map((ev, index) => (
        <TimelineItem key={ev.id} ev={ev} last={index === events.length - 1} />
      ))}
    </ol>
  )
}

function TimelineItem({ ev, last }: { ev: JobEvent; last: boolean }) {
  const [raw, setRaw] = useState(false)
  const payload = ev.payload || {}
  const tone = eventTone(ev)
  const reply = payload.reply || ''
  const error = payload.error || payload.reason || ''
  const tokens = tokenLine(payload)
  const hasRaw = Object.keys(payload).length > 0

  return (
    <li className={`job-tl-item is-${tone}${last ? ' is-last' : ''}`}>
      <div className="job-tl-rail" aria-hidden="true">
        <span className="job-tl-dot" />
      </div>
      <div className="job-tl-card">
        <div className="job-tl-head">
          <strong>{eventTitle(ev)}</strong>
          <time dateTime={ev.created_at}>{formatDateTime(ev.created_at)}</time>
        </div>
        <div className="job-tl-meta">
          {payload.to ? <StatusBadge status={payload.to} /> : null}
          {payload.workstation_id ? <span>工作站 {payload.workstation_id}</span> : null}
          {payload.session_id ? <span>会话 {payload.session_id}</span> : null}
          {payload.agent && !tokens ? <span>Agent {payload.agent}</span> : null}
        </div>
        {error ? <div className="job-tl-error">{error}</div> : null}
        {tokens ? <div className="job-tl-note">{tokens}</div> : null}
        {payload.message && payload.message !== error ? <div className="job-tl-note">{payload.message}</div> : null}
        {reply ? <div className="job-tl-reply">{reply}</div> : null}
        {hasRaw ? (
          <button type="button" className="job-tl-raw-btn" onClick={() => setRaw((v) => !v)}>
            {raw ? '收起原始字段' : '原始字段'}
          </button>
        ) : null}
        {raw ? <pre className="job-tl-raw">{JSON.stringify(payload, null, 2)}</pre> : null}
      </div>
    </li>
  )
}
