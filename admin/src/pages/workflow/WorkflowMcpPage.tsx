/**
 * 工作流MCP 管理页：工作流 / 技能包 / 知识库 三 Tab。
 */
import { FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { PageFeatureGuide } from '../../components/PageFeatureGuide'
import { EntityName } from '../../components/EntityName'
import { IconBook, IconPackage, IconZap } from '../../components/Icons'
import { CodeEditor } from '../../components/CodeEditor'
import { usePerm } from '../../stores/permissions'
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
  const { canAll } = usePerm()
  const catalogWrite = canAll('workflow.write')
  const catalogDelete = canAll('workflow.delete')
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
  const isEditing =
    (tab === 'workflows' && selectedWf !== null) ||
    (tab === 'skills' && selectedSkill !== null) ||
    (tab === 'knowledge' && selectedDoc !== null)
  const rowCount =
    tab === 'workflows' ? workflows.length : tab === 'skills' ? skills.length : docs.length

  return (
    <div className="page">
      <div style={{ marginBottom: '1rem', fontSize: '0.88rem' }}>
        <Link to="/mcp-servers" style={{ color: 'var(--primary)', textDecoration: 'none', display: 'inline-flex', alignItems: 'center', gap: 4 }}>
          <span>← 返回 MCP 服务管理</span>
        </Link>
        <span style={{ margin: '0 8px', color: 'var(--text-muted)' }}>/</span>
        <span style={{ color: 'var(--text-secondary)' }}>workflow-mcp (系统内置)</span>
      </div>
      <div className="page-header">
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <h1>workflow-mcp 服务配置与详情</h1>
            <span className="badge badge-ok">系统内置 Builtin</span>
          </div>
          <p className="muted">系统内置核心能力：统一提供标准 SOP 工作流编排、技能包同步与企业知识库检索</p>
        </div>
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

      {/* 现代化分段选项卡导航 */}
      <div className="tab-nav">
        <button
          type="button"
          className={`tab-btn ${tab === 'workflows' ? 'active' : ''}`}
          onClick={() => {
            setTab('workflows')
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
          <IconZap size={16} />
          <span>工作流</span>
          <span className="tab-count">
            {searchActive && searchSnap ? searchSnap.workflows.length : allWorkflows.length}
          </span>
        </button>
        <button
          type="button"
          className={`tab-btn ${tab === 'skills' ? 'active' : ''}`}
          onClick={() => {
            setTab('skills')
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
          <IconPackage size={16} />
          <span>技能包</span>
          <span className="tab-count">
            {searchActive && searchSnap ? searchSnap.skills.length : allSkills.length}
          </span>
        </button>
        <button
          type="button"
          className={`tab-btn ${tab === 'knowledge' ? 'active' : ''}`}
          onClick={() => {
            setTab('knowledge')
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
          <IconBook size={16} />
          <span>知识库</span>
          <span className="tab-count">
            {searchActive && searchSnap ? searchSnap.docs.length : allDocs.length}
          </span>
        </button>
      </div>

      {/* 单一 panel，避免切 Tab 时整块卸载导致跳动 */}
      <div className="panel wf-panel">
        {!isEditing ? (
          <>
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
                            {catalogDelete ? (
                            <button
                              type="button"
                              className="btn-danger btn-sm"
                              onClick={() => void deleteWorkflow(w.id).then(load).catch((e) => setErr(String(e)))}
                            >
                              删除
                            </button>
                            ) : null}
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
                            {catalogDelete ? (
                            <button
                              type="button"
                              className="btn-danger btn-sm"
                              onClick={() => void deleteSkill(s.id).then(load).catch((e) => setErr(String(e)))}
                            >
                              删除
                            </button>
                            ) : null}
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
                            {catalogDelete ? (
                            <button
                              type="button"
                              className="btn-danger btn-sm"
                              onClick={() => void deleteKnowledge(d.id).then(load).catch((e) => setErr(String(e)))}
                            >
                              删除
                            </button>
                            ) : null}
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
          </>
        ) : (
          <form
            onSubmit={(e) => {
              if (tab === 'workflows') void saveWorkflow(e)
              else if (tab === 'skills') void saveSkill(e)
              else if (tab === 'knowledge') void saveDoc(e)
            }}
            style={{ display: 'flex', flexDirection: 'column', gap: '0.85rem' }}
          >
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                flexWrap: 'wrap',
                gap: '0.75rem',
                paddingBottom: '0.75rem',
                borderBottom: '1px solid #e2e8f0',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem' }}>
                <h3 style={{ margin: 0, fontSize: '1.05rem', fontWeight: 600 }}>
                  {tab === 'workflows'
                    ? (selectedWf === '__new__' ? '新建工作流' : `编辑工作流：${selectedWf}`)
                    : tab === 'skills'
                    ? (selectedSkill === '__new__' ? '新建技能包' : `编辑技能包：${selectedSkill}`)
                    : (selectedDoc === '__new__' ? '新建知识库文档' : `编辑知识库文档：${docPath || selectedDoc}`)}
                </h3>
                <span className="badge badge-ok" style={{ fontSize: '0.75rem' }}>
                  {tab === 'workflows' ? 'YAML 编排定义' : 'Markdown / Frontmatter'}
                </span>
              </div>
              <div className="table-actions" style={{ display: 'flex', gap: '0.5rem' }}>
                {catalogWrite ? (
                <button type="submit" className="btn-sm">
                  保存
                </button>
                ) : null}
                <button
                  type="button"
                  className="btn-ghost btn-sm"
                  onClick={() => {
                    setSelectedWf(null)
                    setSelectedSkill(null)
                    setSelectedDoc(null)
                  }}
                >
                  取消
                </button>
              </div>
            </div>

            {tab === 'knowledge' && (
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                <label style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>
                  文档相对路径:
                </label>
                <input
                  value={docPath}
                  onChange={(e) => setDocPath(e.target.value)}
                  placeholder="例如 notes/cases.md 或 guide.md"
                  style={{ flex: 1, height: 36 }}
                  required
                />
              </div>
            )}

            <CodeEditor
              value={tab === 'workflows' ? wfYaml : tab === 'skills' ? skillMd : docContent}
              onChange={(val) => {
                if (tab === 'workflows') setWfYaml(val)
                else if (tab === 'skills') setSkillMd(val)
                else if (tab === 'knowledge') setDocContent(val)
              }}
              language={tab === 'workflows' ? 'yaml' : 'markdown'}
              height="540px"
            />
          </form>
        )}
      </div>
    </div>
  )
}
