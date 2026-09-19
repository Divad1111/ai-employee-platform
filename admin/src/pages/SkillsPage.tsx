/**
 * Skills 技能目录 CRUD + 数字员工赋能绑定。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiDelete, apiGet, apiPatch, apiPost } from '../api/client'
import { IconZap, IconPlus } from '../components/Icons'
import { EntityName } from '../components/EntityName'

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

  const [employees, setEmployees] = useState<Array<{ id: string; name: string }>>([])
  const [bindEmp, setBindEmp] = useState('')
  const [bindSkill, setBindSkill] = useState('')

  async function load() {
    const [d, empData] = await Promise.all([
      apiGet<{ items: Skill[] }>('/skills'),
      apiGet<{ items: Array<{ id: string; name: string }> }>('/employees').catch(() => ({ items: [] })),
    ])
    setItems(d.items ?? [])
    setEmployees(empData.items ?? [])
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
      setMsg('新技能创建成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建技能失败')
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
    setMsg('')
    try {
      await apiPatch(`/skills/${editId}`, {
        name: editName,
        description: editDesc,
        category: editCat,
      })
      setEditId(null)
      setMsg('技能更新成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function onDelete(id: string) {
    if (!confirm('确认删除该技能吗？关联该技能的员工将解除赋能。')) return
    setError('')
    setMsg('')
    try {
      await apiDelete(`/skills/${id}`)
      setMsg('技能已删除')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  async function onBind(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      await apiPost(`/employees/${bindEmp}/skills`, { skill_id: bindSkill })
      setMsg(`已为员工 ${bindEmp} 赋能技能`)
      setBindEmp('')
      setBindSkill('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '技能赋能失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>数字员工技能库 (Skills)</h1>
          <p>标准化专业技能定义 · 赋能数字员工解决特定专业领域的复杂工程任务</p>
        </div>
      </header>

      {error ? <div className="error">{error}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>注册新技能</h2>
            <p>定义专业领域与技能 Prompt 描述模板</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onCreate}>
          <input placeholder="技能名称 (例: 代码重构审查)" value={name} onChange={(e) => setName(e.target.value)} required />
          <input placeholder="分类 (例: 开发规范 / 安全 / 测试)" value={category} onChange={(e) => setCategory(e.target.value)} />
          <input placeholder="技能详述与职责边界" value={desc} onChange={(e) => setDesc(e.target.value)} style={{ flex: 1, minWidth: '220px' }} />
          <button type="submit">
            <IconPlus size={15} />
            <span>注册技能</span>
          </button>
        </form>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>为员工赋能技能</h2>
            <p>建立数字员工与专业技能的关联关系</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onBind}>
          <input
            list="skills-emp-options"
            placeholder="选择员工或输入 ID (例: emp-1)"
            value={bindEmp}
            onChange={(e) => setBindEmp(e.target.value)}
            required
            style={{ width: '240px' }}
          />
          <datalist id="skills-emp-options">
            {employees.map((e) => (
              <option key={e.id} value={e.id}>
                {e.name} ({e.id})
              </option>
            ))}
          </datalist>
          <select value={bindSkill} onChange={(e) => setBindSkill(e.target.value)} required>
            <option value="">请选择要赋能的技能...</option>
            {items.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.category || '通用'})
              </option>
            ))}
          </select>
          <button type="submit">绑定赋能</button>
        </form>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>技能库名录</h2>
            <p>共登记 {items.length} 项标准技能</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>技能名称 / 标识</th>
                <th>专业分类</th>
                <th>技能说明</th>
                <th>管理操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id}>
                  <td>
                    <EntityName
                      name={s.name}
                      id={s.id}
                      icon={<IconZap size={16} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td>
                    <span className="badge badge-ok">{s.category || '通用技能'}</span>
                  </td>
                  <td style={{ maxWidth: '360px', color: 'var(--text-muted)' }}>{s.description}</td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.4rem' }}>
                      <button type="button" className="btn-ghost btn-sm" onClick={() => startEdit(s)}>
                        编辑
                      </button>
                      <button type="button" className="btn-danger btn-sm" onClick={() => void onDelete(s.id)}>
                        删除
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={4} className="empty-tip">暂无登记技能，请在上方注册</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      {editId ? (
        <div className="panel">
          <h2>编辑技能信息 ({editId})</h2>
          <form className="inline-form" onSubmit={onSaveEdit}>
            <input value={editName} onChange={(e) => setEditName(e.target.value)} placeholder="技能名称" required />
            <input value={editCat} onChange={(e) => setEditCat(e.target.value)} placeholder="分类" />
            <input value={editDesc} onChange={(e) => setEditDesc(e.target.value)} placeholder="描述" style={{ flex: 1 }} />
            <button type="submit">保存修改</button>
            <button type="button" className="btn-ghost" onClick={() => setEditId(null)}>取消</button>
          </form>
        </div>
      ) : null}
    </section>
  )
}
