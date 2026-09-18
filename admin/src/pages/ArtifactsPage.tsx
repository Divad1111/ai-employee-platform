/**
 * Artifacts 列表与下载（M9）。
 */
import { useEffect, useState } from 'react'
import { apiGet, getToken } from '../api/client'

type Artifact = {
  id: string
  job_id: string
  name: string
  type: string
  size_bytes: number
  sha256: string
  created_at: string
}

export function ArtifactsPage() {
  const [items, setItems] = useState<Artifact[]>([])
  const [err, setErr] = useState('')

  useEffect(() => {
    void apiGet<{ items: Artifact[] }>('/artifacts')
      .then((d) => setItems(d.items ?? []))
      .catch((e: Error) => setErr(e.message))
  }, [])

  const download = (id: string, name: string) => {
    const tok = getToken()
    void fetch(`/api/artifacts/${id}/download`, {
      headers: tok ? { Authorization: `Bearer ${tok}` } : {},
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(String(res.status))
        const blob = await res.blob()
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = name
        a.click()
        URL.revokeObjectURL(url)
      })
      .catch((e: Error) => setErr(e.message))
  }

  return (
    <section>
      <h1>Artifacts</h1>
      <p className="muted">Job 产物；相同 SHA256 去重。下载需鉴权。</p>
      {err ? <p className="error">{err}</p> : null}
      <table className="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Job</th>
            <th>SHA256</th>
            <th>Size</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map((a) => (
            <tr key={a.id}>
              <td>{a.name}</td>
              <td>{a.job_id}</td>
              <td style={{ fontSize: 12 }}>{a.sha256.slice(0, 12)}…</td>
              <td>{a.size_bytes}</td>
              <td>
                <button type="button" onClick={() => download(a.id, a.name)}>
                  下载
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
