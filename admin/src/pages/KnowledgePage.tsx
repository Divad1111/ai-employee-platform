/**
 * Knowledge 知识库条目 CRUD + 数字员工绑定。
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost } from '../api/client'
import { IconBook, IconPlus } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { PageFeatureGuide } from '../components/PageFeatureGuide'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { SearchableSelect } from '../components/SearchableSelect'

type Entry = {
  id: string
  title: string
  summary: string
  content: string
  tags: string[]
}

export function KnowledgePage() {
  const [items, setItems] = useState<Entry[]>([])
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')

  const [title, setTitle] = useState('')
  const [summary, setSummary] = useState('')
  const [content, setContent] = useState('')
  const [tags, setTags] = useState('')

  const [editId, setEditId] = useState<string | null>(null)
  const [editTitle, setEditTitle] = useState('')
  const [editSummary, setEditSummary] = useState('')
  const [editContent, setEditContent] = useState('')
  const [editTags, setEditTags] = useState('')

  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [bindEmp, setBindEmp] = useState('')
  const [bindKid, setBindKid] = useState('')
  // 删除确认弹窗目标
  const [deleteTarget, setDeleteTarget] = useState<Entry | null>(null)
  const [deleting, setDeleting] = useState(false)

  const empOptions = useMemo(
    () => employees.map((e) => ({ value: e.id, label: e.name, keywords: e.id })),
    [employees],
  )
  const knowledgeOptions = useMemo(
    () => items.map((k) => ({ value: k.id, label: k.title, keywords: k.id })),
    [items],
  )

  async function load() {
    const [d, empData] = await Promise.all([
      apiGet<{ items: Entry[] }>('/knowledge'),
      apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
    ])
    setItems(d.items ?? [])
    setEmployees(empData.items ?? [])
  }

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [])

  function parseTags(s: string): string[] {
    return s
      .split(/[,，\s]+/)
      .map((t) => t.trim())
      .filter(Boolean)
  }

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      await apiPost('/knowledge', {
        title,
        summary,
        content,
        tags: parseTags(tags),
      })
      setTitle('')
      setSummary('')
      setContent('')
      setTags('')
      setMsg('知识库文档创建成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建知识条目失败')
    }
  }

  function startEdit(entry: Entry) {
    setEditId(entry.id)
    setEditTitle(entry.title)
    setEditSummary(entry.summary)
    setEditContent(entry.content)
    setEditTags((entry.tags || []).join(', '))
  }

  async function onSaveEdit(e: FormEvent) {
    e.preventDefault()
    if (!editId) return
    setError('')
    setMsg('')
    try {
      await apiPatch(`/knowledge/${editId}`, {
        title: editTitle,
        summary: editSummary,
        content: editContent,
        tags: parseTags(editTags),
      })
      setEditId(null)
      setMsg('文档更新成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  function requestDelete(entry: Entry) {
    setDeleteTarget(entry)
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    setDeleting(true)
    setError('')
    setMsg('')
    try {
      await apiDelete(`/knowledge/${deleteTarget.id}`)
      setDeleteTarget(null)
      setMsg('知识文档已删除')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败')
    } finally {
      setDeleting(false)
    }
  }

  async function onBind(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      await apiPost(`/employees/${bindEmp}/knowledge`, { knowledge_id: bindKid })
      setMsg(`已为员工 ${bindEmp} 关联知识库文档`)
      setBindEmp('')
      setBindKid('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '关联失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>企业知识库管理 (Knowledge)</h1>
          <p>为数字员工注入业务规范、架构标准与工程知识背景 · 支持标签检索与按需挂载</p>
        </div>
      </header>

      <PageFeatureGuide
        title="企业领域知识库注入与上下文增强指引"
        summary="管理公司研发规范、接口字典、架构拓扑与最佳实践，在数字员工执行具体 Prompt 时作为领域外脑提供语义上下文支撑。"
        steps={[
          {
            step: '1',
            title: '规范条目沉淀 (Markdown)',
            desc: '录入工程设计规约、核心数据库字段释义、运维发布 SOP 等长效文本。',
            tag: '知识资产',
          },
          {
            step: '2',
            title: '多维标签分类与索引 (Tags)',
            desc: '通过打标分类（如 后端审核、JIRA联动、发布规范），实现知识快速索引。',
            tag: '分类检索',
          },
          {
            step: '3',
            title: '员工个性化挂载 (Binding)',
            desc: '将特定知识库按需挂载至指定数字员工，任务启动时自动加载至智能体上下文提示词中。',
            tag: '上下文增强',
          },
        ]}
      />

      {error ? <div className="error">{error}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>录入新知识文档</h2>
            <p>录入标题、概要摘要、主体 Markdown 内容与检索标签</p>
          </div>
        </div>
        <form className="stack-form" onSubmit={onCreate}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <label>
              文档标题
              <input placeholder="例: Go 代码整洁架构指南" value={title} onChange={(e) => setTitle(e.target.value)} required />
            </label>
            <label>
              检索标签 (逗号分隔)
              <input placeholder="例: golang, architecture, style-guide" value={tags} onChange={(e) => setTags(e.target.value)} />
            </label>
          </div>
          <label>
            核心摘要说明
            <input placeholder="简述该文档覆盖的核心技术点与参考背景" value={summary} onChange={(e) => setSummary(e.target.value)} />
          </label>
          <label>
            知识正文内容 (Markdown)
            <textarea
              placeholder="编写具体的架构规范、代码模板或业务定义..."
              value={content}
              onChange={(e) => setContent(e.target.value)}
              rows={4}
              required
            />
          </label>
          <div>
            <button type="submit">
              <IconPlus size={15} />
              <span>保存至知识库</span>
            </button>
          </div>
        </form>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>为员工关联挂载知识</h2>
            <p>将知识库上下文按需附加到指定数字员工的交互环境中</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onBind}>
          <SearchableSelect
            value={bindEmp}
            onChange={setBindEmp}
            options={empOptions}
            placeholder="选择或搜索员工…"
            allowCustom
            required
            style={{ minWidth: 220 }}
          />
          <SearchableSelect
            value={bindKid}
            onChange={setBindKid}
            options={knowledgeOptions}
            placeholder="请选择挂载的知识文档…"
            required
            style={{ minWidth: 240 }}
          />
          <button type="submit">关联挂载</button>
        </form>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>知识库文档列表</h2>
            <p>共入库 {items.length} 篇文档规范</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>文档名称 / 标题</th>
                <th>概要说明</th>
                <th>关联标签</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((k) => (
                <tr key={k.id}>
                  <td>
                    <EntityName
                      name={k.title}
                      id={k.id}
                      icon={<IconBook size={16} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td style={{ maxWidth: '300px', color: 'var(--text-muted)' }}>{k.summary}</td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.3rem', flexWrap: 'wrap' }}>
                      {(k.tags || []).map((t) => (
                        <span key={t} className="badge">
                          {t}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.4rem' }}>
                      <button type="button" className="btn-ghost btn-sm" onClick={() => startEdit(k)}>
                        编辑
                      </button>
                      <button type="button" className="btn-danger btn-sm" onClick={() => requestDelete(k)}>
                        删除
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={4} className="empty-tip">暂无知识库文档，请在上方录入</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      {editId ? (
        <div className="panel">
          <h2>编辑知识文档 ({editId})</h2>
          <form className="stack-form" onSubmit={onSaveEdit}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
              <label>
                文档标题
                <input value={editTitle} onChange={(e) => setEditTitle(e.target.value)} required />
              </label>
              <label>
                标签
                <input value={editTags} onChange={(e) => setEditTags(e.target.value)} />
              </label>
            </div>
            <label>
              摘要
              <input value={editSummary} onChange={(e) => setEditSummary(e.target.value)} />
            </label>
            <label>
              正文 (Markdown)
              <textarea value={editContent} onChange={(e) => setEditContent(e.target.value)} rows={5} required />
            </label>
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              <button type="submit">保存更新</button>
              <button type="button" className="btn-ghost" onClick={() => setEditId(null)}>取消</button>
            </div>
          </form>
        </div>
      ) : null}

      <ConfirmDialog
        open={!!deleteTarget}
        title="确认删除知识文档"
        description="删除后无法恢复，已挂载到员工的关联也将失效。"
        targetLabel={deleteTarget?.title}
        targetMeta={deleteTarget?.id}
        confirmText="确认删除"
        busy={deleting}
        onCancel={() => !deleting && setDeleteTarget(null)}
        onConfirm={() => void confirmDelete()}
      />
    </section>
  )
}
