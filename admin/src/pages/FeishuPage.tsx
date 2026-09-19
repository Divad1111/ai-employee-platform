/**
 * Feishu 飞书集成与协同配置。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiGet, apiPost, apiPut } from '../api/client'
import { IconPlus } from '../components/Icons'
import { EntityName } from '../components/EntityName'

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
  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [empId, setEmpId] = useState('')
  const [openId, setOpenId] = useState('')
  const [alias, setAlias] = useState('')
  const [chatId, setChatId] = useState('')

  async function load() {
    const [c, b, empData] = await Promise.all([
      apiGet<FeishuConfig>('/integrations/feishu/config'),
      apiGet<{ items: Binding[] }>('/integrations/feishu/bindings'),
      apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
    ])
    setCfg(c)
    setAppId(c.app_id || '')
    setVt(c.verification_token || '')
    setEnabled(!!c.enabled)
    setBindings(b.items ?? [])
    setEmployees(empData.items ?? [])
  }

  const empMap = Object.fromEntries(employees.map((e) => [e.id, e.name]))

  useEffect(() => {
    void load().catch((e) => setError(e instanceof Error ? e.message : '加载飞书配置失败'))
  }, [])

  async function saveConfig(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      // 敏感配置二次身份确认
      await apiPost('/auth/step-up', {
        password: stepPassword,
        ...(stepTotp ? { totp: stepTotp } : {}),
      })
      const next = await apiPut<FeishuConfig>('/integrations/feishu/config', {
        app_id: appId,
        ...(appSecret ? { app_secret: appSecret } : {}),
        verification_token: vt,
        ...(encryptKey ? { encrypt_key: encryptKey } : {}),
        enabled,
      })
      setCfg(next)
      setAppSecret('')
      setEncryptKey('')
      setStepPassword('')
      setStepTotp('')
      setMsg('飞书应用集成配置保存成功')
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存配置失败（请检查二次认证密码或 TOTP）')
    }
  }

  async function addBinding(e: FormEvent) {
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
      setMsg('飞书协同映射绑定成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加绑定失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>飞书企业协同集成 (Feishu Integration)</h1>
          <p>对接飞书开放平台机器人与群聊事件 · 支持以自然语言 @机器人 触发任务派发</p>
        </div>
      </header>

      {error ? <div className="error">{error}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel" style={{ borderLeft: '4px solid var(--brand-500)', background: '#f8fafc' }}>
        <div className="panel-header" style={{ marginBottom: '0.8rem' }}>
          <div>
            <h3 style={{ margin: 0, fontSize: '1rem', color: 'var(--brand-700)', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <span>📖</span>
              <span>飞书开放平台参数获取与接入配置全流程指引</span>
            </h3>
            <p style={{ marginTop: '0.2rem' }}>请登录飞书开放平台 (open.feishu.cn) 开发者后台，按以下 5 个步骤获取各字段并完成联动配置</p>
          </div>
          <a
            href="https://open.feishu.cn/app"
            target="_blank"
            rel="noreferrer"
            className="btn-ghost btn-sm"
            style={{ textDecoration: 'none', display: 'inline-flex', alignItems: 'center', gap: '0.3rem' }}
          >
            <span>前往飞书开发者后台 ↗</span>
          </a>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: '0.85rem', fontSize: '0.84rem' }}>
          <div style={{ background: '#ffffff', padding: '0.85rem 1rem', borderRadius: '6px', border: '1px solid #e2e8f0' }}>
            <div style={{ fontWeight: 700, color: 'var(--brand-700)', marginBottom: '0.35rem' }}>
              步骤 1. 创建企业自建应用
            </div>
            <p style={{ margin: 0, color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              访问开放平台开发者后台 → 点击「创建企业自建应用」，填写应用名称（如“AI数字员工协同”）并上传机器人头像图标。
            </p>
          </div>

          <div style={{ background: '#ffffff', padding: '0.85rem 1rem', borderRadius: '6px', border: '1px solid #e2e8f0' }}>
            <div style={{ fontWeight: 700, color: 'var(--brand-700)', marginBottom: '0.35rem' }}>
              步骤 2. 获取 App ID 与 Secret
            </div>
            <p style={{ margin: 0, color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              进入应用详情页 → 左侧「凭证与基础信息」：复制 <code>App ID</code>（格式如 <code>cli_a...</code>）；点击生成并复制 <code>App Secret</code> 填入下方。
            </p>
          </div>

          <div style={{ background: '#ffffff', padding: '0.85rem 1rem', borderRadius: '6px', border: '1px solid #e2e8f0' }}>
            <div style={{ fontWeight: 700, color: 'var(--brand-700)', marginBottom: '0.35rem' }}>
              步骤 3. 配置事件与回调 URL
            </div>
            <p style={{ margin: 0, color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              进入左侧「事件与回调」：<br />
              • <strong>请求网址 (Request URL)</strong>：配置为 <code>{window.location.origin}/api/integrations/feishu/events</code><br />
              • 复制页面显示的 <code>Verification Token</code> 与 <code>Encrypt Key</code> 填入下方；<br />
              • 点击「添加事件」，勾选 <code>接收消息 (im.message.receive_v1)</code>。
            </p>
          </div>

          <div style={{ background: '#ffffff', padding: '0.85rem 1rem', borderRadius: '6px', border: '1px solid #e2e8f0' }}>
            <div style={{ fontWeight: 700, color: 'var(--brand-700)', marginBottom: '0.35rem' }}>
              步骤 4. 开通机器人能力与权限
            </div>
            <p style={{ margin: 0, color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              • 左侧「添加应用能力」：启用<strong>「机器人」</strong>能力；<br />
              • 左侧「权限管理」：开通 <code>获取与发送消息 (im:message)</code> 与 <code>以应用身份发消息 (im:message:send_as_bot)</code>。
            </p>
          </div>

          <div style={{ background: '#ffffff', padding: '0.85rem 1rem', borderRadius: '6px', border: '1px solid #e2e8f0', gridColumn: '1 / -1' }}>
            <div style={{ fontWeight: 700, color: 'var(--brand-700)', marginBottom: '0.35rem' }}>
              步骤 5. 发布应用版本并在群内 @ 机器人使用
            </div>
            <p style={{ margin: 0, color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              左侧「版本管理与发布」→「创建版本」并申请发布上线。审批生效后，将该机器人拉入飞书群聊，即可通过 <code>@机器人 任务内容</code> 协同对话！在下方配置「员工与机器人别名绑定」后，更支持按别名精准呼叫专属数字员工。
            </p>
          </div>
        </div>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>飞书开放平台凭证设置</h2>
            <p>包含应用 App ID、事件校验 Token 与安全机密引用（修改需管理员二次鉴权认证）</p>
          </div>
        </div>

        <form className="stack-form" onSubmit={saveConfig}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: '1rem' }}>
            <label>
              飞书应用 App ID
              <input value={appId} onChange={(e) => setAppId(e.target.value)} placeholder="cli_a1b2c3d4..." required />
            </label>
            <label>
              应用密钥 App Secret (选填)
              <input
                type="password"
                value={appSecret}
                onChange={(e) => setAppSecret(e.target.value)}
                placeholder={cfg?.app_secret_ref ? `已配置: ${cfg.app_secret_ref}` : '首次配置请输入 Secret'}
              />
            </label>
            <label>
              事件校验 Verification Token
              <input value={vt} onChange={(e) => setVt(e.target.value)} placeholder="开放平台事件校验 Token" />
            </label>
            <label>
              消息加密 Encrypt Key (选填)
              <input
                type="password"
                value={encryptKey}
                onChange={(e) => setEncryptKey(e.target.value)}
                placeholder={cfg?.encrypt_key_ref ? `已配置: ${cfg.encrypt_key_ref}` : '留空表示不开启加密'}
              />
            </label>
          </div>

          <div style={{ margin: '0.85rem 0', display: 'flex', alignItems: 'center', gap: '0.65rem' }}>
            <input
              id="feishu-enabled-check"
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              style={{ width: '1.2rem', height: '1.2rem', cursor: 'pointer', accentColor: 'var(--brand-600)', margin: 0, flexShrink: 0 }}
            />
            <label
              htmlFor="feishu-enabled-check"
              style={{ margin: 0, cursor: 'pointer', fontWeight: 600, fontSize: '0.9rem', color: 'var(--text-primary)', display: 'inline' }}
            >
              启用飞书事件接收与智能回执协同
            </label>
          </div>

          <div style={{ background: '#f8fafc', padding: '1rem', borderRadius: '8px', border: '1px solid var(--border-subtle)', marginTop: '0.5rem' }}>
            <h3 style={{ margin: '0 0 0.5rem', fontSize: '0.86rem', color: 'var(--text-secondary)' }}>安全二次鉴权确认</h3>
            <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
              <input
                type="password"
                placeholder="当前管理员登录密码"
                value={stepPassword}
                onChange={(e) => setStepPassword(e.target.value)}
                style={{ width: '220px' }}
                required
              />
              <input
                placeholder="TOTP 验证码 (若未开启可留空)"
                value={stepTotp}
                onChange={(e) => setStepTotp(e.target.value)}
                style={{ width: '200px' }}
              />
              <button type="submit">保存飞书配置</button>
            </div>
          </div>
        </form>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>数字员工与飞书机器人别名绑定</h2>
            <p>在飞书群中 @别名 (例: @小智) 可精准下发对应数字员工</p>
          </div>
        </div>

        <form className="inline-form" onSubmit={addBinding}>
          <input
            list="feishu-emp-options"
            placeholder="选择员工或输入 ID (例: emp-1)"
            value={empId}
            onChange={(e) => setEmpId(e.target.value)}
            required
            style={{ width: '240px' }}
          />
          <datalist id="feishu-emp-options">
            {employees.map((e) => (
              <option key={e.id} value={e.id}>
                {e.name} ({e.id})
              </option>
            ))}
          </datalist>
          <input placeholder="机器人别名 (例: dev / test)" value={alias} onChange={(e) => setAlias(e.target.value)} required />
          <input placeholder="飞书 OpenID (可选)" value={openId} onChange={(e) => setOpenId(e.target.value)} />
          <input placeholder="默认群聊 Chat ID (可选)" value={chatId} onChange={(e) => setChatId(e.target.value)} />
          <button type="submit">
            <IconPlus size={15} />
            <span>添加绑定映射</span>
          </button>
        </form>

        <div className="table-wrapper" style={{ marginTop: '1rem' }}>
          <table className="table">
            <thead>
              <tr>
                <th>绑定员工 (Employee)</th>
                <th>飞书机器人呼叫别名</th>
                <th>飞书用户 OpenID</th>
                <th>限定群聊 Chat ID</th>
              </tr>
            </thead>
            <tbody>
              {bindings.map((b, i) => (
                <tr key={`${b.employee_id}-${i}`}>
                  <td>
                    <EntityName
                      name={empMap[b.employee_id] || b.employee_id}
                      id={b.employee_id}
                      to={`/employees/${b.employee_id}`}
                    />
                  </td>
                  <td>
                    <span className="badge badge-ok">@{b.feishu_bot_alias}</span>
                  </td>
                  <td className="mono">{b.feishu_open_id || '—'}</td>
                  <td className="mono">{b.chat_id || '—'}</td>
                </tr>
              ))}
              {bindings.length === 0 ? (
                <tr>
                  <td colSpan={4} className="empty-tip">暂无飞书员工映射关系，请在上方添加</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
