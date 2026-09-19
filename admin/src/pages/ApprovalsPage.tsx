/**
 * Approvals 审批中心：高风险与敏感操作人工审核流。
 */
import { useEffect, useState } from 'react'
import { apiGet, apiPost } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { IconCheckCircle, IconRefresh, IconKey } from '../components/Icons'
import { EntityName } from '../components/EntityName'

type Approval = {
  id: string
  job_id: string
  action: string
  status: string
  critical: boolean
  reason: string
  created_at: string
}

type JobItem = {
  id: string
  prompt: string
}

export function ApprovalsPage() {
  const [items, setItems] = useState<Approval[]>([])
  const [jobMap, setJobMap] = useState<Record<string, string>>({})
  const [totp, setTotp] = useState('')
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [totpEnabled, setTotpEnabled] = useState(false)
  const [loading, setLoading] = useState(false)

  const reload = () => {
    setLoading(true)
    Promise.all([
      apiGet<{ items: Approval[] }>('/approvals').catch((e: Error) => {
        setErr(e.message)
        return { items: [] as Approval[] }
      }),
      apiGet<{ items: JobItem[] }>('/jobs').catch(() => ({ items: [] as JobItem[] })),
      apiGet<{ enabled: boolean }>('/auth/totp').catch(() => ({ enabled: false })),
    ])
      .then(([appData, jobsData, totpData]) => {
        setItems(appData.items ?? [])
        setJobMap(Object.fromEntries((jobsData.items ?? []).map((j) => [j.id, j.prompt])))
        setTotpEnabled(!!totpData.enabled)
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    reload()
  }, [])

  const enroll = async () => {
    setErr('')
    setMsg('')
    try {
      const r = await apiPost<{ secret: string }>('/auth/totp/enroll')
      alert(`请使用身份验证器 App 扫描或绑定此密钥（仅显示一次）:\n${r.secret}`)
      setMsg('已成功申请 TOTP 绑定密钥')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const act = async (id: string, kind: 'approve' | 'reject') => {
    setErr('')
    setMsg('')
    try {
      if (kind === 'approve') {
        await apiPost(`/approvals/${id}/approve`, { totp })
        setMsg('审批单已同意通过，任务将恢复执行')
      } else {
        await apiPost(`/approvals/${id}/reject`)
        setMsg('审批单已驳回，任务将终止执行')
      }
      setTotp('')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>人工审批管控中心 (Approvals)</h1>
          <p>针对命中 ASK 策略、高风险高危指令及生产变更触发严格人工双因子鉴权审批</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => reload()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新单据</span>
        </button>
      </header>

      {err ? <div className="error">{err}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>双因子安全认证 (TOTP) 状态</h2>
            <p>高风险指令批准需强制输入 6 位动态口令，防止未授权越权操作</p>
          </div>
          <div>
            {totpEnabled ? (
              <span className="badge badge-ok">TOTP 双因子已生效</span>
            ) : (
              <button type="button" className="btn-ghost" onClick={() => void enroll()}>
                <IconKey size={15} />
                <span>立即开通 TOTP 认证器</span>
              </button>
            )}
          </div>
        </div>

        <div className="inline-form" style={{ background: '#f8fafc', padding: '1rem', borderRadius: '8px', border: '1px solid var(--border-subtle)' }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', fontSize: '0.86rem', fontWeight: 600 }}>
            <span>当前审批动态验证码 (TOTP):</span>
            <input
              placeholder="请输入 6 位动态口令"
              value={totp}
              onChange={(e) => setTotp(e.target.value)}
              style={{ width: '200px' }}
            />
          </label>
          <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>（若为普通非高危单据或未强制 TOTP，可留空直接批准）</span>
        </div>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>待办与历史审批工单</h2>
            <p>共查出 {items.length} 笔审批流记录</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>审批单据</th>
                <th>关联任务需求</th>
                <th>触发动作 (Action)</th>
                <th>风险等级</th>
                <th>申请原因与上下文</th>
                <th>申请时间</th>
                <th>审核决策操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.id}>
                  <td>
                    <EntityName name={`审批单 #${a.id.slice(0, 8)}`} id={a.id} />
                  </td>
                  <td style={{ maxWidth: '240px' }}>
                    <EntityName
                      name={jobMap[a.job_id] || `任务 #${a.job_id.slice(0, 8)}`}
                      id={a.job_id}
                      to={`/jobs/${a.job_id}`}
                    />
                  </td>
                  <td><span className="mono">{a.action}</span></td>
                  <td>
                    {a.critical ? <StatusBadge status="CRITICAL" /> : <span style={{ color: 'var(--text-muted)' }}>常规</span>}
                  </td>
                  <td style={{ maxWidth: '300px', color: 'var(--text-secondary)' }}>{a.reason || '无说明'}</td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {new Date(a.created_at).toLocaleString()}
                  </td>
                  <td>
                    {a.status === 'PENDING' ? (
                      <div style={{ display: 'flex', gap: '0.4rem' }}>
                        <button type="button" className="btn-sm" onClick={() => void act(a.id, 'approve')}>
                          <IconCheckCircle size={14} />
                          <span>批准放行</span>
                        </button>
                        <button type="button" className="btn-danger btn-sm" onClick={() => void act(a.id, 'reject')}>
                          驳回阻断
                        </button>
                      </div>
                    ) : (
                      <StatusBadge status={a.status} />
                    )}
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={7} className="empty-tip">当前暂无待处理审批单据</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
