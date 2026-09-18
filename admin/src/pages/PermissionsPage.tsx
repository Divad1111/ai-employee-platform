/**
 * Permission Profiles + Rules 列表与 upsert。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiGet, apiPut } from '../api/client'

type Profile = {
  id: string
  name: string
  description: string
  is_default: boolean
}

type Rule = {
  id: string
  profile_id: string
  action: string
  effect: string
  critical: boolean
  priority: number
  description: string
}

export function PermissionsPage() {
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [rules, setRules] = useState<Rule[]>([])
  const [selected, setSelected] = useState('')
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')

  // Profile 表单
  const [pid, setPid] = useState('')
  const [pname, setPname] = useState('')
  const [pdesc, setPdesc] = useState('')
  const [pisDefault, setPisDefault] = useState(false)

  // Rule 表单
  const [raction, setRaction] = useState('')
  const [reffect, setReffect] = useState('ASK')
  const [rcritical, setRcritical] = useState(false)
  const [rpriority, setRpriority] = useState(100)
  const [rdesc, setRdesc] = useState('')

  async function loadProfiles() {
    const d = await apiGet<{ items: Profile[] }>('/permission/profiles')
    const list = d.items ?? []
    setProfiles(list)
    if (!selected && list.length > 0) {
      setSelected(list[0].id)
    }
  }

  async function loadRules(profileId: string) {
    if (!profileId) {
      setRules([])
      return
    }
    const q = `?profile_id=${encodeURIComponent(profileId)}`
    const d = await apiGet<{ items: Rule[] }>(`/permission/rules${q}`)
    setRules(d.items ?? [])
  }

  useEffect(() => {
    void loadProfiles().catch((e) => setError(String(e)))
  }, [])

  useEffect(() => {
    void loadRules(selected).catch((e) => setError(String(e)))
  }, [selected])

  async function upsertProfile(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      const p = await apiPut<Profile>('/permission/profiles', {
        id: pid,
        name: pname,
        description: pdesc,
        is_default: pisDefault,
      })
      setMsg(`Profile ${p.id} 已保存`)
      setPid('')
      setPname('')
      setPdesc('')
      setPisDefault(false)
      await loadProfiles()
      setSelected(p.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  async function upsertRule(e: FormEvent) {
    e.preventDefault()
    if (!selected) {
      setError('请先选择 Profile')
      return
    }
    setError('')
    setMsg('')
    try {
      const r = await apiPut<Rule>('/permission/rules', {
        profile_id: selected,
        action: raction,
        effect: reffect,
        critical: rcritical,
        priority: rpriority,
        description: rdesc,
      })
      setMsg(`Rule ${r.id} 已保存`)
      setRaction('')
      setRdesc('')
      setRcritical(false)
      setRpriority(100)
      await loadRules(selected)
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    }
  }

  function effectClass(eff: string): string {
    const u = (eff || '').toUpperCase()
    if (u === 'ALLOW') return 'badge badge-ok'
    if (u === 'ASK') return 'badge badge-warn'
    if (u === 'DENY') return 'badge badge-err'
    return 'badge'
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>Permissions</h1>
          <p className="muted">Profile 与 Rule 管理；Effect = ALLOW / ASK / DENY。</p>
        </div>
      </header>
      {error ? <p className="error">{error}</p> : null}
      {msg ? <p className="ok-msg">{msg}</p> : null}

      <div className="panel">
        <h2>Profiles</h2>
        <form className="inline-form" onSubmit={upsertProfile}>
          <input placeholder="ID" value={pid} onChange={(e) => setPid(e.target.value)} required />
          <input placeholder="Name" value={pname} onChange={(e) => setPname(e.target.value)} required />
          <input placeholder="Description" value={pdesc} onChange={(e) => setPdesc(e.target.value)} />
          <label className="check-row">
            <input type="checkbox" checked={pisDefault} onChange={(e) => setPisDefault(e.target.checked)} />
            Default
          </label>
          <button type="submit">Upsert Profile</button>
        </form>
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Default</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {profiles.map((p) => (
              <tr key={p.id} className={selected === p.id ? 'row-active' : undefined}>
                <td className="mono">{p.id}</td>
                <td>{p.name}</td>
                <td>{p.is_default ? 'Y' : ''}</td>
                <td>
                  <button type="button" className="btn-ghost" onClick={() => setSelected(p.id)}>
                    查看 Rules
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="panel">
        <h2>Rules{selected ? ` · ${selected}` : ''}</h2>
        <form className="inline-form" onSubmit={upsertRule}>
          <input
            placeholder="Action（如 git.push）"
            value={raction}
            onChange={(e) => setRaction(e.target.value)}
            required
          />
          <select value={reffect} onChange={(e) => setReffect(e.target.value)}>
            <option value="ALLOW">ALLOW</option>
            <option value="ASK">ASK</option>
            <option value="DENY">DENY</option>
          </select>
          <input
            type="number"
            placeholder="Priority"
            value={rpriority}
            onChange={(e) => setRpriority(Number(e.target.value))}
            style={{ width: 90 }}
          />
          <label className="check-row">
            <input type="checkbox" checked={rcritical} onChange={(e) => setRcritical(e.target.checked)} />
            Critical
          </label>
          <input placeholder="Description" value={rdesc} onChange={(e) => setRdesc(e.target.value)} />
          <button type="submit" disabled={!selected}>
            Upsert Rule
          </button>
        </form>
        <table className="table">
          <thead>
            <tr>
              <th>Action</th>
              <th>Effect</th>
              <th>Critical</th>
              <th>Priority</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {rules.length === 0 ? (
              <tr>
                <td colSpan={5} className="muted">
                  {selected ? '该 Profile 暂无 Rule' : '请选择 Profile'}
                </td>
              </tr>
            ) : (
              rules.map((r) => (
                <tr key={r.id || `${r.profile_id}:${r.action}`}>
                  <td className="mono">{r.action}</td>
                  <td>
                    <span className={effectClass(r.effect)}>{r.effect}</span>
                  </td>
                  <td>{r.critical ? 'Y' : ''}</td>
                  <td>{r.priority}</td>
                  <td className="muted">{r.description || '—'}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}
