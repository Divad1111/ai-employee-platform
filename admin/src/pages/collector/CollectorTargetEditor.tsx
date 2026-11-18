import { useState } from 'react'
import { CollectorListField } from './CollectorListField'
import { discoverTarget, previewTarget, saveTarget, type CollectorRun, type CollectorTarget } from '../../api/collector'

export const sourceLabels: Record<string, string> = { jira: 'Jira', sheet: '飞书电子表格', wiki: '飞书 Wiki', bitable: '多维表格', meego: '飞书项目', feishu_im: '飞书消息' }
export const freshTarget = (): CollectorTarget => ({
  id: '', name: '', source: 'sheet', enabled: false, url: '', interval_minutes: 60, cron: '',
  timezone: 'Asia/Shanghai', notify_baseline: false, version: 0,
  config: { identity: 'user', range: 'A1:AZ1000', header_row: 1, person_columns: ['开发人', '验收人'], only_mine: true, id_column: '单号', title_column: '标题', status_column: '状态', due_column: '截止日期' },
})

export function CollectorTargetEditor({ initial, owner, onClose, onSaved }: {
  initial: CollectorTarget; owner?: string; onClose: () => void; onSaved: () => void
}) {
  const [target, setTarget] = useState<CollectorTarget>(() => structuredClone(initial))
  const [discovered, setDiscovered] = useState<Record<string, any> | null>(null)
  const [preview, setPreview] = useState<CollectorRun | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [filter, setFilter] = useState(JSON.stringify(initial.config.filter || {}, null, 2))
  const cfg = target.config
  const set = (key: keyof CollectorTarget, value: unknown) => { setTarget(t => ({ ...t, [key]: value })); setPreview(null) }
  const config = (key: string, value: unknown) => { setTarget(t => ({ ...t, config: { ...t.config, [key]: value } })); setPreview(null) }
  const field = (key: string, label: string, placeholder = '') => <label className="col-field">{label}
    <input list={['id_column', 'title_column', 'due_column', 'status_column', 'person_field'].includes(key) ? 'collector-column-options' : undefined}
      value={cfg[key] || ''} placeholder={placeholder} onChange={e => config(key, e.target.value)} />
  </label>
  const listField = (key: string, label: string, placeholder = '') => <CollectorListField key={key} label={label}
    value={cfg[key] || []} placeholder={placeholder} onChange={values => config(key, values)} />
  async function action(kind: string) {
    setError(''); setBusy(kind)
    try {
      const value = structuredClone(target)
      if (value.source === 'bitable' && filter.trim() && filter.trim() !== '{}') value.config.filter = JSON.parse(filter)
      else delete value.config.filter
      if (kind === 'discover') setDiscovered(await discoverTarget(value, owner))
      if (kind === 'preview') setPreview(await previewTarget(value, owner))
      if (kind === 'save') { await saveTarget(value, owner); onSaved() }
    } catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setBusy('') }
  }
  const headers: string[] = (discovered?.sample_rows?.[(cfg.header_row || 1) - 1] || []).map(String)
  return <div className="modal-backdrop col-modal-backdrop"><div className="col-editor" role="dialog" aria-modal="true" aria-label="配置扫描目标">
    <div className="col-editor-header"><div><span className="col-eyebrow">扫描目标</span><h2>{initial.id ? '编辑采集范围' : '连接一个工作来源'}</h2></div>
      <button type="button" className="btn-ghost" disabled={!!busy} onClick={onClose}>关闭</button></div>
    <div className="col-editor-content">
      <div className="col-editor-form">
        {error && <div role="alert" className="col-alert error">{error}</div>}
        <fieldset disabled={!!busy}>
          <legend><span>1</span> 选择来源与访问身份</legend>
          <div className="col-grid-2">
            <label className="col-field">名称<input value={target.name} onChange={e => set('name', e.target.value)} placeholder="例如：研发周计划" /></label>
            <label className="col-field">来源<select value={target.source} onChange={e => { set('source', e.target.value); setDiscovered(null) }}>{Object.entries(sourceLabels).map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select></label>
            {target.source !== 'jira' && target.source !== 'meego' && <label className="col-field">访问身份<select value={cfg.identity || 'user'} onChange={e => config('identity', e.target.value)}><option value="user">我的飞书用户身份</option><option value="app">显式使用飞书应用身份</option></select><small>个人授权失效时会提示重新授权。</small></label>}
          </div>
          {['sheet', 'wiki', 'bitable'].includes(target.source) && <label className="col-field">资源链接<input value={target.url} onChange={e => { set('url', e.target.value); setDiscovered(null) }} placeholder="粘贴飞书文档、表格或 Wiki 链接" /></label>}
          {['sheet', 'wiki', 'bitable'].includes(target.source) && <button type="button" className="btn-ghost" onClick={() => void action('discover')}>读取资源结构</button>}
        </fieldset>
        <fieldset disabled={!!busy}>
          <legend><span>2</span> 配置扫描范围与字段</legend>
          {target.source === 'jira' && <>
            <label className="col-field">JQL<textarea rows={4} value={cfg.jql || ''} onChange={e => config('jql', e.target.value)} placeholder="project = GAME AND assignee = currentUser() ORDER BY updated DESC" /></label>
            <label className="col-check"><input type="checkbox" checked={!!cfg.incremental} onChange={e => config('incremental', e.target.checked)} />启用增量对账</label>
            <p className="col-muted">增量 JQL 应覆盖已完成事项，才能跟踪完成状态。未启用时扫描整个查询结果并对比快照。</p>
          </>}
          {target.source === 'sheet' && <>
            {discovered?.sheets && <div className="col-check-list">{discovered.sheets.map((tab: any) => <label key={tab.sheet_id} className="col-check">
              <input type="checkbox" checked={(cfg.sheet_ids || []).includes(tab.sheet_id)} onChange={e => {
                const ids = new Set<string>(cfg.sheet_ids || []); if (e.target.checked) ids.add(tab.sheet_id); else ids.delete(tab.sheet_id)
                config('sheet_ids', [...ids]); config('sheet_id', [...ids][0] || '')
              }} />{tab.title} <small>{tab.sheet_id}</small>
            </label>)}</div>}
            {!discovered?.sheets && listField('sheet_ids', '工作表 ID', '多个 ID 用逗号分隔')}
            <label className="col-check"><input type="checkbox" checked={!!cfg.discover_sheets} onChange={e => config('discover_sheets', e.target.checked)} />自动发现工作簿内的工作表</label>
            <div className="col-grid-2">
              {field('range', '读取区域', 'A1:AZ1000')}
              <label className="col-field">区域内表头行<input type="number" min={1} value={cfg.header_row || 1} onChange={e => config('header_row', Number(e.target.value))} /></label>
              {field('id_column', '唯一标识列', '单号')}{field('title_column', '事项标题列', '标题')}
              {listField('person_columns', '人员列', '开发人, 验收人')}
              {field('due_column', '截止日期列', '截止日期')}{field('status_column', '状态列', '状态')}
            </div>
            {discovered?.sheets && <button type="button" className="btn-ghost" onClick={() => void action('discover')}>读取所选工作表的列名</button>}
            <datalist id="collector-column-options">{headers.filter(Boolean).map((h, i) => <option key={`${h}-${i}`} value={h} />)}</datalist>
          </>}
          {target.source === 'bitable' && <>
            {discovered?.tables ? <label className="col-field">数据表<select value={cfg.table_id || ''} onChange={e => config('table_id', e.target.value)}><option value="">请选择</option>{discovered.tables.map((t: any) => <option key={t.table_id} value={t.table_id}>{t.name}</option>)}</select></label> : field('table_id', '数据表 Table ID')}
            <div className="col-grid-2">{field('view_id', '视图 ID（可选）')}{field('person_field', '人员字段', '负责人')}{field('title_column', '标题字段', '标题')}{field('due_column', '截止日期字段', '截止日期')}{field('status_column', '状态字段', '状态')}</div>
            <details><summary>高级筛选</summary><label className="col-field">飞书筛选条件 JSON<textarea rows={6} value={filter} onChange={e => setFilter(e.target.value)} /></label></details>
          </>}
          {target.source === 'wiki' && <>
            <label className="col-check"><input type="checkbox" checked={!!cfg.search_only} onChange={e => { config('search_only', e.target.checked); if (e.target.checked) set('enabled', false) }} />仅作为内容检索目录（不定时扫描或通知）</label>
            <label className="col-check"><input type="checkbox" checked={!!cfg.include_whiteboards} onChange={e => config('include_whiteboards', e.target.checked)} />监控文档内嵌画板（需画板读取权限）</label>
            <label className="col-check"><input type="checkbox" checked={!!cfg.recursive} onChange={e => config('recursive', e.target.checked)} />包含目录下的子文档</label>
            <div className="col-grid-2">
              <label className="col-field">递归深度<input type="number" min={0} max={10} value={cfg.max_depth ?? 3} onChange={e => config('max_depth', Number(e.target.value))} /></label>
              <label className="col-field">最多节点<input type="number" min={1} max={1000} value={cfg.max_nodes || 200} onChange={e => config('max_nodes', Number(e.target.value))} /></label>
              {field('include', '标题包含（正则，可选）')}{field('exclude', '标题排除（正则，可选）')}
            </div>
            <p className="col-muted">监控 docx/doc 正文变化，并将快照用于 MCP 内容检索。</p>
          </>}
          {target.source === 'meego' && <>{field('project_key', '项目 Project Key')}<label className="col-field">首次回溯天数<input type="number" min={1} max={3650} value={cfg.initial_days || 90} onChange={e => config('initial_days', Number(e.target.value))} /></label></>}
          {target.source === 'feishu_im' && <>
            {field('chat_id', '群 Chat ID', 'oc_…')}
            <label className="col-field">首次回溯天数<input type="number" min={1} max={90} value={cfg.initial_days || 7} onChange={e => config('initial_days', Number(e.target.value))} /></label>
            <label className="col-check"><input type="checkbox" checked={cfg.only_mentioned !== false} onChange={e => config('only_mentioned', e.target.checked)} />只关注 @ 我的消息</label>
          </>}
          {['jira', 'sheet', 'bitable', 'meego'].includes(target.source) && <>
            <label className="col-check"><input type="checkbox" checked={cfg.only_mine !== false} onChange={e => config('only_mine', e.target.checked)} />按我的姓名、别名或账号匹配事项</label>
            {listField('done_statuses', '视为完成的状态', '留空使用完成、已完成、Done、Closed 等默认值')}
          </>}
        </fieldset>
        <fieldset disabled={!!busy}>
          <legend><span>3</span> 调度与首次采集</legend>
          <div className="col-grid-2">
            <label className="col-field">扫描间隔（分钟）<input type="number" min={1} max={10080} value={target.interval_minutes} onChange={e => set('interval_minutes', Number(e.target.value))} /></label>
            <label className="col-field">时区<input value={target.timezone} onChange={e => set('timezone', e.target.value)} /></label>
          </div>
          <details><summary>Cron 调度</summary><label className="col-field">Cron 表达式<input value={target.cron} onChange={e => set('cron', e.target.value)} placeholder="0 9 * * 1-5" /><small>填写后优先使用 Cron。</small></label></details>
          <label className="col-check"><input type="checkbox" checked={target.notify_baseline} onChange={e => set('notify_baseline', e.target.checked)} />首次扫描也应用通知规则</label>
          <label className="col-check"><input type="checkbox" disabled={!!cfg.search_only} checked={target.enabled} onChange={e => set('enabled', e.target.checked)} />保存后启用定时采集</label>
        </fieldset>
      </div>
      <aside className="col-preview">
        <span className="col-eyebrow">配置预览</span><h3>先看看会采集什么</h3>
        <p>预览会读取上游数据并计算命中结果，不更新正式快照，也不发送通知。</p>
        <button type="button" className="btn-primary" disabled={!!busy} onClick={() => void action('preview')}>{busy === 'preview' ? '正在读取与匹配…' : '预览本次扫描'}</button>
        {busy && <p role="status">正在{busy === 'discover' ? '读取资源结构' : busy === 'save' ? '保存配置' : '生成预览'}…</p>}
        {discovered && <div className="col-preview-result"><strong>资源已识别</strong><p>{discovered.type} · {discovered.sheets?.length ?? discovered.children?.length ?? discovered.tables?.length ?? 1} 个资源</p></div>}
        {preview && <div className="col-preview-result"><strong>找到 {preview.count} 条记录</strong><p>{preview.changes} 条变更 · 以下展示样例</p>
          {preview.samples?.map(item => <article key={item.id}><b>{item.title || item.id}</b><p>{item.status || '未填写状态'} {item.due_date && `· ${item.due_date}`}</p></article>)}
          {preview.events?.slice(0, 6).map(e => <p key={e.id} className="col-muted">{e.item.title}：{e.reason}</p>)}
        </div>}
      </aside>
    </div>
    <div className="col-editor-footer"><span>配置保存后，下一轮采集使用新版本。</span><button type="button" className="btn-primary" disabled={!!busy || !target.name} onClick={() => void action('save')}>保存扫描目标</button></div>
  </div></div>
}
