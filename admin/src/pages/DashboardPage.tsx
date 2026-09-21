/**
 * Dashboard：对齐设计文档 §9 — 统计卡 + Active Jobs + Workstations 资源监控。
 */
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { IconRefresh } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { PageFeatureGuide } from '../components/PageFeatureGuide'

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

export function DashboardPage() {
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
            title: 'mTLS 双向安全保活监控',
            desc: '通过 gRPC 双向流与客户端证书验证，实时展示各宿主机节点的 CPU 负载率与内存占用率。',
            tag: '硬件监控',
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
                <p>实时心跳监控 · 调度资源决策依据</p>
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
                      <th style={{ width: '220px' }}>CPU 负载率</th>
                      <th style={{ width: '220px' }}>内存占用率</th>
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
                        <td>
                          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.78rem' }}>
                            <span>{w.cpu_percent.toFixed(1)}%</span>
                          </div>
                          <div className="progress-bar-wrap">
                            <div
                              className="progress-bar-fill"
                              style={{
                                width: `${Math.min(w.cpu_percent, 100)}%`,
                                background: w.cpu_percent > 85 ? 'var(--danger)' : 'var(--brand-500)',
                              }}
                            />
                          </div>
                        </td>
                        <td>
                          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.78rem' }}>
                            <span>{w.memory_percent.toFixed(1)}%</span>
                          </div>
                          <div className="progress-bar-wrap">
                            <div
                              className="progress-bar-fill"
                              style={{
                                width: `${Math.min(w.memory_percent, 100)}%`,
                                background: w.memory_percent > 85 ? 'var(--danger)' : 'var(--brand-500)',
                              }}
                            />
                          </div>
                        </td>
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
