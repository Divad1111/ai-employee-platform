/**
 * Knowledge 条目 CRUD + Employee 绑定。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost } from '../api/client'

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

  const [bindEmp, setBindEmp] = useState('')
  const [bindKid, setBindKid] = useState('')

  async function load() {
    const d = await apiGet<{ items: Entry[] }>('/knowledge')
    setItems(d.items ?? [])
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
      setMsg('已创建')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    }
  }

  function startEdit(k: Entry) {
    setEditId(k.id)
    setEditTitle(k.title)
    setEditSummary(k.summary)
    setEditContent(k.content)
    setEditTags((k.tags ?? []).join(', '))
  }

  async function onSaveEdit(e: FormEvent) {
    e.preventDefault()
    if (!editId) return
    setError('')
    try {
      await apiPatch(`/knowledge/${editId}`, {
        title: editTitle,
        summary: editSummary,
        content: editContent,
        tags: parseTags(editTags),
      })
      setEditId(null)
      setMsg('已更新')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新失败')
    }
  }

  async function onDelete(id: string) {
    setError('')
    try {
      await apiDelete(`/knowledge/${id}`)
      setMsg('已删除')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  async function onBind(e: FormEvent) {
    e.preventDefault()
    setError('')
    try {
      await apiPost('/knowledge/bindings', { employee_id: bindEmp, knowledge_id: bindKid })
      setMsg(`已绑定 ${bindKid} → ${bindEmp}`)
      setBindEmp('')
      setBindKid('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '绑定失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>Knowledge</h1>
          <p className="muted">知识条目与 Employee 绑定。</p>
        </div>
      </header>
      {error ? <p className="error">{error}</p> : null}
      {msg ? <p className="ok-msg">{msg}</p> : null}

      <form className="stack-form" onSubmit={onCreate}>
        <div className="inline-form">
          <input placeholder="标题" value={title} onChange={(e) => setTitle(e.target.value)} required />
          <input placeholder="摘要" value={summary} onChange={(e) => setSummary(e.target.value)} />
          <input placeholder="标签（逗号分隔）" value={tags} onChange={(e) => setTags(e.target.value)} />
        </div>
        <textarea
          placeholder="正文"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          rows={3}
        />
        <button type="submit">创建</button>
      </form>

      <div className="panel">
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Title</th>
              <th>Summary</th>
              <th>Tags</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <td colSpan={5} className="muted">
                  暂无 Knowledge
                </td>
              </tr>
            ) : (
              items.map((k) => (
                <tr key={k.id}>
                  <td className="mono">{k.id}</td>
                  <td>{k.title}</td>
                  <td className="muted">{k.summary || '—'}</td>
                  <td>{(k.tags ?? []).join(', ') || '—'}</td>
                  <td>
                    <button type="button" className="btn-ghost" onClick={() => startEdit(k)}>
                      编辑
                    </button>{' '}
                    <button type="button" className="btn-danger" onClick={() => void onDelete(k.id)}>
                      删除
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {editId ? (
        <div className="panel">
          <h2>编辑 {editId}</h2>
          <form className="stack-form" onSubmit={onSaveEdit}>
            <div className="inline-form">
              <input value={editTitle} onChange={(e) => setEditTitle(e.target.value)} required />
              <input value={editSummary} onChange={(e) => setEditSummary(e.target.value)} />
              <input value={editTags} onChange={(e) => setEditTags(e.target.value)} placeholder="标签" />
            </div>
            <textarea value={editContent} onChange={(e) => setEditContent(e.target.value)} rows={3} />
            <div className="inline-form">
              <button type="submit">保存</button>
              <button type="button" className="btn-ghost" onClick={() => setEditId(null)}>
                取消
              </button>
            </div>
          </form>
        </div>
      ) : null}

      <div className="panel">
        <h2>绑定到 Employee</h2>
        <form className="inline-form" onSubmit={onBind}>
          <input placeholder="Employee ID" value={bindEmp} onChange={(e) => setBindEmp(e.target.value)} required />
          <input placeholder="Knowledge ID" value={bindKid} onChange={(e) => setBindKid(e.target.value)} required />
          <button type="submit">绑定</button>
        </form>
      </div>
    </section>
  )
}
