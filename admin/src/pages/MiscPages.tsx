/**
 * Audit Logs / Settings。
 */
import { useEffect, useState } from 'react'
import { apiGet } from '../api/client'

type AuditItem = {
  id: number
  actor_id: string
  action: string
  result: string
  created_at: string
}

export function AuditPage() {
  const [items, setItems] = useState<AuditItem[]>([])
  const [action, setAction] = useState('')
  useEffect(() => {
    const q = action ? `?action=${encodeURIComponent(action)}&limit=50` : '?limit=50'
    void apiGet<{ items: AuditItem[] }>(`/audit${q}`).then((d) => setItems(d.items ?? []))
  }, [action])
  return (
    <section>
      <header className="page-header">
        <div>
          <h1>Audit Logs</h1>
          <p className="muted">操作审计；可按 action 过滤。</p>
        </div>
      </header>
      <input placeholder="按 action 过滤" value={action} onChange={(e) => setAction(e.target.value)} />
      <table className="table">
        <thead>
          <tr>
            <th>Time</th>
            <th>Actor</th>
            <th>Action</th>
            <th>Result</th>
          </tr>
        </thead>
        <tbody>
          {items.map((a) => (
            <tr key={a.id}>
              <td>{a.created_at}</td>
              <td>{a.actor_id}</td>
              <td>{a.action}</td>
              <td>{a.result}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

export function SettingsPage() {
  const [data, setData] = useState<Record<string, unknown> | null>(null)
  useEffect(() => {
    void apiGet<Record<string, unknown>>('/settings').then(setData)
  }, [])
  return (
    <section>
      <header className="page-header">
        <div>
          <h1>Settings</h1>
          <p className="muted">Secret 类配置不在前端明文回显。</p>
        </div>
      </header>
      <pre className="panel mono">{data ? JSON.stringify(data, null, 2) : '加载中…'}</pre>
    </section>
  )
}
