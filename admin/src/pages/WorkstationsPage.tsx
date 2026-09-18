/**
 * Workstations 列表。
 */
import { useEffect, useState } from 'react'
import { apiGet } from '../api/client'

type WS = {
  id: string
  name: string
  status: string
  cert_status: string
  fingerprint: string
  last_heartbeat_at?: string
}

export function WorkstationsPage() {
  const [items, setItems] = useState<WS[]>([])
  useEffect(() => {
    void apiGet<{ items: WS[] }>('/workstations').then((d) => setItems(d.items ?? []))
  }, [])
  return (
    <section>
      <h1>Workstations</h1>
      <p className="muted">状态来自心跳；吊销请调用证书吊销 API。</p>
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>状态</th>
            <th>证书</th>
            <th>Fingerprint</th>
          </tr>
        </thead>
        <tbody>
          {items.map((w) => (
            <tr key={w.id}>
              <td>{w.id}</td>
              <td>{w.status}</td>
              <td>{w.cert_status || '—'}</td>
              <td className="mono">{w.fingerprint ? `${w.fingerprint.slice(0, 12)}…` : '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
