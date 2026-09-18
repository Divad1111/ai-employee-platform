/**
 * Skills 目录 CRUD + Employee 绑定。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost } from '../api/client'

type Skill = {
  id: string
  name: string
  description: string
  category: string
}

export function SkillsPage() {
  const [items, setItems] = useState<Skill[]>([])
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')

  const [name, setName] = useState('')
  const [desc, setDesc] = useState('')
  const [category, setCategory] = useState('')

  const [editId, setEditId] = useState<string | null>(null)
  const [editName, setEditName] = useState('')
  const [editDesc, setEditDesc] = useState('')
  const [editCat, setEditCat] = useState('')

  const [bindEmp, setBindEmp] = useState('')
  const [bindSkill, setBindSkill] = useState('')

  async function load() {
    const d = await apiGet<{ items: Skill[] }>('/skills')
    setItems(d.items ?? [])
  }

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      await apiPost('/skills', { name, description: desc, category })
      setName('')
      setDesc('')
      setCategory('')
      setMsg('已创建')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    }
  }

  function startEdit(s: Skill) {
    setEditId(s.id)
    setEditName(s.name)
    setEditDesc(s.description)
    setEditCat(s.category)
  }

  async function onSaveEdit(e: FormEvent) {
    e.preventDefault()
    if (!editId) return
    setError('')
    try {
      await apiPatch(`/skills/${editId}`, {
        name: editName,
        description: editDesc,
        category: editCat,
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
      await apiDelete(`/skills/${id}`)
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
      await apiPost('/skills/bindings', { employee_id: bindEmp, skill_id: bindSkill })
      setMsg(`已绑定 ${bindSkill} → ${bindEmp}`)
      setBindEmp('')
      setBindSkill('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '绑定失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>Skills</h1>
          <p className="muted">技能目录与 Employee 绑定。</p>
        </div>
      </header>
      {error ? <p className="error">{error}</p> : null}
      {msg ? <p className="ok-msg">{msg}</p> : null}

      <form className="inline-form" onSubmit={onCreate}>
        <input placeholder="名称" value={name} onChange={(e) => setName(e.target.value)} required />
        <input placeholder="描述" value={desc} onChange={(e) => setDesc(e.target.value)} />
        <input placeholder="分类" value={category} onChange={(e) => setCategory(e.target.value)} />
        <button type="submit">创建</button>
      </form>

      <div className="panel">
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Category</th>
              <th>Description</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <td colSpan={5} className="muted">
                  暂无 Skill
                </td>
              </tr>
            ) : (
              items.map((s) => (
                <tr key={s.id}>
                  <td className="mono">{s.id}</td>
                  <td>{s.name}</td>
                  <td>{s.category || '—'}</td>
                  <td className="muted">{s.description || '—'}</td>
                  <td>
                    <button type="button" className="btn-ghost" onClick={() => startEdit(s)}>
                      编辑
                    </button>{' '}
                    <button type="button" className="btn-danger" onClick={() => void onDelete(s.id)}>
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
          <form className="inline-form" onSubmit={onSaveEdit}>
            <input value={editName} onChange={(e) => setEditName(e.target.value)} required />
            <input value={editDesc} onChange={(e) => setEditDesc(e.target.value)} />
            <input value={editCat} onChange={(e) => setEditCat(e.target.value)} />
            <button type="submit">保存</button>
            <button type="button" className="btn-ghost" onClick={() => setEditId(null)}>
              取消
            </button>
          </form>
        </div>
      ) : null}

      <div className="panel">
        <h2>绑定到 Employee</h2>
        <form className="inline-form" onSubmit={onBind}>
          <input placeholder="Employee ID" value={bindEmp} onChange={(e) => setBindEmp(e.target.value)} required />
          <input placeholder="Skill ID" value={bindSkill} onChange={(e) => setBindSkill(e.target.value)} required />
          <button type="submit">绑定</button>
        </form>
      </div>
    </section>
  )
}
