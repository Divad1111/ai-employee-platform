/**
 * Permission 权限策略配置与规则清单。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiGet, apiPut } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { IconShield, IconPlus } from '../components/Icons'
import { PageFeatureGuide } from '../components/PageFeatureGuide'

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

  async function onSaveProfile(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      await apiPut(`/permission/profiles/${pid}`, {
        name: pname,
        description: pdesc,
        is_default: pisDefault,
      })
      setMsg(`安全策略模板 ${pid} 保存成功`)
      setPid('')
      setPname('')
      setPdesc('')
      setPisDefault(false)
      await loadProfiles()
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存策略模板失败')
    }
  }

  async function onAddRule(e: FormEvent) {
    e.preventDefault()
    if (!selected) return
    setError('')
    setMsg('')
    try {
      await apiPut('/permission/rules', {
        profile_id: selected,
        action: raction,
        effect: reffect,
        critical: rcritical,
        priority: Number(rpriority),
        description: rdesc,
      })
      setMsg(`权限控制规则已生效`)
      setRaction('')
      setRdesc('')
      await loadRules(selected)
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存控制规则失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>权限策略引擎配置 (Permissions)</h1>
          <p>基于 RBAC + 细粒度操作策略 · 支持 ALLOW（放行）、DENY（阻断）、ASK（触发人工审批）</p>
        </div>
      </header>

      <PageFeatureGuide
        title="权限策略沙箱与三态风控拦截指引"
        summary="平台内置细粒度权限判定网关，严格拦截危险 Shell 命令、越权文件改动与网络违规外联。"
        steps={[
          {
            step: '1',
            title: '策略模板 Profile 划分',
            desc: '针对不同岗位创建模板（如 测试助理宽松模版、生产只读模版），支持设为全局默认。',
            tag: '模板定义',
          },
          {
            step: '2',
            title: '三态动作控制 (Effect)',
            desc: 'ALLOW（直接放行）、DENY（直接阻断抛错）、ASK（阻断并提交人工审批单）。',
            tag: '三态风控',
          },
          {
            step: '3',
            title: '优先级 Priority 与高危标记',
            desc: '数值越小优先级越高；勾选 Critical 后触发审批将强制校验 TOTP 6 位动态口令。',
            tag: '多级防护',
          },
        ]}
      />

      {error ? <div className="error">{error}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>创建 / 更新安全策略模板 (Profile)</h2>
            <p>为不同职能的数字员工定制专属的权限沙箱边界</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onSaveProfile}>
          <input placeholder="模板标识 (例: dev-strict)" value={pid} onChange={(e) => setPid(e.target.value)} required />
          <input placeholder="模板名称 (例: 严格开发权限)" value={pname} onChange={(e) => setPname(e.target.value)} required />
          <input placeholder="描述说明" value={pdesc} onChange={(e) => setPdesc(e.target.value)} style={{ flex: 1 }} />
          <label className="check-row">
            <input type="checkbox" checked={pisDefault} onChange={(e) => setPisDefault(e.target.checked)} />
            <span>设为全局默认模板</span>
          </label>
          <button type="submit">
            <IconPlus size={15} />
            <span>保存策略模板</span>
          </button>
        </form>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>当前配置的安全模板</h2>
            <p>点击选择模板即可维护下属的规则清单</p>
          </div>
        </div>
        <div style={{ display: 'flex', gap: '0.65rem', flexWrap: 'wrap' }}>
          {profiles.map((p) => (
            <button
              key={p.id}
              type="button"
              className={selected === p.id ? '' : 'btn-ghost'}
              onClick={() => setSelected(p.id)}
            >
              <IconShield size={15} />
              <span>{p.name} ({p.id})</span>
              {p.is_default ? <span className="badge badge-ok" style={{ marginLeft: '4px' }}>默认</span> : null}
            </button>
          ))}
        </div>
      </div>

      {selected ? (
        <div className="panel">
          <div className="panel-header">
            <div>
              <h2>维护规则清单 (模板: {selected})</h2>
              <p>动作命中规则后，按优先级数值进行最终判定</p>
            </div>
          </div>

          <form className="inline-form" onSubmit={onAddRule}>
            <input
              placeholder="动作 Action (例: command.exec 或 file.write)"
              value={raction}
              onChange={(e) => setRaction(e.target.value)}
              required
              style={{ width: '220px' }}
            />
            <select value={reffect} onChange={(e) => setReffect(e.target.value)}>
              <option value="ALLOW">ALLOW (允许放行)</option>
              <option value="ASK">ASK (人工审核)</option>
              <option value="DENY">DENY (强行阻断)</option>
            </select>
            <input
              type="number"
              placeholder="优先级 (数值越大越先判定)"
              value={rpriority}
              onChange={(e) => setRpriority(Number(e.target.value))}
              style={{ width: '130px' }}
            />
            <input
              placeholder="规则原因或风控提示"
              value={rdesc}
              onChange={(e) => setRdesc(e.target.value)}
              style={{ flex: 1, minWidth: '180px' }}
            />
            <label className="check-row">
              <input type="checkbox" checked={rcritical} onChange={(e) => setRcritical(e.target.checked)} />
              <span>标记为高风险动作</span>
            </label>
            <button type="submit">保存规则</button>
          </form>

          <div className="table-wrapper" style={{ marginTop: '1rem' }}>
            <table className="table">
              <thead>
                <tr>
                  <th>操作动作 (Action)</th>
                  <th>策略判定 (Effect)</th>
                  <th>高风险标记</th>
                  <th>优先级</th>
                  <th>风控说明</th>
                </tr>
              </thead>
              <tbody>
                {rules.map((r) => (
                  <tr key={r.id}>
                    <td><span className="mono">{r.action}</span></td>
                    <td><StatusBadge status={r.effect} /></td>
                    <td>
                      {r.critical ? <StatusBadge status="CRITICAL" /> : <span style={{ color: 'var(--text-muted)' }}>普通</span>}
                    </td>
                    <td><span className="badge">{r.priority}</span></td>
                    <td style={{ color: 'var(--text-muted)' }}>{r.description || '—'}</td>
                  </tr>
                ))}
                {rules.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="empty-tip">该模板下尚未添加任何权限规则</td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </div>
      ) : null}
    </section>
  )
}
