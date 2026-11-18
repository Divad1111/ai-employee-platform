import { useEffect, useState } from 'react'
import { getExternalMCPPolicy, setExternalMCPPolicy } from '../../api/mcp'
import { McpExportActions } from './McpExportActions'

export function McpUserAccess({ userId, isSelf, canManage }: {
  userId: string
  isSelf: boolean
  canManage: boolean
}) {
  const [canWrite, setCanWrite] = useState<boolean | null>(null)
  const [collectorWrite, setCollectorWrite] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    let active = true
    setCanWrite(null)
    setError('')
    setMessage('')
    getExternalMCPPolicy(userId).then(policy => {
      if (active) { setCanWrite(policy.can_write); setCollectorWrite(!!policy.collector_can_write) }
    }).catch(e => { if (active) setError(e instanceof Error ? e.message : String(e)) })
    return () => { active = false }
  }, [userId])

  async function toggleWrite(service = 'workflow-mcp') {
    if (canWrite === null || busy) return
    setBusy(true)
    setError('')
    try {
      const policy = await setExternalMCPPolicy(userId, service === 'workflow-mcp' ? !canWrite : !collectorWrite, service)
      setCanWrite(policy.can_write)
      setCollectorWrite(!!policy.collector_can_write)
      setRevision(value => value + 1)
      setMessage('外部 MCP 授权已更新；后续调用按当前策略校验。')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally { setBusy(false) }
  }

  if (!isSelf && !canManage) return null
  return (
    <section className="panel" style={{ marginBottom: '1rem' }} aria-labelledby="external-mcp-title">
      <div className="panel-header">
        <div>
          <h2 id="external-mcp-title">外部 MCP 授权</h2>
          <p>统一管理工作流与工作采集器授权。默认只读；两个服务各自签发凭证，读写权限不包含删除。</p>
        </div>
      </div>
      <div style={{ padding: '0 1.1rem 1.1rem' }}>
        <p>工作流 MCP：{canWrite === null ? '加载中…' : canWrite ? '可创建读写凭证' : '只读'} · 工作采集器：{collectorWrite ? '可创建读写凭证' : '只读'}</p>
        {canManage && <button type="button" className="btn-ghost" disabled={busy || canWrite === null}
          onClick={() => void toggleWrite()}>
          {busy ? '保存中…' : canWrite ? '收回工作流写权限' : '授予工作流写权限'}
        </button>}
        {canManage && <button type="button" className="btn-ghost" disabled={busy || canWrite === null} onClick={() => void toggleWrite('personal-work-collector')}>
          {collectorWrite ? '收回采集器写权限' : '授予采集器写权限'}
        </button>}
        {isSelf && <>
          <p className="form-hint">为每个客户端创建独立凭证，有效期 90 天。配置下载后即可交给外部客户端，凭证明文仅在创建时返回。第三方服务的权限仍由对应服务管理。</p>
          <McpExportActions key={`${userId}-${revision}`} selfService disabled={busy}
            onMessage={setMessage} onError={setError} />
        </>}
        {error && <p className="error" role="alert">{error}</p>}
        {message && <p className="ok-msg" role="status">{message}</p>}
      </div>
    </section>
  )
}
