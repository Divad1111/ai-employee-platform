/**
 * Jobs 列表与详情 Timeline。
 */
import { FormEvent, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'

type Job = {
  id: string
  employee_id: string
  status: string
  prompt: string
  idempotency_key: string
  timeout_sec: number
}

type JobEvent = {
  id: number
  event_type: string
  payload: Record<string, string>
  created_at: string
}

export function JobsPage() {
  const [items, setItems] = useState<Job[]>([])
  const [employeeId, setEmployeeId] = useState('')
  const [prompt, setPrompt] = useState('')
  const [error, setError] = useState('')

  async function load() {
    const d = await apiGet<{ items: Job[] }>('/jobs')
    setItems(d.items ?? [])
  }

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    try {
      await apiPost('/jobs', {
        employee_id: employeeId,
        prompt,
        idempotency_key: `admin-${Date.now()}`,
        timeout_sec: 600,
      })
      setPrompt('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    }
  }

  return (
    <section>
      <h1>Jobs</h1>
      <form className="inline-form" onSubmit={onCreate}>
        <input placeholder="Employee ID" value={employeeId} onChange={(e) => setEmployeeId(e.target.value)} required />
        <input placeholder="Prompt" value={prompt} onChange={(e) => setPrompt(e.target.value)} required />
        <button type="submit">创建 Job</button>
      </form>
      {error ? <p className="error">{error}</p> : null}
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Employee</th>
            <th>Status</th>
            <th>Prompt</th>
          </tr>
        </thead>
        <tbody>
          {items.map((j) => (
            <tr key={j.id}>
              <td>
                <Link to={`/jobs/${j.id}`}>{j.id}</Link>
              </td>
              <td>{j.employee_id}</td>
              <td>{j.status}</td>
              <td>{j.prompt}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

export function JobDetailPage() {
  const { id } = useParams()
  const [job, setJob] = useState<Job | null>(null)
  const [events, setEvents] = useState<JobEvent[]>([])

  useEffect(() => {
    if (!id) return
    void apiGet<Job>(`/jobs/${id}`).then(setJob)
    void apiGet<{ items: JobEvent[] }>(`/jobs/${id}/events`).then((d) => setEvents(d.items ?? []))
  }, [id])

  if (!job) return <p className="muted">加载中…</p>
  return (
    <section>
      <h1>Job {job.id}</h1>
      <p className="muted">
        {job.status} · timeout {job.timeout_sec}s · key {job.idempotency_key}
      </p>
      <pre>{job.prompt}</pre>
      <h2>Timeline</h2>
      <ul className="timeline">
        {events.map((e) => (
          <li key={e.id}>
            <strong>{e.event_type}</strong> {JSON.stringify(e.payload)}{' '}
            <span className="muted">{e.created_at}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}
