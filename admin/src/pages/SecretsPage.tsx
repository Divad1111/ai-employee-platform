/**
 * Secrets 管理（M8）：列表掩码，无明文回显。
 */
import { useEffect, useState } from 'react'
import { apiGet, apiPost, apiDelete } from '../api/client'

type SecretMeta = {
  id: string
  name: string
  masked: string
  description?: string
  created_at?: string
}

export function SecretsPage() {
  const [items, setItems] = useState<SecretMeta[]>([])
  const [name, setName] = useState('')
  const [value, setValue] = useState('')
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')

  const reload = () => {
    void apiGet<{ items: SecretMeta[] }>('/secrets')
      .then((d) => setItems(d.items ?? []))
      .catch((e: Error) => setErr(e.message))
  }

  useEffect(() => {
    reload()
  }, [])

  const create = async () => {
    setErr('')
    setMsg('')
    try {
      await apiPost('/secrets', { name, value })
      setName('')
      setValue('')
      setMsg('已创建（明文仅提交一次，列表仅显示掩码）')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const remove = async (id: string) => {
    setErr('')
    try {
      await apiPost('/auth/step-up', { password: prompt('二次认证：输入密码') || '' })
      await apiDelete(`/secrets/${id}`)
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  return (
    <section>
      <h1>Secrets</h1>
      <p className="muted">Employee 仅持有引用；明文不进 Prompt / Job / 普通日志。需 secret.write 权限。</p>
      <div className="row" style={{ gap: 8, marginBottom: 12, flexWrap: 'wrap' }}>
        <input placeholder="名称" value={name} onChange={(e) => setName(e.target.value)} />
        <input
          placeholder="明文（仅提交一次）"
          type="password"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <button type="button" onClick={() => void create()}>
          创建
        </button>
      </div>
      {err ? <p className="error">{err}</p> : null}
      {msg ? <p className="muted">{msg}</p> : null}
      <table className="table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Name</th>
            <th>Value</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map((s) => (
            <tr key={s.id}>
              <td>{s.id}</td>
              <td>{s.name}</td>
              <td>{s.masked || '***'}</td>
              <td>
                <button type="button" onClick={() => void remove(s.id)}>
                  删除
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
