/**
 * Feishu 飞书集成与协同配置。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiDelete, apiGet, apiPost, apiPut } from '../api/client'
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

type FeishuStatus = {
  configured: boolean
  enabled: boolean
  connected: boolean
  app_id?: string
  bot_name?: string
  bot_open_id?: string
  activate_status?: number
  latency_ms?: number
  error?: string
  ws_state?: string
  ws_error?: string
  last_checked_at?: string
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

  // 状态与测试
  const [status, setStatus] = useState<FeishuStatus | null>(null)
  const [checkingStatus, setCheckingStatus] = useState(false)
  const [testTargetType, setTestTargetType] = useState('open_id')
  const [testTargetId, setTestTargetId] = useState('')
  const [testContent, setTestContent] = useState('【AI Employee】飞书协同通信测试正常！这是一条系统自检验证消息。')
  const [sendingTest, setSendingTest] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; message: string; details?: string } | null>(null)

  // 绑定表单
  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [empId, setEmpId] = useState('')
  const [openId, setOpenId] = useState('')
  const [alias, setAlias] = useState('')
  const [chatId, setChatId] = useState('')
  const [editing, setEditing] = useState(false)

  async function load() {
    const [c, b, empData, st] = await Promise.all([
      apiGet<FeishuConfig>('/integrations/feishu/config'),
      apiGet<{ items: Binding[] }>('/integrations/feishu/bindings'),
      apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
      apiGet<FeishuStatus>('/integrations/feishu/status').catch(() => null),
    ])
    setCfg(c)
    setAppId(c.app_id || '')
    setVt(c.verification_token || '')
    setEnabled(!!c.enabled)
    setBindings(b.items ?? [])
    setEmployees(empData.items ?? [])
    setStatus(st)
  }

  const empMap = Object.fromEntries(employees.map((e) => [e.id, e.name]))

  useEffect(() => {
    void load().catch((e) => setError(e instanceof Error ? e.message : '加载飞书配置失败'))
  }, [])

  function resetBindingForm() {
    setEmpId('')
    setOpenId('')
    setAlias('')
    setChatId('')
    setEditing(false)
  }

  function startEdit(b: Binding) {
    setEmpId(b.employee_id)
    setAlias(b.feishu_bot_alias || '')
    setOpenId(b.feishu_open_id || '')
    setChatId(b.chat_id || '')
    setEditing(true)
    setError('')
    setMsg('')
  }

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
      await load()
      void checkConnection(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存配置失败（请检查二次认证密码或 TOTP）')
    }
  }

  async function checkConnection(refresh = true) {
    setCheckingStatus(true)
    setTestResult(null)
    try {
      const s = await apiGet<FeishuStatus>(`/integrations/feishu/status${refresh ? '?refresh=true' : ''}`)
      setStatus(s)
      if (s.connected) {
        setTestResult({
          success: true,
          message: `飞书开放平台握手成功！机器人「${s.bot_name || '已就绪'}」在线，网络时延 ${s.latency_ms ?? 0}ms`,
          details: `App ID: ${s.app_id} · Bot OpenID: ${s.bot_open_id || '未知'} · 激活状态: ${s.activate_status === 2 ? '已激活(上线)' : '未上线或测试中'}`,
        })
      } else {
        setTestResult({
          success: false,
          message: `飞书连通性自检未通过: ${s.error || '通信异常'}`,
        })
      }
    } catch (err) {
      setTestResult({
        success: false,
        message: err instanceof Error ? err.message : '检测飞书连通性失败',
      })
    } finally {
      setCheckingStatus(false)
    }
  }

  async function onSendTestMessage(e: FormEvent) {
    e.preventDefault()
    if (!testTargetId.trim()) {
      setError('请输入接收对象的飞书用户 OpenID 或群聊 Chat ID')
      return
    }
    setSendingTest(true)
    setTestResult(null)
    setError('')
    try {
      const res = await apiPost<{ message_id: string; chat_id: string; sent_at: string }>(
        '/integrations/feishu/test-message',
        {
          receive_id_type: testTargetType,
          receive_id: testTargetId.trim(),
          content: testContent.trim(),
        }
      )
      setTestResult({
        success: true,
        message: `测试消息已成功送达飞书！消息 ID: ${res.message_id}`,
        details: res.chat_id ? `目标会话 Chat ID: ${res.chat_id}` : undefined,
      })
    } catch (err) {
      setTestResult({
        success: false,
        message: `发送测试消息失败: ${err instanceof Error ? err.message : '网络或权限错误'}`,
        details: '排查建议：请在飞书开放平台确认：1. 应用已开通「以应用身份发消息 (im:message:send_as_bot)」；2. 机器人已拉入目标群聊或用户在应用可用范围内；3. 应用已发布上线版本。',
      })
    } finally {
      setSendingTest(false)
    }
  }

  async function addBinding(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      const body = {
        employee_id: empId,
        feishu_open_id: openId,
        feishu_bot_alias: alias,
        chat_id: chatId,
      }
      if (editing) {
        await apiPut('/integrations/feishu/bindings', body)
        setMsg('飞书别名绑定已更新')
      } else {
        await apiPost('/integrations/feishu/bindings', body)
        setMsg('飞书协同映射绑定成功')
      }
      resetBindingForm()
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : editing ? '更新绑定失败' : '添加绑定失败')
    }
  }

  async function removeBinding(b: Binding) {
    const name = empMap[b.employee_id] || b.employee_id
    if (!confirm(`确定删除员工「${name}」的别名绑定 @${b.feishu_bot_alias} 吗？`)) return
    setError('')
    setMsg('')
    try {
      const q = new URLSearchParams({
        employee_id: b.employee_id,
        feishu_bot_alias: b.feishu_bot_alias,
      })
      await apiDelete(`/integrations/feishu/bindings?${q.toString()}`)
      setMsg('绑定已删除')
      if (editing && empId === b.employee_id) resetBindingForm()
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除绑定失败')
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
              步骤 3. 事件接收模式 (官方长连接 vs Webhook)
            </div>
            <p style={{ margin: 0, color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              • <strong>🔥 推荐：官方 WebSocket 长连接（免公网IP）</strong>：平台已深度集成！在下方配置 App ID 与 Secret 并启用，系统自动直连飞书开放平台，<strong>无需公网 IP、无需域名、无需配置请求网址</strong>，群内 @机器人 本地即刻秒级响应！<br />
              • <strong>传统 Webhook 模式（可选）</strong>：如部署在云端公网服务器，可进入「事件与回调」配置请求网址为 <code>{window.location.origin}/api/integrations/feishu/events</code>，添加 <code>接收消息 (im.message.receive_v1)</code>。
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

          <div style={{ margin: '0.85rem 0' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem' }}>
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
                启用飞书开放平台长连接网关 (WebSocket) 与智能事件协同
              </label>
            </div>
            <p style={{ margin: '0.3rem 0 0 1.85rem', fontSize: '0.82rem', color: 'var(--text-muted)' }}>
              ⚡ 官方长连接网关模式：本地系统主动向飞书建立出站长连接，彻底摆脱公网 IP 与内网穿透工具依赖，群聊中 @机器人 即刻秒级触发任务！
            </p>
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

        {/* 飞书通信测试与连通性自检 */}
        <div style={{ marginTop: '1.5rem', borderTop: '1px solid var(--border-subtle)', paddingTop: '1.25rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', flexWrap: 'wrap', gap: '0.75rem' }}>
            <div>
              <h3 style={{ margin: 0, fontSize: '1rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <span>🛰️</span>
                <span>飞书开放平台通信测试与状态自检</span>
              </h3>
              <p style={{ margin: '0.2rem 0 0', fontSize: '0.84rem', color: 'var(--text-secondary)' }}>
                实时验证平台与 open.feishu.cn 的凭据鉴权、机器人握手状态与消息送达通信
              </p>
            </div>
            <button
              type="button"
              className="btn-ghost btn-sm"
              onClick={() => void checkConnection(true)}
              disabled={checkingStatus}
              style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}
            >
              <span>{checkingStatus ? '正在探测...' : '🔄 连通性自检'}</span>
            </button>
          </div>

          {/* 实时连接状态指示卡片 */}
          <div style={{ background: '#f8fafc', padding: '0.85rem 1.1rem', borderRadius: '8px', border: '1px solid var(--border-subtle)', marginBottom: '1rem', display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: '0.75rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
              <span style={{ fontSize: '0.86rem', fontWeight: 600, color: 'var(--text-secondary)' }}>当前连接状态:</span>
              {status === null ? (
                <span className="status-pill status-muted">探测中...</span>
              ) : !status.configured ? (
                <span className="status-pill status-neutral">尚未配置 App 凭证</span>
              ) : !status.enabled ? (
                <span className="status-pill status-muted">飞书协同已停用</span>
              ) : status.connected ? (
                <>
                  <span className="status-pill status-success">
                    <span className="status-dot" />
                    飞书在线 · {status.bot_name || '机器人通信正常'}
                  </span>
                  <span
                    className={`status-pill ${status.ws_state === 'CONNECTED' ? 'status-success' : status.ws_state === 'CONNECTING' ? 'status-warning' : 'status-neutral'}`}
                  >
                    <span className="status-dot" />
                    长连接网关: {status.ws_state === 'CONNECTED' ? '已就绪 (免公网IP)' : status.ws_state === 'CONNECTING' ? '连接中...' : '未建立'}
                  </span>
                </>
              ) : (
                <span className="status-pill status-danger">
                  <span className="status-dot" />
                  连接异常: {status.error || '握手失败'}
                </span>
              )}
            </div>
            {status?.connected && (
              <div style={{ display: 'flex', gap: '1.2rem', fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                <span>网络时延: <strong style={{ color: 'var(--brand-700)' }}>{status.latency_ms ?? 0} ms</strong></span>
                {status.bot_open_id && (
                  <span>Bot OpenID: <strong style={{ color: 'var(--text-primary)' }}>{status.bot_open_id}</strong></span>
                )}
              </div>
            )}
          </div>

          {/* 发送测试消息表单 */}
          <div style={{ background: '#ffffff', padding: '1rem', borderRadius: '8px', border: '1px solid var(--border-subtle)' }}>
            <div style={{ fontWeight: 600, fontSize: '0.9rem', marginBottom: '0.6rem', color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
              <span>💬</span>
              <span>发送测试消息 (验证飞书消息实际接收)</span>
            </div>
            <p style={{ margin: '0 0 0.75rem', fontSize: '0.82rem', color: 'var(--text-secondary)' }}>
              向指定飞书用户（OpenID: <code>ou_xxx</code>）或群聊会话（Chat ID: <code>oc_xxx</code>）投递一条测试消息，检验机器人发信权限
            </p>
            <form onSubmit={onSendTestMessage}>
              <div style={{ display: 'grid', gridTemplateColumns: '150px 1fr auto', gap: '0.75rem', alignItems: 'center' }}>
                <select
                  value={testTargetType}
                  onChange={(e) => setTestTargetType(e.target.value)}
                  style={{ padding: '0.5rem 0.6rem' }}
                >
                  <option value="open_id">用户 OpenID</option>
                  <option value="chat_id">群聊 Chat ID</option>
                </select>
                <input
                  list="feishu-test-targets"
                  placeholder={testTargetType === 'open_id' ? '输入接收者飞书用户 OpenID (例: ou_a1b2c3d4)' : '输入接收群聊 Chat ID (例: oc_a1b2c3d4)'}
                  value={testTargetId}
                  onChange={(e) => {
                    const val = e.target.value
                    setTestTargetId(val)
                    if (val.startsWith('ou_')) setTestTargetType('open_id')
                    if (val.startsWith('oc_')) setTestTargetType('chat_id')
                  }}
                  required
                />
                <button type="submit" disabled={sendingTest} style={{ minWidth: '130px' }}>
                  {sendingTest ? '正在发送...' : '📨 发送测试消息'}
                </button>
              </div>
              <div style={{ marginTop: '0.6rem' }}>
                <input
                  placeholder="测试消息文本内容"
                  value={testContent}
                  onChange={(e) => setTestContent(e.target.value)}
                  style={{ width: '100%', fontSize: '0.85rem' }}
                />
              </div>
              <datalist id="feishu-test-targets">
                {bindings.map((b) => (
                  <option key={b.employee_id} value={testTargetType === 'open_id' ? (b.feishu_open_id || '') : (b.chat_id || '')}>
                    {empMap[b.employee_id] || b.employee_id} (别名 @{b.feishu_bot_alias})
                  </option>
                ))}
              </datalist>
            </form>

            {/* 快速获取 OpenID / Chat ID 帮助折叠卡片 */}
            <details style={{ marginTop: '0.85rem', fontSize: '0.82rem', color: 'var(--text-secondary)', background: '#f8fafc', padding: '0.6rem 0.85rem', borderRadius: '6px', border: '1px solid var(--border-subtle)' }}>
              <summary style={{ cursor: 'pointer', fontWeight: 600, color: 'var(--brand-700)' }}>
                💡 如何快速获取飞书 OpenID (ou_xxx) 与群聊 Chat ID (oc_xxx)？
              </summary>
              <div style={{ marginTop: '0.6rem', lineHeight: 1.6 }}>
                <div style={{ marginBottom: '0.5rem' }}>
                  <strong style={{ color: 'var(--text-primary)' }}>1. 获取用户 OpenID (<code>ou_xxx</code>)：</strong>
                  <ul style={{ margin: '0.2rem 0 0 1.2rem', padding: 0 }}>
                    <li>
                      <strong>API 调试台（推荐）：</strong>访问 <a href="https://open.feishu.cn/api-explorer" target="_blank" rel="noreferrer" style={{ color: 'var(--brand-600)' }}>飞书开放平台 API 调试台</a>，切换到自建应用，右侧面板「当前调试用户」直接展示你的 <code>Open ID</code>，点击一键复制。
                    </li>
                    <li>
                      <strong>私聊机器人：</strong>在飞书给机器人发任意一条私聊消息，在平台控制台入站日志中会直接打印发信人的 <code>ou_xxx</code>。
                    </li>
                  </ul>
                </div>
                <div>
                  <strong style={{ color: 'var(--text-primary)' }}>2. 获取群聊 Chat ID (<code>oc_xxx</code>)：</strong>
                  <p style={{ margin: '0.2rem 0', color: 'var(--danger-700)', fontWeight: 500 }}>
                    ⚠️ 注意：测试前必须先把机器人拉入该群聊，否则机器人无发信权限！
                  </p>
                  <ul style={{ margin: '0.2rem 0 0 1.2rem', padding: 0 }}>
                    <li>
                      <strong>网页版飞书 URL（最快）：</strong>用浏览器打开网页版飞书进入群聊，地址栏 URL 中直接包含 <code>chatId=oc_xxxxxx</code>。
                    </li>
                    <li>
                      <strong>群内 @机器人：</strong>把机器人拉进群后，在群里发一条 <code>@机器人 测试</code>，系统入站日志中会直接捕获该群的 <code>Chat ID</code>。
                    </li>
                    <li>
                      <strong>API 调试台：</strong>在 API 调试台中调用 <code>获取机器人所在的群列表 (GET /open-apis/im/v1/chats)</code>，返回值即包含所有群的 <code>chat_id</code>。
                    </li>
                  </ul>
                </div>
              </div>
            </details>
          </div>

          {/* 测试反馈消息框 */}
          {testResult && (
            <div
              style={{
                marginTop: '0.85rem',
                padding: '0.85rem 1rem',
                borderRadius: '6px',
                fontSize: '0.86rem',
                lineHeight: 1.5,
                background: testResult.success ? '#f0fdf4' : '#fef2f2',
                border: `1px solid ${testResult.success ? '#bbf7d0' : '#fecaca'}`,
                color: testResult.success ? '#166534' : '#991b1b',
              }}
            >
              <div style={{ fontWeight: 600 }}>{testResult.message}</div>
              {testResult.details && (
                <div style={{ marginTop: '0.25rem', fontSize: '0.82rem', opacity: 0.9 }}>
                  {testResult.details}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>数字员工与飞书机器人别名绑定</h2>
            <p>单机器人模式下：每位员工绑定一个呼叫别名（例: @小智）；支持新增、修改与删除</p>
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
            disabled={editing}
          />
          <datalist id="feishu-emp-options">
            {employees.map((e) => (
              <option key={e.id} value={e.id}>
                {e.name} ({e.id})
              </option>
            ))}
          </datalist>
          <input placeholder="机器人别名 (例: 小智 / dev)" value={alias} onChange={(e) => setAlias(e.target.value)} required />
          <input placeholder="飞书 OpenID (可选)" value={openId} onChange={(e) => setOpenId(e.target.value)} />
          <input placeholder="默认群聊 Chat ID (可选)" value={chatId} onChange={(e) => setChatId(e.target.value)} />
          <button type="submit">
            <IconPlus size={15} />
            <span>{editing ? '保存修改' : '添加绑定映射'}</span>
          </button>
          {editing ? (
            <button type="button" className="btn-ghost" onClick={resetBindingForm}>
              取消编辑
            </button>
          ) : null}
        </form>

        <div className="table-wrapper" style={{ marginTop: '1rem' }}>
          <table className="table">
            <thead>
              <tr>
                <th>绑定员工 (Employee)</th>
                <th>飞书机器人呼叫别名</th>
                <th>飞书用户 OpenID</th>
                <th>限定群聊 Chat ID</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {bindings.map((b, i) => (
                <tr key={`${b.employee_id}-${b.feishu_bot_alias}-${i}`}>
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
                  <td>
                    <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
                      <button type="button" className="btn-ghost btn-sm" onClick={() => startEdit(b)}>
                        修改
                      </button>
                      <button
                        type="button"
                        className="btn-ghost btn-sm"
                        style={{ color: '#dc2626' }}
                        onClick={() => void removeBinding(b)}
                      >
                        删除
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {bindings.length === 0 ? (
                <tr>
                  <td colSpan={5} className="empty-tip">暂无飞书员工映射关系，请在上方添加</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
