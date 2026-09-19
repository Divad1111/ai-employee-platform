import { useEffect, useState } from 'react'
import { apiGet, apiPost, apiDelete } from '../api/client'
import { IconKey, IconPlus, IconRefresh } from '../components/Icons'
import { EntityName } from '../components/EntityName'

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
  const [loading, setLoading] = useState(false)

  const reload = () => {
    setLoading(true)
    apiGet<{ items: SecretMeta[] }>('/secrets')
      .then((d) => setItems(d.items ?? []))
      .catch((e: Error) => setErr(e.message))
      .finally(() => setLoading(false))
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
      setMsg('机密凭证已加密存入保管箱（明文仅提交一次，列表严禁明文回显）')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const remove = async (id: string) => {
    setErr('')
    setMsg('')
    try {
      const pwd = prompt('删除敏感凭证二次安全校验：请输入管理员密码') || ''
      if (!pwd) return
      await apiPost('/auth/step-up', { password: pwd })
      await apiDelete(`/secrets/${id}`)
      setMsg('机密凭证已安全销毁')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>机密凭证保管箱 (Secret Vault)</h1>
          <p>存储 API Key、Token 与第三方凭证 · 采用引用绑定，明文严格不出管控面</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => reload()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新保管箱</span>
        </button>
      </header>

      {err ? <div className="error">{err}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>录入新加密凭证</h2>
            <p>凭证保存后将自动脱敏掩码，任何终端均无法通过 API 获取明文</p>
          </div>
        </div>
        <form
          className="inline-form"
          onSubmit={(e) => {
            e.preventDefault()
            void create()
          }}
        >
          <input
            placeholder="凭证键名 (例: FEISHU_APP_SECRET)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            style={{ width: '260px' }}
            required
          />
          <input
            type="password"
            placeholder="机密明文内容"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            style={{ width: '320px' }}
            required
          />
          <button type="submit">
            <IconPlus size={15} />
            <span>安全加密存入</span>
          </button>
        </form>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>当前托管凭证列表</h2>
            <p>共保全 {items.length} 组机密凭证</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>凭证引用名称 (Name)</th>
                <th>掩码显示内容 (Masked)</th>
                <th>录入时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id}>
                  <td>
                    <EntityName
                      name={s.name}
                      id={s.id}
                      icon={<IconKey size={15} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td>
                    <span className="mono" style={{ background: '#fef3c7', color: '#92400e' }}>
                      {s.masked}
                    </span>
                  </td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {s.created_at ? new Date(s.created_at).toLocaleString() : '—'}
                  </td>
                  <td>
                    <button type="button" className="btn-danger btn-sm" onClick={() => void remove(s.id)}>
                      销毁凭证
                    </button>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={4} className="empty-tip">保管箱内暂无凭证记录</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
