/**
 * Dashboard：对齐设计文档 §9 — 统计卡 + Active Jobs + Workstations 资源。
 * 使用轮询刷新（SSE 可选，避免假流干扰演示）。
 */
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet } from '../api/client'

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
  cpu_percent: number
  memory_percent: number
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
}

/** 状态 → badge 样式类 */
function statusClass(s: string): string {
  const u = (s || '').toUpperCase()
  if (u === 'ONLINE' || u === 'RUNNING' || u === 'SUCCESS' || u === 'ACTIVE') return 'badge badge-ok'
  if (u === 'STARTING' || u === 'WAITING' || u === 'WAITING_APPROVAL' || u === 'BUSY') return 'badge badge-warn'
  if (u === 'OFFLINE' || u === 'FAILED' || u === 'ERROR' || u === 'REVOKED' || u === 'TIMEOUT') return 'badge badge-err'
  return 'badge'
}

export function DashboardPage() {
  const [data, setData] = useState<Dash | null>(null)
  const [error, setError] = useState('')
  const [updatedAt, setUpdatedAt] = useState('')

  async function refresh() {
    try {
      setData(await apiGet<Dash>('/dashboard'))
      setUpdatedAt(new Date().toLocaleTimeString())
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

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>AI Employee Control Center</h1>
          <p className="muted">系统实时状态 · 每 5s 刷新{updatedAt ? ` · ${updatedAt}` : ''}</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => void refresh()}>
          刷新
        </button>
      </header>
      {error ? <p className="error">{error}</p> : null}

      {data ? (
        <>
          <div className="stat-grid">
            <Stat label="Employees" value={data.employees} />
            <Stat label="Workstations" value={data.workstations} />
            <Stat label="Jobs" value={data.jobs} />
            <Stat label="Online" value={data.workstations_online} />
            <Stat label="Busy" value={data.busy} />
            <Stat label="Errors" value={data.errors} />
          </div>

          <div className="panel">
            <h2>Active Jobs</h2>
            <table className="table">
              <thead>
                <tr>
                  <th>Job</th>
                  <th>Employee</th>
                  <th>Status</th>
                  <th>Prompt</th>
                </tr>
              </thead>
              <tbody>
                {(data.recent_active_jobs ?? []).length === 0 ? (
                  <tr>
                    <td colSpan={4} className="muted">
                      暂无活跃 Job
                    </td>
                  </tr>
                ) : (
                  (data.recent_active_jobs ?? []).map((j) => (
                    <tr key={j.id}>
                      <td>
                        <Link to={`/jobs/${j.id}`}>{j.id}</Link>
                      </td>
                      <td>
                        <Link to={`/employees/${j.employee_id}`}>{j.employee_id}</Link>
                      </td>
                      <td>
                        <span className={statusClass(j.status)}>{j.status}</span>
                      </td>
                      <td className="mono">{j.prompt || '—'}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>

          <div className="panel">
            <h2>Workstations</h2>
            <table className="table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Name</th>
                  <th>Status</th>
                  <th>CPU</th>
                  <th>RAM</th>
                </tr>
              </thead>
              <tbody>
                {(data.workstations_detail ?? []).length === 0 ? (
                  <tr>
                    <td colSpan={5} className="muted">
                      暂无 Workstation
                    </td>
                  </tr>
                ) : (
                  (data.workstations_detail ?? []).map((w) => (
                    <tr key={w.id}>
                      <td>
                        <Link to="/workstations">{w.id}</Link>
                      </td>
                      <td>{w.name || '—'}</td>
                      <td>
                        <span className={statusClass(w.status)}>{w.status}</span>
                      </td>
                      <td>{w.status === 'ONLINE' ? `${Math.round(w.cpu_percent ?? 0)}%` : '—'}</td>
                      <td>{w.status === 'ONLINE' ? `${Math.round(w.memory_percent ?? 0)}%` : '—'}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </>
      ) : (
        <p className="muted">加载中…</p>
      )}
    </section>
  )
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="stat">
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}
