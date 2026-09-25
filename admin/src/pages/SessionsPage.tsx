import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { IconTerminal, IconRefresh } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { PageFeatureGuide } from '../components/PageFeatureGuide'
import { formatDateTime } from '../lib/time'

type Sess = {
  id: string
  employee_id: string
  workstation_id?: string
  workspace_id?: string
  status: string
  provider: string
  created_at?: string
  last_activity_at?: string
}

export function SessionsPage() {
  const [items, setItems] = useState<Sess[]>([])
  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [loading, setLoading] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const [d, empData] = await Promise.all([
        apiGet<{ items: Sess[] }>('/sessions'),
        apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
      ])
      setItems(d.items ?? [])
      setEmployees(empData.items ?? [])
    } finally {
      setLoading(false)
    }
  }

  const empMap = Object.fromEntries(employees.map((e) => [e.id, e.name]))

  useEffect(() => {
    void load()
    const t = setInterval(() => void load().catch(() => undefined), 5000)
    return () => clearInterval(t)
  }, [])

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>运行会话管理 (Sessions)</h1>
          <p>数字员工在工作站上的 Agent 运行上下文（ACP / Provider），由任务触发按需创建</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => void load()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新会话</span>
        </button>
      </header>

      <PageFeatureGuide
        title="Agent 运行会话生命周期与隔离架构指引"
        summary="Session 代表数字员工在宿主工作站中拉起的交互式 Agent 子进程上下文（如 Cursor CLI agent acp），保持持久化记忆与多轮对话。"
        steps={[
          {
            step: '1',
            title: '动态绑定与冷启动拉起',
            desc: '任务分配后，工作站 Runtime 根据员工指定的 Provider (Cursor / Codex) 在工作区目录唤起 Agent 守护进程。',
            tag: '进程管理',
          },
          {
            step: '2',
            title: '状态迁移与健康复用',
            desc: '会话经历 STARTING → READY → BUSY 流转。任务执行完毕后回归 READY 状态，可承接连续多轮会话无需重复拉起。',
            tag: '长效复用',
          },
          {
            step: '3',
            title: '工作区隔离与算力保护',
            desc: '每个 Session 严格限定在对应项目的独立目录与子进程中，具备超时保护与安全沙箱限制。',
            tag: '安全隔离',
          },
        ]}
      />

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>活跃与历史会话</h2>
            <p>V1：同一数字员工同时最多一个 Active Session；多个 Job 可串行复用</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>会话标识</th>
                <th>关联数字员工</th>
                <th>工作站</th>
                <th>会话状态</th>
                <th>驱动引擎</th>
                <th>最近活动</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id}>
                  <td>
                    <EntityName
                      name={`会话 #${s.id.length > 12 ? s.id.slice(0, 12) : s.id}`}
                      id={s.id}
                      icon={<IconTerminal size={15} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td>
                    <EntityName
                      name={empMap[s.employee_id] || s.employee_id}
                      id={s.employee_id}
                      to={`/employees/${s.employee_id}`}
                    />
                  </td>
                  <td>
                    {s.workstation_id ? (
                      <Link to="/workstations" style={{ fontSize: '0.85rem' }}>{s.workstation_id}</Link>
                    ) : (
                      <span style={{ color: 'var(--text-muted)' }}>—</span>
                    )}
                  </td>
                  <td>
                    <StatusBadge status={s.status} />
                  </td>
                  <td>
                    <span className="badge" style={{ textTransform: 'capitalize' }}>
                      {s.provider || 'cursor'}
                    </span>
                  </td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {s.last_activity_at ? formatDateTime(s.last_activity_at) : '—'}
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={6} className="empty-tip">
                    暂无会话。派发任务后工作站会按需启动 Session 并上报到此。
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
