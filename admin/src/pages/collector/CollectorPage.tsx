import { useEffect, useMemo, useRef, useState } from 'react'
import { apiGet } from '../../api/client'
import { collectorItems, collectorRecords, deleteCollectorRecord, importCollector, previewRule, retryDelivery, saveRule, scanTarget,
  type CollectorRule, type CollectorRun, type CollectorTarget, type Delivery, type WorkEvent, type WorkItem } from '../../api/collector'
import { usePerm } from '../../stores/permissions'
import { CollectorIdentity } from './CollectorIdentity'
import { CollectorTargetEditor, freshTarget, sourceLabels } from './CollectorTargetEditor'
import './collector.css'

const tabs = ['概览', '扫描目标', '规则与通知', '工作事项', '运行记录', '接入与身份']
const date = (v?: string) => v && !v.startsWith('0001') ? new Date(v).toLocaleString() : '—'
const safeLink = (v: string) => /^https?:\/\//i.test(v) ? v : undefined
const statusLabel: Record<string, string> = { queued: '排队中', success: '成功', running: '运行中', failed: '失败', pending: '等待发送', retry: '等待重试', sent: '已发送', cancelled: '已取消' }
const emptyRule = (): CollectorRule => ({ id: '', name: '', enabled: true, priority: 10, source: '', target_id: '', match: 'all', conditions: [], action: 'instant', aggregate_minutes: 10 })

export function CollectorPage() {
  const { canAll, user } = usePerm()
  const [tab, setTab] = useState(0)
  const [owner, setOwner] = useState('')
  const currentOwner = useRef(owner)
  currentOwner.current = owner
  const generation = useRef(0)
  const [users, setUsers] = useState<{ id: string; username: string; display_name: string }[]>([])
  const [targets, setTargets] = useState<CollectorTarget[]>([])
  const [rules, setRules] = useState<CollectorRule[]>([])
  const [runs, setRuns] = useState<CollectorRun[]>([])
  const [items, setItems] = useState<WorkItem[]>([])
  const [deliveries, setDeliveries] = useState<Delivery[]>([])
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [filter, setFilter] = useState('')
  const [source, setSource] = useState('')
  const [targetDraft, setTargetDraft] = useState<CollectorTarget | null>(null)
  const [ruleDraft, setRuleDraft] = useState<CollectorRule | null>(null)
  const [ruleResults, setRuleResults] = useState<WorkEvent[] | null>(null)
  const [selectedRun, setSelectedRun] = useState<CollectorRun | null>(null)
  const [showImport, setShowImport] = useState(false)
  const [importKind, setImportKind] = useState('sheets')
  const [importText, setImportText] = useState('')
  const [importResult, setImportResult] = useState<Awaited<ReturnType<typeof importCollector>> | null>(null)
  const canManageUsers = canAll('user.update') && canAll('workflow.read') && !!user?.roles?.some(r => ['ADMIN', 'SUPER_ADMIN'].includes(r))
  const canEdit = !owner || owner === user?.id || canAll('workflow.write')
  async function load() {
    const requestedOwner = owner
    const request = ++generation.current
    if (requestedOwner !== currentOwner.current) return
    setLoading(true); setError('')
    try {
      const [t, r, run, work, notifications] = await Promise.all([
        collectorRecords<CollectorTarget>('target', owner), collectorRecords<CollectorRule>('rule', owner),
        collectorRecords<CollectorRun>('run', owner), collectorItems(owner), collectorRecords<Delivery>('delivery', owner),
      ])
      if (request !== generation.current || requestedOwner !== currentOwner.current) return
      setTargets(t.items); setRules(r.items); setRuns(run.items); setItems(work.items); setDeliveries(notifications.items)
    } catch (e) { if (request === generation.current && requestedOwner === currentOwner.current) setError(e instanceof Error ? e.message : String(e)) }
    finally { if (request === generation.current && requestedOwner === currentOwner.current) setLoading(false) }
  }
  useEffect(() => {
    setTargets([]); setRules([]); setRuns([]); setItems([]); setDeliveries([]); setMessage('')
    void load(); setTargetDraft(null); setRuleDraft(null); setSelectedRun(null); setRuleResults(null)
    return () => { generation.current++ }
  }, [owner])
  useEffect(() => {
    if (!runs.some(r => r.status === 'queued' || r.status === 'running')) return
    const timer = window.setInterval(() => void load(), 5000)
    return () => window.clearInterval(timer)
  }, [owner, runs])
  useEffect(() => {
    if (canManageUsers) apiGet<{ items: typeof users }>('/users').then(r => setUsers(r.items || [])).catch(() => {})
  }, [canManageUsers])
  async function perform(id: string, action: () => Promise<unknown>, success: string) {
    setBusy(id); setError(''); setMessage('')
    const requestedOwner = owner
    try { await action(); if (requestedOwner === currentOwner.current) { setMessage(success); await load() } }
    catch (e) { if (requestedOwner === currentOwner.current) setError(e instanceof Error ? e.message : String(e)) }
    finally { setBusy('') }
  }
  const latest = useMemo(() => {
    const map = new Map<string, CollectorRun>()
    for (const r of runs) if (!map.has(r.target_id)) map.set(r.target_id, r)
    return map
  }, [runs])
  const visibleTargets = targets.filter(t => (!source || t.source === source) && t.name.toLowerCase().includes(filter.toLowerCase()))
  const visibleItems = items.filter(i => (!source || i.source === source) && `${i.title} ${i.status}`.toLowerCase().includes(filter.toLowerCase()))
  const failed = [...latest.values()].filter(r => r.status === 'failed')
  const stats = [
    ['已启用来源', targets.filter(t => t.enabled && !t.config.search_only).length, `${targets.length} 个扫描目标 / 搜索目录`],
    ['在办事项', items.filter(i => !i.done && !['wiki', 'feishu_im'].includes(i.source)).length, '按已配置来源汇总'],
    ['待处理异常', failed.length, '查看运行记录定位原因'],
    ['待发送通知', deliveries.filter(d => ['pending', 'retry'].includes(d.status)).length, '独立重试，不重复扫描'],
  ]
  function toolbar() {
    return <div className="col-toolbar"><input aria-label="搜索采集内容" value={filter} onChange={e => setFilter(e.target.value)} placeholder="搜索名称、事项或状态…" />
      <select aria-label="筛选来源" value={source} onChange={e => setSource(e.target.value)}><option value="">全部来源</option>{Object.entries(sourceLabels).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></div>
  }
  return <div className="collector-page">
    <div className="col-page-header"><div><span className="col-eyebrow">PERSONAL WORK COLLECTOR · 内置 MCP</span><h1>工作采集中心</h1><p>连接工作来源，让每次变化都有记录，让待办与提醒保持同步。</p></div>
      <div className="col-actions">
        {canManageUsers && <select aria-label="采集配置所属用户" value={owner} disabled={!!busy} onChange={e => setOwner(e.target.value)}><option value="">我的工作来源</option>{users.filter(u => u.id !== user?.id).map(u => <option key={u.id} value={u.id}>{u.display_name || u.username}</option>)}</select>}
        <button type="button" className="btn-ghost" disabled={loading} onClick={() => void load()}>{loading ? '刷新中…' : '刷新'}</button>
        {canEdit && <button type="button" className="btn-primary" onClick={() => setTargetDraft(freshTarget())}>＋ 添加扫描目标</button>}
      </div>
    </div>
    {error && <div className="col-alert error" role="alert">{error}</div>}
    {message && <div className="col-alert good" role="status">{message}</div>}
    <nav className="col-tabs" aria-label="工作采集管理">{tabs.map((label, i) => <button type="button" key={label} className={tab === i ? 'active' : ''} onClick={() => { setTab(i); setFilter(''); setSource('') }}>{label}</button>)}</nav>
    {tab === 0 && <>
      <div className="col-stats">{stats.map(([label, n, desc]) => <article key={label}><span>{label}</span><strong>{n}</strong><small>{desc}</small></article>)}</div>
      {!targets.length && !loading ? <div className="col-empty col-welcome"><span className="col-empty-icon">↗</span><h2>从一个工作来源开始</h2><p>先连接个人工作账号，再添加周计划表、Jira 项目或 Wiki 文档。预览扫描结果后即可启用。</p>
        <div className="col-actions"><button className="btn-ghost" onClick={() => setTab(5)}>1. 连接个人账号</button><button className="btn-primary" onClick={() => setTargetDraft(freshTarget())}>2. 添加扫描目标</button></div></div> :
        <div className="col-overview-grid"><section className="col-card"><div className="col-card-title"><h2>来源运行状态</h2><button className="btn-ghost btn-sm" onClick={() => setTab(1)}>管理扫描范围</button></div>
          {targets.map(t => { const r = latest.get(t.id); return <div className="col-source-row" key={t.id}><span className={`col-source-mark ${t.source}`}>{sourceLabels[t.source]?.slice(0, 1)}</span><div><strong>{t.name}</strong><p>{sourceLabels[t.source]} · {r ? `最近采集 ${date(r.finished_at)}` : '尚未采集'}</p></div><span className={`col-status ${r?.status === 'failed' ? 'bad' : t.enabled ? 'good' : ''}`}>{!t.enabled ? '已暂停' : r ? statusLabel[r.status] : '等待首轮'}</span></div> })}
        </section><section className="col-card"><h2>需要关注</h2>{failed.length ? failed.map(r => <div className="col-issue" key={r.id}><strong>{r.target_name}</strong><p>{r.error}</p><button className="btn-ghost btn-sm" onClick={() => setSelectedRun(r)}>查看详情</button></div>) : <div className="col-empty"><h3>暂未发现采集异常</h3><p>授权过期、字段缺失和连接失败会出现在这里。</p></div>}</section></div>}
    </>}
    {tab === 1 && <section className="col-card">{toolbar()}
      <div className="table-wrapper"><table className="table"><thead><tr><th>扫描目标</th><th>身份与周期</th><th>最近结果</th><th>下次扫描</th><th>操作</th></tr></thead><tbody>
        {visibleTargets.map(t => { const run = latest.get(t.id); return <tr key={t.id}><td><strong>{t.name}</strong><small className="col-block">{sourceLabels[t.source]} · {t.enabled ? '已启用' : '已暂停'}</small></td><td>{t.config.identity === 'app' ? '应用身份' : '个人身份'}<small className="col-block">{t.cron || `每 ${t.interval_minutes} 分钟`}</small></td>
          <td><span className={`col-status ${run?.status === 'failed' ? 'bad' : run?.status === 'success' ? 'good' : ''}`}>{run ? `${statusLabel[run.status]} · ${run.count} 条` : '尚未采集'}</span></td><td>{t.enabled ? date(run?.next_at) : '—'}</td><td><div className="col-actions">
            {canEdit && <><button className="btn-ghost btn-sm" onClick={() => setTargetDraft(t)}>配置</button><button className="btn-ghost btn-sm" disabled={!!busy || !!t.config.search_only || ['queued', 'running'].includes(run?.status || '')} onClick={() => void perform(t.id, () => scanTarget(t.id, owner), '已加入扫描队列，运行结果会自动刷新。')}>{t.config.search_only ? '仅用于搜索' : busy === t.id ? '排队中…' : '立即扫描'}</button>
              <button className="btn-ghost btn-sm" disabled={!!busy} onClick={() => { if (window.confirm(`删除扫描目标“${t.name}”及其快照？历史运行记录会保留。`)) void perform(t.id, () => deleteCollectorRecord('target', t.id, owner), '扫描目标已删除。') }}>删除</button></>}</div></td></tr> })}
        {!visibleTargets.length && <tr><td colSpan={5}><div className="col-empty">没有符合条件的扫描目标</div></td></tr>}
      </tbody></table></div>
      {!owner && <button className="btn-ghost" onClick={() => { setShowImport(true); setImportResult(null) }}>导入原采集器配置</button>}
    </section>}
    {tab === 2 && <section className="col-card"><div className="col-card-title"><div><h2>变更如何通知</h2><p className="col-muted">按优先级匹配第一条规则。未命中时只记录；首次扫描默认建立基线。</p></div>{canEdit && <button className="btn-primary" onClick={() => { setRuleDraft(emptyRule()); setRuleResults(null) }}>＋ 新建规则</button>}</div>
      {!rules.length && <div className="col-empty"><h3>先定义一条关注规则</h3><p>例如：状态发生变化时通知我，或把 Wiki 更新合并发送。</p></div>}
      {rules.map(rule => <div className="col-rule-row" key={rule.id}><span className="col-rule-priority">{rule.priority}</span><div><strong>{rule.name}</strong><p>{sourceLabels[rule.source] || '全部来源'} · {rule.conditions.length} 个条件 · {rule.match === 'any' ? '任一满足' : '全部满足'}</p></div>
        <span className="col-status">{!rule.enabled ? '已停用' : rule.action === 'record' ? '仅记录' : rule.action === 'batched' ? `${rule.aggregate_minutes} 分钟合并` : '即时通知'}</span>{canEdit && <button className="btn-ghost" onClick={() => { setRuleDraft(structuredClone(rule)); setRuleResults(null) }}>编辑</button>}</div>)}
    </section>}
    {tab === 3 && <section className="col-card">{toolbar()}<div className="table-wrapper"><table className="table"><thead><tr><th>工作事项</th><th>来源</th><th>负责人</th><th>状态</th><th>截止日期</th><th>更新时间</th></tr></thead><tbody>
      {visibleItems.map(item => <tr key={item.id}><td>{safeLink(item.url) ? <a href={safeLink(item.url)} target="_blank" rel="noreferrer">{item.title || item.id} ↗</a> : item.title || item.id}</td><td>{sourceLabels[item.source]}</td><td>{item.assignees?.filter(Boolean).join('、') || '—'}</td><td><span className={`col-status ${item.done ? 'good' : ''}`}>{item.status || '—'}</span></td><td>{item.due_date || '—'}</td><td>{date(item.updated_at)}</td></tr>)}
      {!visibleItems.length && <tr><td colSpan={6}><div className="col-empty">暂无事项。完成首次采集后，工作内容会出现在这里。</div></td></tr>}
    </tbody></table></div></section>}
    {tab === 4 && <>
      <section className="col-card"><h2>采集记录</h2><div className="table-wrapper"><table className="table"><thead><tr><th>时间</th><th>目标</th><th>结果</th><th>事项 / 变更</th><th>详情</th></tr></thead><tbody>
        {runs.map(run => <tr key={run.id}><td>{date(run.started_at)}</td><td>{run.target_name}</td><td><span className={`col-status ${run.status === 'failed' ? 'bad' : 'good'}`}>{statusLabel[run.status]}</span></td><td>{run.count} / {run.changes}</td><td><button className="btn-ghost btn-sm" onClick={() => setSelectedRun(run)}>查看过程</button></td></tr>)}
        {!runs.length && <tr><td colSpan={5}><div className="col-empty">暂无运行记录</div></td></tr>}
      </tbody></table></div></section>
      <section className="col-card"><h2>通知投递</h2>{deliveries.length ? deliveries.map(d => <div className="col-source-row" key={d.id}><div><strong>{d.events?.[0]?.item.title || '工作变更通知'}</strong><p>{d.events?.length || 0} 条事件 · 已尝试 {d.attempts} 次 {d.error && `· ${d.error}`}</p></div><span className={`col-status ${d.status === 'sent' ? 'good' : 'warn'}`}>{statusLabel[d.status]}</span>{canEdit && ['retry', 'failed'].includes(d.status) && <button className="btn-ghost btn-sm" disabled={!!busy} onClick={() => void perform(d.id, () => retryDelivery(d.id, owner), '已安排重新投递。')}>重试</button>}</div>) : <p className="col-muted">尚未生成通知。</p>}</section>
    </>}
    {tab === 5 && <><p className="col-muted">这里管理当前登录用户的个人凭证。管理员管理其他用户的扫描配置时，也不会看到对方的密钥。</p><CollectorIdentity /></>}
    {targetDraft && <CollectorTargetEditor initial={targetDraft} owner={owner} onClose={() => setTargetDraft(null)} onSaved={() => { setTargetDraft(null); setMessage('扫描目标已保存。'); void load() }} />}
    {ruleDraft && <div className="modal-backdrop"><div className="col-dialog" role="dialog" aria-modal="true" aria-label="编辑通知规则">
      <div className="col-card-title"><h2>{ruleDraft.id ? '编辑规则' : '新建关注规则'}</h2><button className="btn-ghost" onClick={() => setRuleDraft(null)}>关闭</button></div>
      {error && <div role="alert" className="col-alert error">{error}</div>}
      <div className="col-grid-2"><label className="col-field">名称<input value={ruleDraft.name} onChange={e => setRuleDraft({ ...ruleDraft, name: e.target.value })} /></label><label className="col-field">优先级（小值优先）<input type="number" value={ruleDraft.priority} onChange={e => setRuleDraft({ ...ruleDraft, priority: Number(e.target.value) })} /></label>
        <label className="col-field">来源<select value={ruleDraft.source} onChange={e => setRuleDraft({ ...ruleDraft, source: e.target.value })}><option value="">全部来源</option>{Object.entries(sourceLabels).map(([k, v]) => <option key={k} value={k}>{v}</option>)}</select></label>
        <label className="col-field">扫描目标<select value={ruleDraft.target_id} onChange={e => setRuleDraft({ ...ruleDraft, target_id: e.target.value })}><option value="">全部目标</option>{targets.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}</select></label>
      </div>
      <label className="col-field">匹配方式<select value={ruleDraft.match} onChange={e => setRuleDraft({ ...ruleDraft, match: e.target.value })}><option value="all">所有条件满足</option><option value="any">任一条件满足</option></select></label>
      {ruleDraft.conditions.map((cond, i) => <div className="col-condition" key={i}><input aria-label={`条件 ${i + 1} 字段`} list="col-condition-fields" value={cond.field} onChange={e => setRuleDraft({ ...ruleDraft, conditions: ruleDraft.conditions.map((v, n) => n === i ? { ...v, field: e.target.value } : v) })} placeholder="字段名，如 status 或 状态" />
        <select aria-label={`条件 ${i + 1} 运算符`} value={cond.operator} onChange={e => setRuleDraft({ ...ruleDraft, conditions: ruleDraft.conditions.map((v, n) => n === i ? { ...v, operator: e.target.value } : v) })}>{Object.entries({ equals: '等于', not_equals: '不等于', contains: '包含', changed: '发生变化', in: '属于列表', is_me: '是我', due_within: 'N 天内到期', exists: '有值' }).map(([v, label]) => <option key={v} value={v}>{label}</option>)}</select>
        <input aria-label={`条件 ${i + 1} 值`} disabled={['changed', 'is_me', 'exists'].includes(cond.operator)} value={cond.value} onChange={e => setRuleDraft({ ...ruleDraft, conditions: ruleDraft.conditions.map((v, n) => n === i ? { ...v, value: e.target.value } : v) })} placeholder="条件值" />
        <button className="btn-ghost" aria-label={`移除条件 ${i + 1}`} onClick={() => setRuleDraft({ ...ruleDraft, conditions: ruleDraft.conditions.filter((_, n) => n !== i) })}>×</button></div>)}
      <datalist id="col-condition-fields">{['source', 'event_kind', 'title', 'status', 'assignee', 'due_date', '状态', '开发人', '验收人'].map(f => <option key={f} value={f} />)}</datalist>
      <button className="btn-ghost" onClick={() => setRuleDraft({ ...ruleDraft, conditions: [...ruleDraft.conditions, { field: 'status', operator: 'changed', value: '' }] })}>＋ 添加条件</button>
      <div className="col-grid-2"><label className="col-field">命中后<select value={ruleDraft.action} onChange={e => setRuleDraft({ ...ruleDraft, action: e.target.value })}><option value="record">仅记录</option><option value="instant">即时通知</option><option value="batched">合并通知</option></select></label>{ruleDraft.action === 'batched' && <label className="col-field">合并窗口（分钟）<input type="number" min={1} max={1440} value={ruleDraft.aggregate_minutes} onChange={e => setRuleDraft({ ...ruleDraft, aggregate_minutes: Number(e.target.value) })} /></label>}</div>
      <label className="col-check"><input type="checkbox" checked={ruleDraft.enabled} onChange={e => setRuleDraft({ ...ruleDraft, enabled: e.target.checked })} />启用这条规则</label>
      {ruleResults && <div className="col-rule-preview"><strong>最近 {ruleResults.length} 条事件的匹配结果</strong>{ruleResults.slice(0, 15).map(e => <p key={e.id}>{e.item.title}：{e.reason}</p>)}{!ruleResults.length && <p>暂无历史事件，请先完成一次采集。</p>}</div>}
      <div className="col-actions">
        <button className="btn-ghost" disabled={!!busy} onClick={async () => { setBusy('rule-preview'); setError(''); try { setRuleResults((await previewRule(ruleDraft, owner)).items) } catch (e) { setError(String(e)) } finally { setBusy('') } }}>试运行规则</button>
        <button className="btn-primary" disabled={!!busy || !ruleDraft.name} onClick={() => void perform('rule-save', async () => { await saveRule(ruleDraft, owner); setRuleDraft(null) }, '规则已保存。')}>保存规则</button>
        {ruleDraft.id && <button className="btn-ghost" disabled={!!busy} onClick={() => { if (window.confirm('删除这条规则？')) void perform('rule-delete', async () => { await deleteCollectorRecord('rule', ruleDraft.id, owner); setRuleDraft(null) }, '规则已删除。') }}>删除规则</button>}
      </div>
    </div></div>}
    {selectedRun && <div className="modal-backdrop"><div className="col-dialog" role="dialog" aria-modal="true" aria-label="采集运行详情"><div className="col-card-title"><h2>{selectedRun.target_name}</h2><button className="btn-ghost" onClick={() => setSelectedRun(null)}>关闭</button></div><p>{date(selectedRun.started_at)} · {statusLabel[selectedRun.status]} · {selectedRun.count} 条记录 / {selectedRun.changes} 条变化</p>{selectedRun.error && <div className="col-alert error">{selectedRun.error}</div>}
      {selectedRun.events?.map(e => <article className="col-event" key={e.id}><strong>{e.item.title || e.item.external_id}</strong><p>{e.event_kind} · {e.changed_fields.join('、')}</p><small>{e.reason}</small></article>)}
      {!selectedRun.events?.length && <p className="col-muted">本轮没有记录变更；失败原因会显示在上方。</p>}</div></div>}
    {showImport && <div className="modal-backdrop"><div className="col-dialog" role="dialog" aria-modal="true" aria-label="导入原采集器配置"><div className="col-card-title"><h2>导入原采集器配置</h2><button className="btn-ghost" onClick={() => { setShowImport(false); setImportText('') }}>关闭</button></div><p>先检查映射结果再导入。扫描目标和规则以停用状态保存，配置好个人身份后再启用。</p>
      <label className="col-field">配置类型<select value={importKind} onChange={e => { setImportKind(e.target.value); setImportResult(null) }}>{['sheets', 'wiki', 'rules', 'people'].map(k => <option key={k} value={k}>{k}.yaml</option>)}</select></label>
      <label className="col-field">YAML 内容<textarea rows={12} value={importText} onChange={e => { setImportText(e.target.value); setImportResult(null) }} /></label>
      {error && <div className="col-alert error">{error}</div>}
      {importResult && <div className="col-rule-preview"><p>{importResult.targets?.length || 0} 个扫描目标 · {importResult.rules?.length || 0} 条规则{importResult.person ? ' · 1 份个人配置' : ''}</p>{importResult.warnings?.map((w, i) => <p key={i}>{w}</p>)}<details><summary>查看映射结果</summary><pre>{JSON.stringify({ targets: importResult.targets, rules: importResult.rules, person: importResult.person }, null, 2)}</pre></details></div>}
      <div className="col-actions"><button className="btn-ghost" disabled={!!busy || !importText} onClick={async () => { setBusy('import-preview'); setError(''); try { setImportResult(await importCollector(importKind, importText, false)) } catch (e) { setError(String(e)) } finally { setBusy('') } }}>检查配置</button>
        <button className="btn-primary" disabled={!!busy || !importResult} onClick={() => void perform('import', async () => { await importCollector(importKind, importText, true); setImportText(''); setShowImport(false) }, '配置已导入，请复核后启用。')}>确认导入</button></div>
    </div></div>}
  </div>
}
