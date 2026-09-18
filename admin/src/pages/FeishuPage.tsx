/**
 * Feishu 集成：配置（需 step-up）+ Employee 绑定列表/创建。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiGet, apiPost, apiPut } from '../api/client'

type FeishuConfig = {
  app_id: string
  app_secret_ref: string
  verification_token: string
  encrypt_key_ref: string
  enabled: boolean
}

type Binding = {
  employee_id: string
  feishu_open_id: string
  feishu_bot_alias: string
  chat_id: string
}

export function FeishuPage() {
  const [cfg, setCfg] = useState<FeishuConfig | null>(null)
  const [bindings, setBindings] = useState<Binding[]>([])
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')

  // 配置表单
  const [appId, setAppId] = useState('')
  const [appSecret, setAppSecret] = useState('')
  const [vt, setVt] = useState('')
  const [encryptKey, setEncryptKey] = useState('')
  const [enabled, setEnabled] = useState(false)
  const [stepPassword, setStepPassword] = useState('')
  const [stepTotp, setStepTotp] = useState('')

  // 绑定表单
  const [empId, setEmpId] = useState('')
  const [openId, setOpenId] = useState('')
  const [alias, setAlias] = useState('')
  const [chatId, setChatId] = useState('')

  async function load() {
    const [c, b] = await Promise.all([
      apiGet<FeishuConfig>('/integrations/feishu/config'),
      apiGet<{ items: Binding[] }>('/integrations/feishu/bindings'),
    ])
    setCfg(c)
    setAppId(c.app_id || '')
    setVt(c.verification_token || '')
    setEnabled(!!c.enabled)
    setBindings(b.items ?? [])
  }

  useEffect(() => {
    void load().catch((e) => setError(e instanceof Error ? e.message : '加载失败'))
  }, [])

  async function saveConfig(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      // PUT 需 step-up：先二次认证
      await apiPost('/auth/step-up', {
        password: stepPassword,
        ...(stepTotp ? { totp: stepTotp } : {}),
      })
      const next = await apiPut<FeishuConfig>('/integrations/feishu/config', {
        app_id: appId,
        app_secret: appSecret || undefined,
        verification_token: vt,
        encrypt_key: encryptKey || undefined,
        enabled,
      })
      setCfg(next)
      setAppSecret('')
      setEncryptKey('')
      setStepPassword('')
      setStepTotp('')
      setMsg('配置已保存（Secret 仅存引用，不回显明文）')
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function createBinding(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      await apiPost('/integrations/feishu/bindings', {
        employee_id: empId,
        feishu_open_id: openId,
        feishu_bot_alias: alias,
        chat_id: chatId,
      })
      setEmpId('')
      setOpenId('')
      setAlias('')
      setChatId('')
      setMsg('绑定已创建/更新')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '绑定失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>Feishu</h1>
          <p className="muted">应用配置与 Employee ↔ 飞书身份绑定。改配置需 step-up。</p>
        </div>
      </header>
      {error ? <p className="error">{error}</p> : null}
      {msg ? <p className="ok-msg">{msg}</p> : null}

      <div className="panel">
        <h2>应用配置</h2>
        {cfg ? (
          <p className="muted">
            Secret 引用：app_secret={cfg.app_secret_ref || '—'} · encrypt_key={cfg.encrypt_key_ref || '—'}
          </p>
        ) : null}
        <form className="stack-form" onSubmit={saveConfig}>
          <label>
            App ID
            <input value={appId} onChange={(e) => setAppId(e.target.value)} />
          </label>
          <label>
            App Secret（留空则保留原引用）
            <input type="password" value={appSecret} onChange={(e) => setAppSecret(e.target.value)} />
          </label>
          <label>
            Verification Token
            <input value={vt} onChange={(e) => setVt(e.target.value)} />
          </label>
          <label>
            Encrypt Key（留空则保留原引用）
            <input type="password" value={encryptKey} onChange={(e) => setEncryptKey(e.target.value)} />
          </label>
          <label className="check-row">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            启用 Feishu 集成
          </label>
          <div className="inline-form">
            <input
              type="password"
              placeholder="step-up 密码"
              value={stepPassword}
              onChange={(e) => setStepPassword(e.target.value)}
              required
            />
            <input
              placeholder="TOTP（若已绑定）"
              value={stepTotp}
              onChange={(e) => setStepTotp(e.target.value)}
            />
            <button type="submit">保存配置</button>
          </div>
        </form>
      </div>

      <div className="panel">
        <h2>Bindings</h2>
        <form className="inline-form" onSubmit={createBinding}>
          <input placeholder="Employee ID" value={empId} onChange={(e) => setEmpId(e.target.value)} required />
          <input placeholder="Feishu Open ID" value={openId} onChange={(e) => setOpenId(e.target.value)} />
          <input placeholder="Bot Alias" value={alias} onChange={(e) => setAlias(e.target.value)} />
          <input placeholder="Chat ID" value={chatId} onChange={(e) => setChatId(e.target.value)} />
          <button type="submit">Upsert 绑定</button>
        </form>
        <table className="table">
          <thead>
            <tr>
              <th>Employee</th>
              <th>Open ID</th>
              <th>Alias</th>
              <th>Chat</th>
            </tr>
          </thead>
          <tbody>
            {bindings.length === 0 ? (
              <tr>
                <td colSpan={4} className="muted">
                  暂无绑定
                </td>
              </tr>
            ) : (
              bindings.map((b) => (
                <tr key={b.employee_id}>
                  <td>{b.employee_id}</td>
                  <td className="mono">{b.feishu_open_id || '—'}</td>
                  <td>{b.feishu_bot_alias || '—'}</td>
                  <td className="mono">{b.chat_id || '—'}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
