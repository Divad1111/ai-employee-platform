/**
 * 工作流MCP 管理页：工作流 / 技能包 / 知识库 三 Tab。
 */
import { FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { PageFeatureGuide } from '../../components/PageFeatureGuide'
import { EntityName } from '../../components/EntityName'
import { IconBook, IconZap } from '../../components/Icons'
import {
  deleteKnowledge,
  deleteSkill,
  deleteWorkflow,
  getKnowledge,
  getSkill,
  getWorkflow,
  importWorkflowMCPZip,
  listKnowledge,
  listSkills,
  listWorkflows,
  reindexKnowledge,
  unifiedSearch,
  upsertKnowledge,
  upsertSkill,
  upsertWorkflow,
  type KnowledgeDoc,
  type SkillPackage,
  type Workflow,
} from '../../api/workflowmcp'

type Tab = 'workflows' | 'skills' | 'knowledge'

type SearchHit = {
  id: string
  name?: string
  title?: string
  version?: string
  description?: string
  cursor_name?: string
  path?: string
  source?: string
  content?: string
  score?: number
}

type SearchSnapshot = {
  query: string
  workflows: Workflow[]
  skills: SkillPackage[]
  docs: KnowledgeDoc[]
}

const TAB_LABEL: Record<Tab, string> = {
  workflows: '工作流',
  skills: '技能包',
  knowledge: '知识库',
}

function hitToWorkflow(h: SearchHit): Workflow {
  return {
    id: h.id,
    name: h.name || h.id,
    version: h.version || '',
    description: h.description || '',
    skills: [],
  }
}

function hitToSkill(h: SearchHit): SkillPackage {
  return {
    id: h.id,
    name: h.name || h.id,
    cursor_name: h.cursor_name || '',
    version: h.version || '',
    description: h.description || '',
  }
}

function hitToDoc(h: SearchHit): KnowledgeDoc {
  const path = h.path || h.id
  const ns = path.includes('/') ? path.split('/')[0] : ''
  return {
    id: h.id,
    path,
    namespace: ns,
    title: h.title || h.name || h.id,
    source: h.source || '',
    content: h.content || '',
    version: '',
  }
}

export function WorkflowMcpPage() {
  const [tab, setTab] = useState<Tab>('workflows')
  const [q, setQ] = useState('')
  const [err, setErr] = useState('')
  const [ok, setOk] = useState('')
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [skills, setSkills] = useState<SkillPackage[]>([])
  const [docs, setDocs] = useState<KnowledgeDoc[]>([])
  const [allWorkflows, setAllWorkflows] = useState<Workflow[]>([])
  const [allSkills, setAllSkills] = useState<SkillPackage[]>([])
  const [allDocs, setAllDocs] = useState<KnowledgeDoc[]>([])
  const [searchSnap, setSearchSnap] = useState<SearchSnapshot | null>(null)
  const [selectedWf, setSelectedWf] = useState<string | null>(null)
  const [wfYaml, setWfYaml] = useState('')
  const [selectedSkill, setSelectedSkill] = useState<string | null>(null)
  const [skillMd, setSkillMd] = useState('')
  const [selectedDoc, setSelectedDoc] = useState<string | null>(null)
  const [docContent, setDocContent] = useState('')
  const [docPath, setDocPath] = useState('')
  const [nsFilter, setNsFilter] = useState('')
  const [importing, setImporting] = useState(false)
  const importInputRef = useRef<HTMLInputElement>(null)

  const applyLists = useCallback((w: Workflow[], s: SkillPackage[], k: KnowledgeDoc[]) => {
    setAllWorkflows(w)
    setAllSkills(s)
    setAllDocs(k)
    setWorkflows(w)
    setSkills(s)
    setDocs(k)
  }, [])

  const load = useCallback(async () => {
    setErr('')
    try {
      const [w, s, k] = await Promise.all([
        listWorkflows(),
        listSkills(),
        listKnowledge(nsFilter),
      ])
      const wi = w.items || []
      const si = s.items || []
      const ki = k.items || []
      applyLists(wi, si, ki)
      setSearchSnap(null)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }, [nsFilter, applyLists])

  useEffect(() => {
    void load()
  }, [load])

  function showSearchOnTab(next: Tab, snap: SearchSnapshot) {
    setTab(next)
    setSelectedWf(null)
    setSelectedSkill(null)
    setSelectedDoc(null)
    setWorkflows(snap.workflows)
    setSkills(snap.skills)
    setDocs(snap.docs)
    setOk('')
  }

  async function onSearch(e: FormEvent) {
    e.preventDefault()
    const query = q.trim()
    if (!query) {
      setSearchSnap(null)
      setWorkflows(allWorkflows)
      setSkills(allSkills)
      setDocs(allDocs)
      setOk('')
      return
    }
    try {
      const res = await unifiedSearch(query)
      const snap: SearchSnapshot = {
        query,
        workflows: ((res.workflows || []) as SearchHit[]).map(hitToWorkflow),
        skills: ((res.skills || []) as SearchHit[]).map(hitToSkill),
        docs: ((res.knowledge || []) as SearchHit[]).map(hitToDoc),
      }
      setSearchSnap(snap)
      // 默认切到第一个有结果的页签并展示对应列表
      const preferred: Tab =
        snap.workflows.length > 0 ? 'workflows' : snap.skills.length > 0 ? 'skills' : 'knowledge'
      showSearchOnTab(preferred, snap)
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  async function clearSearch() {
    setQ('')
    setSearchSnap(null)
    setOk('')
    await load()
  }

  async function openWorkflow(id: string) {
    setSelectedWf(id)
    const res = await getWorkflow(id)
    setWfYaml(res.yaml || '')
  }

  async function saveWorkflow(e: FormEvent) {
    e.preventDefault()
    try {
      await upsertWorkflow(wfYaml)
      setOk('工作流已保存')
      setSelectedWf(null)
      await load()
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  async function openSkill(id: string) {
    setSelectedSkill(id)
    const res = await getSkill(id)
    setSkillMd(res.skill_md || '')
  }

  async function saveSkill(e: FormEvent) {
    e.preventDefault()
    try {
      await upsertSkill(skillMd)
      setOk('技能包已保存')
      setSelectedSkill(null)
      await load()
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  async function openDoc(id: string) {
    setSelectedDoc(id)
    const res = await getKnowledge(id)
    setDocPath(res.doc.path)
    setDocContent(
      `---\ntitle: ${res.doc.title}\nnamespace: ${res.doc.namespace}\nversion: ${res.doc.version}\n---\n${res.doc.content}`,
    )
  }

  async function saveDoc(e: FormEvent) {
    e.preventDefault()
    try {
      await upsertKnowledge(docPath || 'untitled.md', docContent)
      setOk('知识文档已保存')
      setSelectedDoc(null)
      await load()
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  async function onImportZip(file: File | undefined) {
    if (!file) return
    setImporting(true)
    setErr('')
    setOk('')
    try {
      const res = await importWorkflowMCPZip(file)
      const s = res.stats || { workflows: 0, skills: 0, knowledge: 0, errors: 0 }
      setOk(
        `导入完成：工作流 ${s.workflows} / 技能包 ${s.skills} / 知识 ${s.knowledge}` +
          (s.errors ? `（失败 ${s.errors}）` : ''),
      )
      await load()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setImporting(false)
      if (importInputRef.current) importInputRef.current.value = ''
    }
  }

  const searchActive = searchSnap !== null
  const rowCount =
    tab === 'workflows' ? workflows.length : tab === 'skills' ? skills.length : docs.length

  return (
    <div className="page">
      <div className="page-header">
        <h1>工作流管理</h1>
        <p className="muted">管理工作流、技能包与知识库（工作流MCP）</p>
      </div>
      <PageFeatureGuide
        title="工作流MCP"
        summary="管理工作流、技能包与知识库，支持 PersonalWorkMCP 数据包导入与语义索引。"
        steps={[
          {
            step: '1',
            title: '授权规则',
            desc: '仅工作流需要授权给数字员工；技能与知识通过工作流引用自动放行。',
            tag: '权限隔离',
          },
          {
            step: '2',
            title: '数据包导入',
            desc: '点击下方「导入 data.zip」，上传 PersonalWorkMCP 的 data 目录压缩包（需含 workflows/、skills/、knowledge/）。',
            tag: '资产导入',
          },
          {
            step: '3',
            title: '语义检索',
            desc: '重建索引将刷新知识库全文检索，提升工作流执行上下文准确率。',
            tag: '全文检索',
          },
        ]}
      />
      {err && <div className="error">{err}</div>}
      {ok && <div className="ok-msg">{ok}</div>}

      <form className="inline-form" onSubmit={onSearch} style={{ marginBottom: '1rem' }}>
        <input
          placeholder="统一搜索工作流 / 技能 / 知识..."
          value={q}
          onChange={(e) => setQ(e.target.value)}
          style={{ flex: 1 }}
        />
        <button type="submit">搜索</button>
        {searchActive && (
          <button type="button" className="btn-ghost" onClick={() => void clearSearch()}>
            清除筛选
          </button>
        )}
        <input
          ref={importInputRef}
          type="file"
          accept=".zip,application/zip"
          style={{ display: 'none' }}
          onChange={(e) => void onImportZip(e.target.files?.[0])}
        />
        <button
          type="button"
          className="btn-ghost"
          disabled={importing}
          onClick={() => importInputRef.current?.click()}
          title="上传 PersonalWorkMCP data.zip"
        >
          {importing ? '导入中…' : '导入 data.zip'}
        </button>
        <button
          type="button"
          className="btn-ghost"
          onClick={() => {
            void reindexKnowledge()
              .then((r) => setOk(`已重建 ${r.reindexed} 篇文档索引`))
              .catch((e) => setErr(String(e)))
          }}
        >
          重建知识索引
        </button>
      </form>

      {searchActive && searchSnap && (
        <div className="wf-search-summary">
          <span className="muted">
            搜索「{searchSnap.query}」：
          </span>
          {(
            [
              ['workflows', searchSnap.workflows.length],
              ['skills', searchSnap.skills.length],
              ['knowledge', searchSnap.docs.length],
            ] as const
          ).map(([key, count]) => (
            <button
              key={key}
              type="button"
              className={tab === key ? 'wf-search-chip is-active' : 'wf-search-chip'}
              onClick={() => showSearchOnTab(key, searchSnap)}
            >
              {TAB_LABEL[key]} {count}
            </button>
          ))}
        </div>
      )}

      <div className="wf-tabs">
        {(
          [
            ['workflows', '工作流'],
            ['skills', '技能包'],
            ['knowledge', '知识库'],
          ] as const
        ).map(([k, label]) => (
          <button
            key={k}
            type="button"
            className={tab === k ? 'btn-sm' : 'btn-ghost btn-sm'}
            onClick={() => {
              setTab(k)
              setSelectedWf(null)
              setSelectedSkill(null)
              setSelectedDoc(null)
              if (searchSnap) {
                setWorkflows(searchSnap.workflows)
                setSkills(searchSnap.skills)
                setDocs(searchSnap.docs)
              }
            }}
          >
            {label}
            {searchActive && searchSnap
              ? ` (${k === 'workflows' ? searchSnap.workflows.length : k === 'skills' ? searchSnap.skills.length : searchSnap.docs.length})`
              : ''}
          </button>
        ))}
      </div>

      {/* 单一 panel，避免切 Tab 时整块卸载导致跳动 */}
      <div className="panel wf-panel">
        <div className="wf-panel-toolbar">
          <h3>
            {TAB_LABEL[tab]}列表
            <span className="muted" style={{ fontWeight: 400, marginLeft: 8, fontSize: 13 }}>
              {searchActive ? `筛选 ${rowCount} 条` : `共 ${rowCount} 条`}
            </span>
          </h3>
          <div className="wf-panel-toolbar-actions">
            {/* 固定占位，避免仅知识库页签出现过滤框时工具栏宽度变化 */}
            <input
              className="wf-ns-filter"
              placeholder="namespace 过滤（如 cases）"
              value={nsFilter}
              onChange={(e) => setNsFilter(e.target.value)}
              disabled={tab !== 'knowledge' || searchActive}
              style={{ visibility: tab === 'knowledge' ? 'visible' : 'hidden' }}
              title={
                tab !== 'knowledge'
                  ? undefined
                  : searchActive
                    ? '搜索筛选中时禁用 namespace 过滤'
                    : undefined
              }
              tabIndex={tab === 'knowledge' ? 0 : -1}
              aria-hidden={tab !== 'knowledge'}
            />
            {tab === 'workflows' && (
              <button
                type="button"
                className="btn-sm"
                onClick={() => {
                  setSelectedWf('__new__')
                  setWfYaml(
                    'id: my-workflow\nname: 新工作流\nversion: 1.0.0\ndescription: \nskills: []\nknowledge: []\nsteps:\n  - id: step1\n    description: 第一步\n',
                  )
                }}
              >
                新建
              </button>
            )}
            {tab === 'skills' && (
              <button
                type="button"
                className="btn-sm"
                onClick={() => {
                  setSelectedSkill('__new__')
                  setSkillMd(
                    '---\nname: my-skill\nid: my.skill\ntitle: 我的技能\nversion: 1.0.0\ndescription: \ndisable-model-invocation: true\n---\n# 我的技能\n\n说明...\n',
                  )
                }}
              >
                新建
              </button>
            )}
            {tab === 'knowledge' && (
              <button
                type="button"
                className="btn-sm"
                onClick={() => {
                  setSelectedDoc('__new__')
                  setDocPath('notes/example.md')
                  setDocContent('---\ntitle: 示例\nnamespace: notes\nversion: 1.0.0\n---\n# 示例\n\n内容...\n')
                }}
              >
                新建
              </button>
            )}
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table wf-table">
            <colgroup>
              <col />
              <col style={{ width: '22%' }} />
              <col style={{ width: '18%' }} />
              <col className="col-actions" />
            </colgroup>
            <thead>
              <tr>
                <th>{tab === 'knowledge' ? '标题 / ID' : '名称 / ID'}</th>
                <th>{tab === 'workflows' ? '版本' : tab === 'skills' ? 'cursor_name' : '命名空间'}</th>
                <th>{tab === 'workflows' ? '技能引用' : tab === 'skills' ? '版本' : '路径'}</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {tab === 'workflows' &&
                workflows.map((w) => (
                  <tr key={w.id}>
                    <td>
                      <EntityName name={w.name} id={w.id} icon={<IconZap size={16} />} />
                      {w.description ? (
                        <div className="muted" style={{ fontSize: 12 }}>
                          {w.description}
                        </div>
                      ) : null}
                    </td>
                    <td>{w.version || '—'}</td>
                    <td>{(w.skills || []).join(', ') || '—'}</td>
                    <td className="col-actions">
                      <div className="table-actions">
                        <button type="button" className="btn-ghost btn-sm" onClick={() => void openWorkflow(w.id)}>
                          编辑
                        </button>
                        <button
                          type="button"
                          className="btn-danger btn-sm"
                          onClick={() => void deleteWorkflow(w.id).then(load).catch((e) => setErr(String(e)))}
                        >
                          删除
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              {tab === 'skills' &&
                skills.map((s) => (
                  <tr key={s.id}>
                    <td>
                      <EntityName name={s.name || s.id} id={s.id} icon={<IconZap size={16} />} />
                    </td>
                    <td>{s.cursor_name || '—'}</td>
                    <td>{s.version || '—'}</td>
                    <td className="col-actions">
                      <div className="table-actions">
                        <button type="button" className="btn-ghost btn-sm" onClick={() => void openSkill(s.id)}>
                          编辑
                        </button>
                        <button
                          type="button"
                          className="btn-danger btn-sm"
                          onClick={() => void deleteSkill(s.id).then(load).catch((e) => setErr(String(e)))}
                        >
                          删除
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              {tab === 'knowledge' &&
                docs.map((d) => (
                  <tr key={d.id}>
                    <td>
                      <EntityName name={d.title} id={d.id} icon={<IconBook size={16} />} />
                    </td>
                    <td>{d.namespace || '—'}</td>
                    <td className="muted">{d.path || '—'}</td>
                    <td className="col-actions">
                      <div className="table-actions">
                        <button type="button" className="btn-ghost btn-sm" onClick={() => void openDoc(d.id)}>
                          编辑
                        </button>
                        <button
                          type="button"
                          className="btn-danger btn-sm"
                          onClick={() => void deleteKnowledge(d.id).then(load).catch((e) => setErr(String(e)))}
                        >
                          删除
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              {rowCount === 0 && (
                <tr>
                  <td colSpan={4} className="muted" style={{ textAlign: 'center', padding: '1.5rem' }}>
                    {searchActive ? '当前页签无匹配结果，可点击上方计数切换到其他页签' : '暂无数据'}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {tab === 'workflows' && selectedWf && (
          <form className="stack-form" onSubmit={saveWorkflow} style={{ marginTop: '1rem' }}>
            <label>
              YAML
              <textarea rows={16} value={wfYaml} onChange={(e) => setWfYaml(e.target.value)} style={{ fontFamily: 'monospace' }} />
            </label>
            <div className="table-actions">
              <button type="submit">保存</button>
              <button type="button" className="btn-ghost" onClick={() => setSelectedWf(null)}>
                取消
              </button>
            </div>
          </form>
        )}

        {tab === 'skills' && selectedSkill && (
          <form className="stack-form" onSubmit={saveSkill} style={{ marginTop: '1rem' }}>
            <label>
              SKILL.md
              <textarea rows={16} value={skillMd} onChange={(e) => setSkillMd(e.target.value)} style={{ fontFamily: 'monospace' }} />
            </label>
            <div className="table-actions">
              <button type="submit">保存</button>
              <button type="button" className="btn-ghost" onClick={() => setSelectedSkill(null)}>
                取消
              </button>
            </div>
          </form>
        )}

        {tab === 'knowledge' && selectedDoc && (
          <form className="stack-form" onSubmit={saveDoc} style={{ marginTop: '1rem' }}>
            <label>
              路径
              <input value={docPath} onChange={(e) => setDocPath(e.target.value)} required />
            </label>
            <label>
              Markdown
              <textarea
                rows={14}
                value={docContent}
                onChange={(e) => setDocContent(e.target.value)}
                style={{ fontFamily: 'monospace' }}
              />
            </label>
            <div className="table-actions">
              <button type="submit">保存</button>
              <button type="button" className="btn-ghost" onClick={() => setSelectedDoc(null)}>
                取消
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  )
}
