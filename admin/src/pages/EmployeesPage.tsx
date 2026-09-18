/**
 * Employees 列表与创建。
 */
import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'

type Emp = {
  id: string
  name: string
  status: string
  default_provider: string
  workstation_id: string
  workspace_id: string
}

export function EmployeesPage() {
  const [items, setItems] = useState<Emp[]>([])
  const [name, setName] = useState('')
  const [error, setError] = useState('')

  async function load() {
    const data = await apiGet<{ items: Emp[] }>('/employees')
    setItems(data.items ?? [])
  }

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    try {
      await apiPost('/employees', { name })
      setName('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    }
  }

  return (
    <section>
      <h1>Employees</h1>
      <p className="muted">Employee ≠ 进程；可无 Active Session。</p>
      <form className="inline-form" onSubmit={onCreate}>
        <input placeholder="名称" value={name} onChange={(e) => setName(e.target.value)} required />
        <button type="submit">创建</button>
      </form>
      {error ? <p className="error">{error}</p> : null}
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>名称</th>
            <th>状态</th>
            <th>Provider</th>
          </tr>
        </thead>
        <tbody>
          {items.map((e) => (
            <tr key={e.id}>
              <td>
                <Link to={`/employees/${e.id}`}>{e.id}</Link>
              </td>
              <td>{e.name}</td>
              <td>{e.status}</td>
              <td>{e.default_provider || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
