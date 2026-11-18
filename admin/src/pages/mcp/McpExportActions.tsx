import { useEffect, useState } from 'react'
import { createExternalMCPToken, exportMCPServers, getExternalMCPPolicy, listMCPExportTokens, revokeOwnExternalMCPToken } from '../../api/mcp'
import { type MCPToken } from '../../api/workflowmcp'
import { IconKey } from '../../components/Icons'
import { usePerm } from '../../stores/permissions'

type Props = {
  disabled: boolean
  onMessage: (message: string) => void
  onError: (message: string) => void
  selfService?: boolean
}

export function McpExportActions({ disabled, onMessage, onError, selfService = false }: Props) {
  const { canAll } = usePerm()
  const [format, setFormat] = useState<'json' | 'codex'>('json')
  const [scope, setScope] = useState<'READ' | 'WRITE'>('READ')
  const [canWrite, setCanWrite] = useState(false)
  const [collectorWrite, setCollectorWrite] = useState(false)
  const [collectorScope, setCollectorScope] = useState<'READ' | 'WRITE'>('READ')
  const [services, setServices] = useState<string[]>(['workflow-mcp', 'personal-work-collector'])
  const [policyLoaded, setPolicyLoaded] = useState(false)
  const [label, setLabel] = useState('')
  const [exporting, setExporting] = useState(false)
  const [showTokens, setShowTokens] = useState(false)
  const [tokens, setTokens] = useState<MCPToken[]>([])
  const [tokensBusy, setTokensBusy] = useState(false)
  const [tokenError, setTokenError] = useState('')
  const canExport = selfService || (canAll('workflow.grant') && canAll('workflow.read'))

  useEffect(() => {
    let active = true
    getExternalMCPPolicy().then(policy => {
      if (active) { setCanWrite(policy.can_write); setCollectorWrite(!!policy.collector_can_write); setPolicyLoaded(true) }
    }).catch(error => { if (active) onError(error instanceof Error ? error.message : String(error)) })
    return () => { active = false }
  }, [])

  async function download() {
    if (exporting) return
    setExporting(true)
    onError('')
    onMessage('')
    try {
      const result = selfService
        ? await createExternalMCPToken(format, 'READ', label, services.map(service => ({ service, scope: service === 'workflow-mcp' ? scope : collectorScope })))
        : await exportMCPServers(format, scope, collectorScope)
      const blob = new Blob([result.content], {
        type: format === 'json' ? 'application/json;charset=utf-8' : 'text/plain;charset=utf-8',
      })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = result.filename
      document.body.appendChild(link)
      link.click()
      link.remove()
      window.setTimeout(() => URL.revokeObjectURL(url), 1000)
      const expiry = result.token?.expires_at
        ? `内置服务授权有效期至 ${new Date(result.token.expires_at).toLocaleDateString()}，可在“外部授权”中吊销。`
        : ''
      const usage = format === 'codex'
        ? '将文件中的 MCP 配置合并到 ~/.codex/config.toml。'
        : '可直接导入 Cursor，或合并到 .cursor/mcp.json。'
      onMessage(`${selfService ? `已创建凭证并下载 ${result.server_count} 个内置 MCP 配置。` : `已导出全部 ${result.server_count} 个 MCP 服务。`}${usage}${expiry}${(result.warnings || []).join('；')}`)
    } catch (error: unknown) {
      onError(error instanceof Error ? error.message : String(error))
    } finally {
      setExporting(false)
    }
  }

  async function openTokens() {
    setShowTokens(true)
    setTokenError('')
    setTokensBusy(true)
    try {
      const [list, policy] = await Promise.all([listMCPExportTokens(), getExternalMCPPolicy()])
      setTokens(list.items || [])
      setCanWrite(policy.can_write)
      setCollectorWrite(!!policy.collector_can_write)
      if (!policy.can_write) setScope('READ')
      if (!policy.collector_can_write) setCollectorScope('READ')
    } catch (error: unknown) {
      setTokenError(error instanceof Error ? error.message : String(error))
    } finally {
      setTokensBusy(false)
    }
  }

  async function revoke(id: string) {
    setTokenError('')
    setTokensBusy(true)
    try {
      await revokeOwnExternalMCPToken(id)
      setTokens(current => current.map(token => token.id === id
        ? { ...token, revoked_at: new Date().toISOString() }
        : token))
    } catch (error: unknown) {
      setTokenError(error instanceof Error ? error.message : String(error))
    } finally {
      setTokensBusy(false)
    }
  }

  return (
    <>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', flexWrap: 'wrap', gap: 8, marginLeft: 'auto' }}>
        {canExport && (
          <>
            {selfService && <input aria-label="外部凭证名称" placeholder="名称，例如我的 Cursor" maxLength={80}
              value={label} disabled={exporting} onChange={event => setLabel(event.target.value)} />}
            {selfService && ['workflow-mcp', 'personal-work-collector'].map(service => <label key={service} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <input type="checkbox" checked={services.includes(service)} disabled={exporting} onChange={e => setServices(current => e.target.checked ? [...current, service] : current.filter(s => s !== service))} />
              {service === 'workflow-mcp' ? '工作流' : '工作采集器'}
            </label>)}
            <select aria-label="MCP 导出格式" value={format} disabled={exporting}
              onChange={event => setFormat(event.target.value as 'json' | 'codex')}>
              <option value="json">Cursor / JSON</option>
              <option value="codex">Codex / TOML</option>
            </select>
            <select aria-label="内置 MCP 外部授权范围" value={scope} disabled={exporting}
              onChange={event => setScope(event.target.value as 'READ' | 'WRITE')}>
              <option value="READ">工作流：只读</option>
              {canWrite && (
                <option value="WRITE">工作流：读写（不含删除）</option>
              )}
            </select>
            <select aria-label="采集器外部授权范围" value={collectorScope} disabled={exporting}
              onChange={event => setCollectorScope(event.target.value as 'READ' | 'WRITE')}>
              <option value="READ">采集器：只读</option>
              {collectorWrite && <option value="WRITE">采集器：读写（不含删除）</option>}
            </select>
            <button type="button" className="btn-primary" onClick={() => void download()}
              disabled={disabled || exporting || !policyLoaded || selfService && !services.length}>
              {exporting ? '正在导出…' : selfService ? '创建凭证并下载配置' : '导出 MCP'}
            </button>
          </>
        )}
        <button type="button" className="btn-ghost" onClick={() => void openTokens()}>
          <IconKey size={15} />外部授权
        </button>
      </div>
      {showTokens && (
        <div className="modal-backdrop">
          <div className="modal-card" role="dialog" aria-modal="true" aria-labelledby="mcp-export-tokens-title"
            style={{ width: 'min(900px, calc(100vw - 32px))' }}>
            <div className="modal-header">
              <h3 id="mcp-export-tokens-title">我的外部 MCP 授权</h3>
              <button type="button" className="modal-close-btn" title="关闭外部授权"
                onClick={() => setShowTokens(false)}>✕</button>
            </div>
            <div className="modal-body">
              <p>每份内置 MCP 凭证有效期 90 天。工作流与采集器分别授权，收回写权限后立即按只读校验。吊销只影响该份凭证。</p>
              <p>当前可创建：{canWrite ? '只读或读写（不含删除）' : '只读'}。只读凭证不会随账号授权自动升级。</p>
              <p className="form-hint">其他 MCP 使用原服务的认证；需要撤销时，请在对应服务中吊销或轮换凭证。</p>
              {tokenError && <p className="error" role="alert">{tokenError}</p>}
              <div className="table-wrapper">
                <table className="table">
                  <thead><tr><th>创建时间</th><th>权限</th><th>到期时间</th><th>最近使用</th><th>状态</th><th>操作</th></tr></thead>
                  <tbody>
                    {tokens.map(token => {
                      const expired = !!token.expires_at && new Date(token.expires_at).getTime() <= Date.now()
                      const revoked = !!token.revoked_at
                      return (
                        <tr key={token.id}>
                          <td>{new Date(token.created_at).toLocaleString()}<div className="form-hint">{token.label} · {token.service === 'personal-work-collector' ? '采集器' : '工作流'}</div></td>
                          <td>{token.scope === 'READ' ? '只读' : token.scope === 'WRITE'
                            ? (token.service === 'personal-work-collector' ? collectorWrite : canWrite) ? '读写（不含删除）' : '只读（写权限已收回）' : '旧版授权，请重新创建'}</td>
                          <td>{token.expires_at ? new Date(token.expires_at).toLocaleDateString() : '—'}</td>
                          <td>{token.last_used_at ? new Date(token.last_used_at).toLocaleString() : '未使用'}</td>
                          <td>{revoked ? '已吊销' : expired ? '已过期' : '有效'}</td>
                          <td><button type="button" className="btn-ghost btn-sm" disabled={tokensBusy || revoked || expired}
                            onClick={() => void revoke(token.id)}>吊销</button></td>
                        </tr>
                      )
                    })}
                    {!tokens.length && <tr><td colSpan={6} className="text-muted">{tokensBusy ? '正在加载…' : '暂无外部授权'}</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
