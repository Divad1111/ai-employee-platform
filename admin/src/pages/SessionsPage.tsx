/**
 * Sessions 列表。
 */
import { useEffect, useState } from 'react'
import { apiGet } from '../api/client'

type Sess = {
  id: string
  employee_id: string
  status: string
  provider: string
}

export function SessionsPage() {
  const [items, setItems] = useState<Sess[]>([])
  useEffect(() => {
    void apiGet<{ items: Sess[] }>('/sessions').then((d) => setItems(d.items ?? []))
  }, [])
  return (
    <section>
      <h1>Sessions</h1>
      <p className="muted">V1：每 Employee 最多 1 个 Active Session。</p>
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Employee</th>
            <th>Status</th>
            <th>Provider</th>
          </tr>
        </thead>
        <tbody>
          {items.map((s) => (
            <tr key={s.id}>
              <td>{s.id}</td>
              <td>{s.employee_id}</td>
              <td>{s.status}</td>
              <td>{s.provider || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
