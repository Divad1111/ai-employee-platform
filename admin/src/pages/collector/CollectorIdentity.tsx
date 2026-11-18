import { useEffect, useState } from 'react'
import { beginFeishuOAuth, getCollectorProfile, saveCollectorProfile, type CollectorProfile } from '../../api/collector'
import { CollectorListField } from './CollectorListField'
import './collector.css'

const scopeGroups = [
  ['云文档读取', 'docx:document:readonly'], ['云文档编辑', 'docx:document'],
  ['云空间读取', 'drive:drive:readonly'], ['内容搜索', 'search:docs:read'],
  ['知识库', 'wiki:wiki'], ['电子表格', 'sheets:spreadsheet'], ['多维表格', 'bitable:app'],
  ['日历', 'calendar:calendar'], ['任务', 'task:task'], ['消息发送', 'im:message'],
  ['通讯录查询', 'contact:user.id:readonly'], ['自动续期', 'offline_access'],
]
const editableFields = ['display_name', 'aliases', 'timezone', 'jira_url', 'jira_account', 'jira_auth',
  'meego_user_key', 'meego_url', 'meego_plugin_id', 'feishu_app_id', 'scopes', 'notify_enabled', 'notify_chat_id'] as const

export function CollectorIdentity() {
  const [profile, setProfile] = useState<CollectorProfile | null>(null)
  const [secrets, setSecrets] = useState<Record<string, string>>({})
  const [expires, setExpires] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [advanced, setAdvanced] = useState(false)
  useEffect(() => {
    getCollectorProfile().then(setProfile).catch(e => setError(e.message))
    const state = new URLSearchParams(window.location.search).get('feishu')
    if (state === 'authorized') setMessage('飞书个人身份已连接。后续检索与操作将默认使用这份用户授权。')
    if (state === 'cancelled') setMessage('本次授权已取消，可以重新发起。')
  }, [])
  function update<K extends keyof CollectorProfile>(key: K, value: CollectorProfile[K]) {
    setProfile(p => p ? { ...p, [key]: value } : p)
  }
  async function persist(clear: string[] = []) {
    if (!profile) return
    const body: Record<string, unknown> = {}
    for (const key of editableFields) body[key] = profile[key]
    body.credentials = secrets
    body.clear_credentials = clear
    if (secrets.access_token && expires) body.expires_at = new Date(expires).toISOString()
    const saved = await saveCollectorProfile(body)
    setProfile(saved); setSecrets({}); setExpires('')
    return saved
  }
  async function act(action: 'save' | 'authorize' | 'disconnect') {
    setBusy(true); setError(''); setMessage('')
    try {
      await persist(action === 'disconnect' ? ['feishu'] : [])
      if (action === 'authorize') {
        const result = await beginFeishuOAuth()
        window.location.assign(result.authorize_url)
      } else setMessage(action === 'disconnect' ? '已移除平台保存的飞书用户凭证。如需撤销上游授权，请在飞书授权管理中取消。' : '个人接入设置已保存。')
    } catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setBusy(false) }
  }
  const secretInput = (key: string, label: string, configured?: boolean) => (
    <label className="col-field">{label}
      <input type="password" autoComplete="new-password" value={secrets[key] || ''}
        placeholder={configured ? '已配置，留空保留' : '尚未配置'}
        onChange={e => setSecrets(s => ({ ...s, [key]: e.target.value }))} />
    </label>
  )
  return (
    <section className="collector-identity panel" aria-labelledby="collector-identity-title">
      <div className="panel-header">
        <div><h2 id="collector-identity-title">我的工作账号</h2><p>连接个人飞书与 Jira，让采集器以你的身份读取和处理工作内容。</p></div>
        <span className={`col-status ${profile?.authorized && !profile.needs_reauthorization ? 'good' : 'warn'}`}>
          {profile?.authorized && !profile.needs_reauthorization ? '飞书用户已连接' : '待授权'}
        </span>
      </div>
      <div className="col-form-body">
        {error && <div className="col-alert error" role="alert">{error}</div>}
        {message && <div className="col-alert good" role="status">{message}</div>}
        {!profile ? <p>正在读取个人配置…</p> : <>
          <div className="col-identity-banner">
            <div className="col-avatar">{(profile.feishu_name || profile.display_name || '我').slice(0, 1)}</div>
            <div><strong>{profile.feishu_name || '尚未连接飞书个人账号'}</strong>
              <p>{profile.feishu_open_id || '完成授权后，文档、日历和任务默认使用用户身份访问。'}</p>
              {profile.expires_at && <small>访问凭证到期：{new Date(profile.expires_at).toLocaleString()} · {profile.has_refresh_token ? '已配置自动续期' : '到期后需重新授权'}</small>}
            </div>
          </div>
          {profile.capabilities && <div className="col-capabilities">
            <p className="col-muted">以下为飞书返回的授权范围；具体文档、群组和日历仍以飞书资源权限为准。</p>
            <div className="col-scope-grid">{profile.capabilities.map(cap => <div key={cap.name}><strong>{cap.name}</strong> <span className={`col-status ${cap.status === 'granted' ? 'good' : 'warn'}`}>{{ granted: '范围已授权', not_granted: '未返回所需范围', disconnected: '待连接', unknown: '范围待验证' }[cap.status] || cap.status}</span></div>)}</div>
          </div>}
          <div className="col-grid-2">
            <label className="col-field">姓名<input value={profile.display_name || ''} onChange={e => update('display_name', e.target.value)} placeholder="表格里的姓名" /></label>
            <CollectorListField label="别名" value={profile.aliases || []} onChange={values => update('aliases', values)} placeholder="英文名、表格使用的其他名字" />
            <label className="col-field">时区<input value={profile.timezone || 'Asia/Shanghai'} onChange={e => update('timezone', e.target.value)} /></label>
            <label className="col-field">飞书应用 App ID<input value={profile.feishu_app_id || ''} onChange={e => update('feishu_app_id', e.target.value)}
              placeholder={profile.platform_app_available ? '留空使用平台应用进行个人授权' : '填写你自己的飞书应用 ID'} /></label>
            {secretInput('app_secret', '个人应用 App Secret', profile.has_app_secret)}
            <label className="col-field">OAuth 回调地址<input readOnly value={profile.redirect_uri || ''} /><small>在飞书应用的重定向 URL 中登记此地址。</small></label>
          </div>
          <div className="col-divider" />
          <h3>需要使用的飞书能力</h3>
          <p className="col-muted">勾选后保存并授权。应用需要开通对应权限，用户还需在飞书授权页同意。操作工具会先返回预览。</p>
          <div className="col-scope-grid">{scopeGroups.map(([label, scope]) => {
            const scopes = new Set((profile.scopes || '').split(/\s+/).filter(Boolean))
            return <label key={scope} className="col-check">
              <input type="checkbox" checked={scopes.has(scope)} onChange={e => {
                if (e.target.checked) scopes.add(scope); else scopes.delete(scope)
                update('scopes', [...scopes].join(' '))
              }} />{label}
            </label>
          })}</div>
          <div className="col-actions">
            <button type="button" className="btn-primary" disabled={busy || !profile.scopes} onClick={() => void act('authorize')}>{busy ? '正在处理…' : profile.authorized ? '更新飞书授权' : '连接我的飞书账号'}</button>
            {profile.authorized && <button type="button" className="btn-ghost" disabled={busy} onClick={() => {
              if (window.confirm('移除平台保存的个人飞书授权？使用该身份的采集任务将等待重新授权。')) void act('disconnect')
            }}>断开个人授权</button>}
          </div>
          <details open={advanced} onToggle={e => setAdvanced(e.currentTarget.open)}>
            <summary>高级授权设置与已有用户凭证</summary>
            <label className="col-field">权限 Scope（空格分隔）<textarea rows={3} value={profile.scopes || ''} onChange={e => update('scopes', e.target.value)} /></label>
            <p className="col-muted">也可导入已有 user_access_token。服务端会读取用户信息验证身份，不接受应用 Token 代替个人身份。</p>
            <div className="col-grid-2">
              {secretInput('access_token', 'User Access Token')}
              {secretInput('refresh_token', 'Refresh Token')}
              <label className="col-field">Access Token 到期时间<input type="datetime-local" value={expires} onChange={e => setExpires(e.target.value)} /></label>
            </div>
          </details>
          <div className="col-divider" />
          <h3>Jira 与飞书项目</h3>
          <div className="col-grid-2">
            <label className="col-field">Jira 站点<input value={profile.jira_url || ''} onChange={e => update('jira_url', e.target.value)} placeholder="https://jira.example.com" /></label>
            <label className="col-field">Jira 账号<input value={profile.jira_account || ''} onChange={e => update('jira_account', e.target.value)} /></label>
            <label className="col-field">Jira 认证<select value={profile.jira_auth || 'pat'} onChange={e => update('jira_auth', e.target.value)}><option value="pat">个人访问令牌 PAT</option><option value="basic">账号 + API Token</option></select></label>
            {secretInput('jira_token', 'Jira Token', profile.has_jira_token)}
            <label className="col-field">飞书项目用户标识<input value={profile.meego_user_key || ''} onChange={e => update('meego_user_key', e.target.value)} /></label>
            <label className="col-field">飞书项目 Plugin ID<input value={profile.meego_plugin_id || ''} onChange={e => update('meego_plugin_id', e.target.value)} /></label>
            {secretInput('meego_secret', '飞书项目 Plugin Secret（自动获取 Token）', profile.has_meego_secret)}
            {secretInput('meego_token', '飞书项目 Plugin Token', profile.has_meego_token)}
          </div>
          <div className="col-divider" />
          <h3>通知与事件接入</h3>
          <label className="col-check"><input type="checkbox" checked={profile.notify_enabled} onChange={e => update('notify_enabled', e.target.checked)} />允许已启用的通知规则发送提醒</label>
          <div className="col-grid-2">
            <label className="col-field">通知群 Chat ID<input value={profile.notify_chat_id || ''} onChange={e => update('notify_chat_id', e.target.value)} placeholder="oc_…" /></label>
            {secretInput('bot_webhook', '或使用群机器人 Webhook', profile.has_bot_webhook)}
            {secretInput('webhook_secret', '事件接入 HMAC 密钥')}
          </div>
          <div className="col-actions"><button type="button" className="btn-primary" disabled={busy} onClick={() => void act('save')}>{busy ? '保存中…' : '保存接入设置'}</button></div>
        </>}
      </div>
    </section>
  )
}
