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

export function WorkflowMcpPage() {
  const [tab, setTab] = useState<Tab>('workflows')
  const [q, setQ] = useState('')
  const [err, setErr] = useState('')
  const [ok, setOk] = useState('')
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [skills, setSkills] = useState<SkillPackage[]>([])
  const [docs, setDocs] = useState<KnowledgeDoc[]>([])
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

  const load = useCallback(async () => {
    setErr('')
    try {
      const [w, s, k] = await Promise.all([
        listWorkflows(),
        listSkills(),
        listKnowledge(nsFilter),
      ])
      setWorkflows(w.items || [])
      setSkills(s.items || [])
      setDocs(k.items || [])
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }, [nsFilter])

  useEffect(() => {
    void load()
  }, [load])

  async function onSearch(e: FormEvent) {
    e.preventDefault()
    if (!q.trim()) {
      void load()
      return
    }
    try {
      const res = await unifiedSearch(q.trim())
      setOk(`搜索完成：工作流 ${(res.workflows || []).length} / 技能 ${(res.skills || []).length} / 知识 ${(res.knowledge || []).length}`)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
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
        <input placeholder="统一搜索工作流 / 技能 / 知识..." value={q} onChange={(e) => setQ(e.target.value)} style={{ flex: 1 }} />
        <button type="submit">搜索</button>
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
            void reindexKnowledge().then((r) => setOk(`已重建 ${r.reindexed} 篇文档索引`)).catch((e) => setErr(String(e)))
          }}
        >
          重建知识索引
        </button>
      </form>

      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem' }}>
        {(
          [
            ['workflows', '工作流'],
            ['skills', '技能包'],
            ['knowledge', '知识库'],
          ] as const
        ).map(([k, label]) => (
          <button key={k} type="button" className={tab === k ? 'btn-sm' : 'btn-ghost btn-sm'} onClick={() => setTab(k)}>
            {label}
          </button>
        ))}
      </div>

      {tab === 'workflows' && (
        <div className="panel">
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
            <h3>工作流列表</h3>
            <button
              type="button"
              className="btn-sm"
              onClick={() => {
                setSelectedWf('__new__')
                setWfYaml('id: my-workflow\nname: 新工作流\nversion: 1.0.0\ndescription: \nskills: []\nknowledge: []\nsteps:\n  - id: step1\n    description: 第一步\n')
              }}
            >
              新建
            </button>
          </div>
          <div className="table-wrapper">
            <table className="table">
              <thead>
                <tr>
                  <th>名称 / ID</th>
                  <th>版本</th>
                  <th>技能引用</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {workflows.map((w) => (
                  <tr key={w.id}>
                    <td>
                      <EntityName name={w.name} id={w.id} icon={<IconZap size={16} />} />
                      <div className="muted" style={{ fontSize: 12 }}>{w.description}</div>
                    </td>
                    <td>{w.version}</td>
                    <td>{(w.skills || []).join(', ') || '—'}</td>
                    <td>
                      <button type="button" className="btn-ghost btn-sm" onClick={() => void openWorkflow(w.id)}>编辑</button>
                      <button
                        type="button"
                        className="btn-danger btn-sm"
                        onClick={() => void deleteWorkflow(w.id).then(load).catch((e) => setErr(String(e)))}
                      >
                        删除
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {selectedWf && (
            <form className="stack-form" onSubmit={saveWorkflow} style={{ marginTop: '1rem' }}>
              <label>
                YAML
                <textarea rows={16} value={wfYaml} onChange={(e) => setWfYaml(e.target.value)} style={{ fontFamily: 'monospace' }} />
              </label>
              <div style={{ display: 'flex', gap: '0.5rem' }}>
                <button type="submit">保存</button>
                <button type="button" className="btn-ghost" onClick={() => setSelectedWf(null)}>取消</button>
              </div>
            </form>
          )}
        </div>
      )}

      {tab === 'skills' && (
        <div className="panel">
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
            <h3>技能包列表</h3>
            <button
              type="button"
              className="btn-sm"
              onClick={() => {
                setSelectedSkill('__new__')
                setSkillMd('---\nname: my-skill\nid: my.skill\ntitle: 我的技能\nversion: 1.0.0\ndescription: \ndisable-model-invocation: true\n---\n# 我的技能\n\n说明...\n')
              }}
            >
              新建
            </button>
          </div>
          <div className="table-wrapper">
            <table className="table">
              <thead>
                <tr>
                  <th>名称 / ID</th>
                  <th>cursor_name</th>
                  <th>版本</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {skills.map((s) => (
                  <tr key={s.id}>
                    <td>
                      <EntityName name={s.name || s.id} id={s.id} icon={<IconZap size={16} />} />
                    </td>
                    <td>{s.cursor_name}</td>
                    <td>{s.version}</td>
                    <td>
                      <button type="button" className="btn-ghost btn-sm" onClick={() => void openSkill(s.id)}>编辑</button>
                      <button type="button" className="btn-danger btn-sm" onClick={() => void deleteSkill(s.id).then(load).catch((e) => setErr(String(e)))}>删除</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {selectedSkill && (
            <form className="stack-form" onSubmit={saveSkill} style={{ marginTop: '1rem' }}>
              <label>
                SKILL.md
                <textarea rows={16} value={skillMd} onChange={(e) => setSkillMd(e.target.value)} style={{ fontFamily: 'monospace' }} />
              </label>
              <div style={{ display: 'flex', gap: '0.5rem' }}>
                <button type="submit">保存</button>
                <button type="button" className="btn-ghost" onClick={() => setSelectedSkill(null)}>取消</button>
              </div>
            </form>
          )}
        </div>
      )}

      {tab === 'knowledge' && (
        <div className="panel">
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.75rem', gap: '0.5rem' }}>
            <h3>知识文档</h3>
            <input placeholder="namespace 过滤（如 cases）" value={nsFilter} onChange={(e) => setNsFilter(e.target.value)} />
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
          </div>
          <div className="table-wrapper">
            <table className="table">
              <thead>
                <tr>
                  <th>标题 / ID</th>
                  <th>命名空间</th>
                  <th>路径</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {docs.map((d) => (
                  <tr key={d.id}>
                    <td>
                      <EntityName name={d.title} id={d.id} icon={<IconBook size={16} />} />
                    </td>
                    <td>{d.namespace}</td>
                    <td className="muted">{d.path}</td>
                    <td>
                      <button type="button" className="btn-ghost btn-sm" onClick={() => void openDoc(d.id)}>编辑</button>
                      <button type="button" className="btn-danger btn-sm" onClick={() => void deleteKnowledge(d.id).then(load).catch((e) => setErr(String(e)))}>删除</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {selectedDoc && (
            <form className="stack-form" onSubmit={saveDoc} style={{ marginTop: '1rem' }}>
              <label>
                路径
                <input value={docPath} onChange={(e) => setDocPath(e.target.value)} required />
              </label>
              <label>
                Markdown
                <textarea rows={14} value={docContent} onChange={(e) => setDocContent(e.target.value)} style={{ fontFamily: 'monospace' }} />
              </label>
              <div style={{ display: 'flex', gap: '0.5rem' }}>
                <button type="submit">保存</button>
                <button type="button" className="btn-ghost" onClick={() => setSelectedDoc(null)}>取消</button>
              </div>
            </form>
          )}
        </div>
      )}
    </div>
  )
}
