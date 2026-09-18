/**
 * Approvals 审批中心（M7）。
 */
import { useEffect, useState } from 'react'
import { apiGet, apiPost } from '../api/client'

type Approval = {
  id: string
  job_id: string
  action: string
  status: string
  critical: boolean
  reason: string
  created_at: string
}

export function ApprovalsPage() {
  const [items, setItems] = useState<Approval[]>([])
  const [totp, setTotp] = useState('')
  const [err, setErr] = useState('')
  const [totpEnabled, setTotpEnabled] = useState(false)

  const reload = () => {
    void apiGet<{ items: Approval[] }>('/approvals')
      .then((d) => setItems(d.items ?? []))
      .catch((e: Error) => setErr(e.message))
    void apiGet<{ enabled: boolean }>('/auth/totp')
      .then((d) => setTotpEnabled(!!d.enabled))
      .catch(() => undefined)
  }

  useEffect(() => {
    reload()
  }, [])

  const enroll = async () => {
    setErr('')
    try {
      const r = await apiPost<{ secret: string }>('/auth/totp/enroll')
      alert(`请绑定 Authenticator，密钥（仅显示一次）:\n${r.secret}`)
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const act = async (id: string, kind: 'approve' | 'reject') => {
    setErr('')
    try {
      if (kind === 'approve') {
        await apiPost(`/approvals/${id}/approve`, { totp })
      } else {
        await apiPost(`/approvals/${id}/reject`)
      }
      setTotp('')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  return (
    <section>
      <h1>Approvals</h1>
      <p className="muted">ASK → 人工批准；CRITICAL 需 TOTP。未配置 TOTP 时 CRITICAL 直接 DENY。</p>
      <div className="row" style={{ gap: 8, marginBottom: 12 }}>
        <button type="button" onClick={() => void enroll()}>
          {totpEnabled ? '重新绑定 TOTP' : '绑定 TOTP'}
        </button>
        <input
          placeholder="TOTP 码（批准 CRITICAL）"
          value={totp}
          onChange={(e) => setTotp(e.target.value)}
          style={{ maxWidth: 200 }}
        />
      </div>
      {err ? <p className="error">{err}</p> : null}
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Action</th>
            <th>Job</th>
            <th>Status</th>
            <th>Critical</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map((a) => (
            <tr key={a.id}>
              <td>{a.id}</td>
              <td>{a.action}</td>
              <td>{a.job_id}</td>
              <td>{a.status}</td>
              <td>{a.critical ? 'Y' : ''}</td>
              <td>
                {a.status === 'PENDING' ? (
                  <>
                    <button type="button" onClick={() => void act(a.id, 'approve')}>
                      Approve
                    </button>{' '}
                    <button type="button" onClick={() => void act(a.id, 'reject')}>
                      Reject
                    </button>
                  </>
                ) : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
