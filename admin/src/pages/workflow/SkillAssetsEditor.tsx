import { useEffect, useRef, useState } from 'react'
import { CodeDiff, CodeEditor } from '../../components/CodeEditor'
import { savedIndexMessage } from '../../api/savedIndex'
import { listSkillAssets, patchSkillAssets, readSkillAsset, SkillAssetConflict, type SkillAssetIndex } from '../../api/workflowmcp'
import { assetLanguage, decodeAsset, dirtyAsset, draftBlob, editAsset, MAX_ASSET_BYTES, sameBytes, validAssetPath, type AssetDraft } from './skillAssetDraft'
import './skill-assets.css'

type Props = {
  skillId: string
  canWrite: boolean
  canDelete: boolean
  onClose: () => void
  onSaved: () => void
  onStateChange: (dirty: boolean, busy: boolean) => void
}

function sizeLabel(size: number) { return size < 1024 ? `${size} B` : size < (1 << 20) ? `${(size / 1024).toFixed(1)} KiB` : `${(size / (1 << 20)).toFixed(1)} MiB` }
function status(d: AssetDraft) { return d.deleted ? '待删除' : !d.base ? '新增' : d.changed ? '修改' : '' }
type Tree = { dirs: Map<string, Tree>; files: AssetDraft[] }
function fileTree(drafts: AssetDraft[], query: string): Tree {
  const root: Tree = { dirs: new Map(), files: [] }
  for (const draft of drafts) {
    if (!draft.path.toLowerCase().includes(query.toLowerCase())) continue
    const parts = draft.path.split('/'); parts.pop()
    let node = root
    for (const part of parts) {
      if (!node.dirs.has(part)) node.dirs.set(part, { dirs: new Map(), files: [] })
      node = node.dirs.get(part)!
    }
    node.files.push(draft)
  }
  return root
}
function AssetTree({ tree, selected, select }: { tree: Tree; selected: string; select: (path: string) => void }) {
  return <>
    {[...tree.dirs].sort(([a], [b]) => a.localeCompare(b)).map(([name, child]) => <details key={name} open><summary>{name}/</summary><div className="skill-tree-branch"><AssetTree tree={child} selected={selected} select={select} /></div></details>)}
    {tree.files.sort((a, b) => a.path === 'SKILL.md' ? -1 : b.path === 'SKILL.md' ? 1 : a.path.localeCompare(b.path)).map(d => <button type="button" key={d.path} title={d.path} className={`skill-tree-file ${selected === d.path ? 'selected' : ''} ${d.deleted ? 'deleted' : ''}`} onClick={() => select(d.path)}><span>{d.path.split('/').pop()}</span><small>{status(d)}</small></button>)}
  </>
}

export function SkillAssetsEditor({ skillId, canWrite, canDelete, onClose, onSaved, onStateChange }: Props) {
  const [index, setIndex] = useState<SkillAssetIndex>()
  const [drafts, setDrafts] = useState<AssetDraft[]>([])
  const draftsRef = useRef(drafts); draftsRef.current = drafts
  const [selected, setSelected] = useState('SKILL.md')
  const [query, setQuery] = useState('')
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [newPath, setNewPath] = useState<string | null>(null)
  const [showDiff, setShowDiff] = useState(false)
  const [previewURL, setPreviewURL] = useState('')
  const [conflict, setConflict] = useState<SkillAssetIndex>()
  const [choices, setChoices] = useState<Record<string, 'local' | 'remote'>>({})
  const [remoteDiff, setRemoteDiff] = useState<{ path: string; text: string }>()
  const filesInput = useRef<HTMLInputElement>(null)
  const directoryInput = useRef<HTMLInputElement>(null)
  const replaceInput = useRef<HTMLInputElement>(null)
  const replacePath = useRef('')
  const changes = drafts.filter(dirtyAsset)
  const dirty = changes.length > 0
  const locked = busy || Boolean(conflict)
  const active = drafts.find(d => d.path === selected)

  function update(path: string, change: (draft: AssetDraft) => AssetDraft) {
    setDrafts(current => current.map(d => d.path === path ? change(d) : d))
  }

  async function load() {
    setBusy(true)
    try {
      const next = await listSkillAssets(skillId)
      setIndex(next)
      setDrafts(next.files.map(base => ({ path: base.path, base, changed: false, deleted: false })))
      setSelected('SKILL.md')
      setConflict(undefined); setChoices({}); setRemoteDiff(undefined); setShowDiff(false)
    } finally { setBusy(false) }
  }
  useEffect(() => { void load().catch(e => setError(String(e))) }, [skillId])
  useEffect(() => { onStateChange(dirty, busy); return () => onStateChange(false, false) }, [dirty, busy, onStateChange])

  useEffect(() => {
    const unload = (e: BeforeUnloadEvent) => { if (dirty || busy) { e.preventDefault(); e.returnValue = '' } }
    const navigation = (e: MouseEvent) => {
      const link = (e.target as Element).closest('a[href]')
      if (!link || link.hasAttribute('download') || link.getAttribute('target') === '_blank') return
      if (busy || (dirty && !window.confirm('有未保存的资产变更，确定离开并放弃变更？'))) { e.preventDefault(); e.stopPropagation() }
    }
    window.addEventListener('beforeunload', unload)
    document.addEventListener('click', navigation, true)
    return () => { window.removeEventListener('beforeunload', unload); document.removeEventListener('click', navigation, true) }
  }, [dirty, busy])

  // Load only the selected file. Preserve cached drafts when another file is selected.
  useEffect(() => {
    setLoading('')
    if (!index || !active || active.deleted || (active.blob && (!active.base || active.baseBytes))) return
    let cancelled = false
    const path = active.path
    setLoading(path); setError('')
    void readSkillAsset(skillId, path).then(async result => {
      if (result.revision !== index.skill.revision) throw new SkillAssetConflict('技能包已更新，请获取最新版本后继续')
      const bytes = new Uint8Array(await result.blob.arrayBuffer())
      if (!cancelled) update(path, d => ({ ...d, baseBytes: bytes, blob: d.blob || result.blob, ...(d.blob ? {} : decodeAsset(bytes)) }))
    }).catch(e => { if (!cancelled) { setError(String(e)); if (e instanceof SkillAssetConflict) void fetchConflict() } }).finally(() => { if (!cancelled) setLoading('') })
    return () => { cancelled = true }
  }, [selected, index, active?.deleted])

  useEffect(() => {
    if (!active || active.deleted || !active.blob || !/\.(png|jpe?g|gif|webp|avif|bmp)$/i.test(active.path)) { setPreviewURL(''); return }
    const url = URL.createObjectURL(draftBlob(active)); setPreviewURL(url)
    return () => URL.revokeObjectURL(url)
  }, [active])

  async function fetchConflict() {
    try { setConflict(await listSkillAssets(skillId)); setChoices({}) } catch (e) { setError(String(e)) }
  }

  async function save() {
    if (!index || !dirty || locked || loading) return
    setBusy(true); setError(''); setMessage('')
    try {
      const writes = changes.filter(d => !d.deleted).map(d => ({ path: d.path, blob: draftBlob(d) }))
      const deletes = changes.filter(d => d.deleted).map(d => d.path)
      const savedBytes = new Map(await Promise.all(writes.map(async f => [f.path, new Uint8Array(await f.blob.arrayBuffer())] as const)))
      const result = await patchSkillAssets(skillId, index.skill.revision!, writes, deletes)
      // Adopt this exact commit locally before a refresh: an unrelated later writer
      // must not cause a successful save to be offered for submission again.
      const committed = drafts.filter(d => !d.deleted).map(d => {
        if (!dirtyAsset(d)) return d
        const blob = draftBlob(d)
        return { ...d, blob, baseBytes: savedBytes.get(d.path), base: result.files.find(f => f.path === d.path)!, changed: false, deleted: false }
      })
      setDrafts(committed)
      setIndex({ skill: result.skill, files: committed.map(d => d.base!) })
      setMessage(savedIndexMessage(result.changed ? `已保存 ${writes.length} 个新增/修改文件，删除 ${deletes.length} 个文件` : '文件内容未变化，无需写入', result))
      onSaved()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      if (e instanceof SkillAssetConflict) await fetchConflict()
    } finally { setBusy(false) }
  }

  async function upload(files: File[], targetPath?: string) {
    if (locked || !index) return
    setBusy(true); setError(''); setMessage('')
    try {
      const pending = [...draftsRef.current]
      for (const file of files) {
        const path = targetPath || file.webkitRelativePath || file.name
        if (!validAssetPath(path)) throw new Error(`文件路径无效：${path}`)
        if (file.size > MAX_ASSET_BYTES) throw new Error(`${path} 超过 16 MiB`)
        const at = pending.findIndex(d => d.path === path)
        if (pending.some(d => d.path.toLowerCase() === path.toLowerCase() && d.path !== path)) throw new Error(`存在仅大小写不同的路径：${path}`)
        if (at >= 0 && !window.confirm(`${path} 已存在。确定替换？取消将跳过此文件。`)) continue
        const bytes = new Uint8Array(await file.arrayBuffer())
        const old = pending[at]
        let baseBytes = old?.baseBytes
        if (old?.base && !baseBytes) {
          const result = await readSkillAsset(skillId, path)
          if (result.revision !== index.skill.revision) throw new SkillAssetConflict('技能包已更新，本地草稿已保留')
          baseBytes = new Uint8Array(await result.blob.arrayBuffer())
        }
        const next: AssetDraft = { path, base: old?.base, baseBytes, blob: file, ...decodeAsset(bytes), changed: !baseBytes || !sameBytes(bytes, baseBytes), deleted: false }
        if (at >= 0) pending[at] = next; else pending.push(next)
      }
      setDrafts(pending)
      // Reopen to obtain the original baseline if an uploaded existing file wasn't cached.
      setIndex(current => current ? { ...current } : current)
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); if (e instanceof SkillAssetConflict) await fetchConflict() } finally { setBusy(false) }
  }

  function createFile() {
    const path = newPath || ''
    if (!validAssetPath(path)) { setError('请输入有效的相对路径，例如 scripts/helper.py'); return }
    if (drafts.some(d => d.path.toLowerCase() === path.toLowerCase())) { setError('该路径已存在，请编辑或替换已有文件'); return }
    setDrafts(current => [...current, { path, blob: new Blob([]), text: '', eol: '\n', bom: false, changed: true, deleted: false }])
    setSelected(path); setNewPath(null); setError('')
  }

  function revert(path: string) {
    setDrafts(current => current.flatMap(d => d.path !== path ? [d] : !d.base ? [] : [{ path: d.path, base: d.base, changed: false, deleted: false }]))
    setShowDiff(false)
    // Trigger lazy loading again even when reverting the currently selected file.
    setIndex(current => current ? { ...current } : current)
  }

  async function download() {
    if (!active) return
    try {
      const blob = active.blob ? draftBlob(active) : (await readSkillAsset(skillId, active.path)).blob
      const url = URL.createObjectURL(blob); const a = document.createElement('a')
      a.href = url; a.download = active.path.split('/').pop()!; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (e) { setError(String(e)) }
  }

  const remoteChanges = conflict ? [...new Set([...index!.files.map(f => f.path), ...conflict.files.map(f => f.path)])].filter(path => index!.files.find(f => f.path === path)?.sha256 !== conflict.files.find(f => f.path === path)?.sha256) : []
  const overlaps = changes.filter(d => remoteChanges.includes(d.path))
  async function compareRemote(path: string) {
    try {
      const result = await readSkillAsset(skillId, path)
      if (result.revision !== conflict?.skill.revision) { await fetchConflict(); throw new Error('服务端再次更新，请重新比较') }
      const decoded = decodeAsset(new Uint8Array(await result.blob.arrayBuffer()))
      if (decoded.text === undefined) throw new Error('此文件不支持文本比较，可下载查看')
      setRemoteDiff({ path, text: decoded.text }); setSelected(path)
    } catch (e) { setError(String(e)) }
  }
  async function rebase() {
    if (!conflict || overlaps.some(d => !choices[d.path])) return
    setBusy(true); setError('')
    try {
      const merged: AssetDraft[] = conflict.files.map(base => ({ path: base.path, base, changed: false, deleted: false }))
      for (const local of changes) {
        if (choices[local.path] === 'remote') continue
        const base = conflict.files.find(f => f.path === local.path)
        if (local.deleted && !base) continue
        let baseBytes: Uint8Array | undefined
        if (base) {
          const result = await readSkillAsset(skillId, local.path)
          if (result.revision !== conflict.skill.revision) throw new SkillAssetConflict('服务端再次更新，请重新获取最新内容')
          baseBytes = new Uint8Array(await result.blob.arrayBuffer())
        }
        const desiredBytes = new Uint8Array(await draftBlob(local).arrayBuffer())
        const next = { ...local, base, baseBytes, changed: !baseBytes || !sameBytes(desiredBytes, baseBytes) }
        const at = merged.findIndex(d => d.path === local.path)
        if (at >= 0) merged[at] = next; else merged.push(next)
      }
      setIndex(conflict); setDrafts(merged); setConflict(undefined); setChoices({}); setRemoteDiff(undefined)
      if (!merged.some(d => d.path === selected)) setSelected('SKILL.md')
      setMessage('已合并最新版本，请检查待保存变更后再次保存')
    } catch (e) { setError(String(e)); if (e instanceof SkillAssetConflict) await fetchConflict() } finally { setBusy(false) }
  }

  return <div className="skill-assets">
    <div className="skill-assets-toolbar">
      <div><h3>{index?.skill.name || skillId}</h3><span className="muted">版本 {index?.skill.version || '—'} · 修订 {index?.skill.revision || '—'} · {drafts.filter(d => !d.deleted).length} 个文件</span></div>
      <div className="table-actions">
        {canWrite && <><button type="button" className="btn-ghost btn-sm" disabled={locked || !index} onClick={() => setNewPath('')}>新建文件</button><button type="button" className="btn-ghost btn-sm" disabled={locked || !index} onClick={() => filesInput.current?.click()}>上传文件</button><button type="button" className="btn-ghost btn-sm" disabled={locked || !index} onClick={() => directoryInput.current?.click()}>上传目录</button><button type="button" className="btn-primary btn-sm" disabled={!dirty || locked || Boolean(loading)} onClick={() => void save()}>{busy ? '处理中…' : '保存变更'}</button></>}
        <button type="button" className="btn-ghost btn-sm" disabled={busy} onClick={() => { if (!dirty || window.confirm('确定放弃全部未保存变更？')) void load().catch(e => setError(String(e))) }}>重新加载</button>
        <button type="button" className="btn-ghost btn-sm" disabled={busy} onClick={() => { if (!dirty || window.confirm('确定离开并放弃未保存变更？')) onClose() }}>返回列表</button>
      </div>
    </div>
    <input ref={filesInput} type="file" multiple hidden onChange={e => { void upload(Array.from(e.target.files || [])); e.target.value = '' }} />
    <input ref={directoryInput} type="file" multiple hidden {...{ webkitdirectory: '', directory: '' }} onChange={e => { void upload(Array.from(e.target.files || [])); e.target.value = '' }} />
    <input ref={replaceInput} type="file" hidden onChange={e => { void upload(Array.from(e.target.files || []), replacePath.current); e.target.value = '' }} />
    {error && <div role="alert" className="banner banner-err">{error}</div>}
    {message && <div role="status" className="banner banner-ok">{message}</div>}
    {newPath !== null && <div className="skill-new-file"><label htmlFor="skill-new-path">新文件路径</label><input id="skill-new-path" value={newPath} onChange={e => setNewPath(e.target.value)} placeholder="scripts/helper.py" onKeyDown={e => { if (e.key === 'Enter') createFile() }} /><button type="button" disabled={locked} onClick={createFile}>创建草稿</button><button type="button" onClick={() => setNewPath(null)}>取消</button></div>}
    {conflict && <div className="skill-conflict" role="alert">
      <strong>服务端已更新至修订 {conflict.skill.revision}，本地草稿已保留</strong>
      <p>服务端变化：{remoteChanges.join('、') || '包信息已更新'}。重叠文件需逐项选择处理方式。</p>
      {overlaps.map(d => <div key={d.path} className="skill-conflict-row"><code>{d.path}</code>{conflict.files.some(f => f.path === d.path) && d.text !== undefined && <button type="button" onClick={() => void compareRemote(d.path)}>比较最新内容</button>}<label><input type="radio" name={`conflict-${d.path}`} checked={choices[d.path] === 'local'} onChange={() => setChoices(c => ({ ...c, [d.path]: 'local' }))} />保留本地操作</label><label><input type="radio" name={`conflict-${d.path}`} checked={choices[d.path] === 'remote'} onChange={() => setChoices(c => ({ ...c, [d.path]: 'remote' }))} />采用服务端</label></div>)}
      <button type="button" disabled={busy || overlaps.some(d => !choices[d.path])} onClick={() => void rebase()}>合并最新版本并保留草稿</button>
    </div>}
    <div className="skill-assets-layout">
      <aside className="skill-assets-tree"><label htmlFor="skill-asset-search">全部资产</label><input id="skill-asset-search" value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索文件路径" /><AssetTree tree={fileTree(drafts, query)} selected={selected} select={path => { setSelected(path); setShowDiff(false); setRemoteDiff(undefined) }} /></aside>
      <main className="skill-assets-content">
        {active ? <>
          <div className="skill-file-toolbar"><div><strong>{active.path}</strong><small className="muted">{sizeLabel(active.changed ? draftBlob(active).size : active.base?.size_bytes || active.blob?.size || 0)} {status(active)}</small></div><div className="table-actions">
            <button type="button" className="btn-ghost btn-sm" onClick={() => void download()} disabled={Boolean(loading)}>下载</button>
            {canWrite && !active.deleted && <button type="button" className="btn-ghost btn-sm" disabled={locked} onClick={() => { replacePath.current = active.path; replaceInput.current?.click() }}>替换文件</button>}
            {active.changed && active.text !== undefined && active.baseBytes && <button type="button" className="btn-ghost btn-sm" onClick={() => setShowDiff(!showDiff)}>{showDiff ? '继续编辑' : '查看差异'}</button>}
            {canWrite && dirtyAsset(active) && <button type="button" className="btn-ghost btn-sm" disabled={locked} onClick={() => revert(active.path)}>撤销变更</button>}
            {canWrite && canDelete && active.path !== 'SKILL.md' && !active.deleted && <button type="button" className="btn-danger btn-sm" disabled={locked} onClick={() => { if (!active.base) { setDrafts(current => current.filter(d => d.path !== active.path)); setSelected('SKILL.md') } else update(active.path, d => ({ ...d, deleted: true })) }}>删除文件</button>}
          </div></div>
          {active.deleted ? <p className="skill-file-notice">此文件将在保存时删除，保存前可撤销。</p> : loading === active.path ? <p className="skill-file-notice">正在读取文件…</p> : remoteDiff?.path === active.path ? <><p>左侧为服务端最新内容，右侧为本地草稿。</p><CodeDiff original={remoteDiff.text} modified={active.text || ''} language={assetLanguage(active.path)} /></> : showDiff && active.baseBytes ? <CodeDiff original={decodeAsset(active.baseBytes).text || ''} modified={active.text || ''} language={assetLanguage(active.path)} /> : active.text !== undefined ? <CodeEditor key={active.path} value={active.text} onChange={text => update(active.path, d => editAsset(d, text))} language={assetLanguage(active.path)} height="500px" readOnly={!canWrite || locked} /> : previewURL ? <div className="skill-image-preview"><img src={previewURL} alt={active.path} /></div> : <div className="skill-file-notice"><p>此文件支持下载和上传替换。非 UTF-8、二进制或超过 1 MiB 的文件不在浏览器中编辑。</p><code>{active.base?.sha256}</code></div>}
        </> : <p className="skill-file-notice">选择左侧文件查看内容。</p>}
      </main>
    </div>
    <section className="skill-pending"><strong>待保存变更：新增 {changes.filter(d => !d.base && !d.deleted).length} · 修改 {changes.filter(d => d.base && !d.deleted).length} · 删除 {changes.filter(d => d.deleted).length}</strong><span className="muted">只提交发生变化的文件，整批成功后生效。</span>{changes.map(d => <div key={d.path}><button type="button" className="btn-ghost btn-sm" onClick={() => setSelected(d.path)}>{status(d)} · {d.path}</button><button type="button" className="btn-ghost btn-sm" disabled={locked} onClick={() => revert(d.path)}>撤销</button></div>)}</section>
  </div>
}
