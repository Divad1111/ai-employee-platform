/**
 * 配额管理（§12–13）
 * 角色预设：同一角色统一月度限额；个人 USER 策略优先覆盖。
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { apiGet, apiPost } from '../api/client'
import { SearchableSelect, type SearchOption } from '../components/SearchableSelect'
import { RESOURCE_TYPE_LABEL, RESOURCE_TYPE_OPTIONS, roleDisplayName } from '../lib/rbacLabels'
import { usePerm } from '../stores/permissions'

type Policy = {
  id: string
  resource_type: string
  resource_id: string
  period_type: string
  token_limit: number
  request_limit: number
  concurrency_limit: number
  enabled: boolean
}

type UserRow = { id: string; username: string; display_name: string }
type WSRow = { id: string; name?: string; hostname?: string }
type EmpRow = { id: string; name: string }
type RoleItem = { name: string; description: string }

const PRESET_HINTS: Record<string, string> = {
  VIEWER: '只读角色默认月度 Token',
  OPERATOR: '操作员角色默认月度 Token',
  ADMIN: '管理员角色默认月度 Token',
}

export function QuotasPage() {
  const { can } = usePerm()
  const canUpdate = can('quota.update')
  const [items, setItems] = useState<Policy[]>([])
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [resourceType, setResourceType] = useState('ROLE')
  const [resourceID, setResourceID] = useState('VIEWER')
  const [tokenLimit, setTokenLimit] = useState(1000000)
  const [users, setUsers] = useState<UserRow[]>([])
  const [workstations, setWorkstations] = useState<WSRow[]>([])
  const [employees, setEmployees] = useState<EmpRow[]>([])
  const [roles, setRoles] = useState<RoleItem[]>([])

  const typeOptions = useMemo(
    () => RESOURCE_TYPE_OPTIONS.map((o) => ({ value: o.value, label: o.label })),
    [],
  )

  const rolePresets = useMemo(
    () => items.filter((p) => p.resource_type === 'ROLE'),
    [items],
  )
  const otherPolicies = useMemo(
    () => items.filter((p) => p.resource_type !== 'ROLE'),
    [items],
  )

  const resourceOptions: SearchOption[] = useMemo(() => {
    if (resourceType === 'USER') {
      return users.map((u) => ({
        value: u.id,
        label: `${u.display_name || u.username}（${u.username}）`,
        keywords: `${u.id} ${u.username} ${u.display_name}`,
      }))
    }
    if (resourceType === 'WORKSTATION') {
      return workstations.map((w) => ({
        value: w.id,
        label: w.name || w.hostname || w.id,
        keywords: `${w.id} ${w.name || ''} ${w.hostname || ''}`,
      }))
    }
    if (resourceType === 'ROLE') {
      const src = roles.length
        ? roles
        : [
            { name: 'VIEWER', description: '只读' },
            { name: 'OPERATOR', description: '操作员' },
            { name: 'ADMIN', description: '管理员' },
          ]
      return src
        .filter((r) => r.name !== 'SUPER_ADMIN')
        .map((r) => ({
          value: r.name,
          label: roleDisplayName(r.name, r.description),
          keywords: `${r.name} ${r.description || ''}`,
        }))
    }
    return employees.map((e) => ({
      value: e.id,
      label: e.name || e.id,
      keywords: `${e.id} ${e.name}`,
    }))
  }, [resourceType, users, workstations, employees, roles])

  const resolveLabel = (p: Policy) => {
    if (p.resource_type === 'ROLE') {
      const r = roles.find((x) => x.name === p.resource_id)
      return roleDisplayName(p.resource_id, r?.description)
    }
    if (p.resource_type === 'USER') {
      const u = users.find((x) => x.id === p.resource_id)
      return u ? `${u.display_name || u.username}` : p.resource_id
    }
    if (p.resource_type === 'WORKSTATION') {
      const w = workstations.find((x) => x.id === p.resource_id)
      return w?.name || w?.hostname || p.resource_id
    }
    const e = employees.find((x) => x.id === p.resource_id)
    return e?.name || p.resource_id
  }

  const load = async () => {
    setErr('')
    try {
      const [q, u, w, e, r] = await Promise.all([
        apiGet<{ items: Policy[] }>('/quotas'),
        apiGet<{ items: UserRow[] }>('/users').catch(() => ({ items: [] as UserRow[] })),
        apiGet<{ items: WSRow[] }>('/workstations').catch(() => ({ items: [] as WSRow[] })),
        apiGet<{ items: EmpRow[] }>('/employees').catch(() => ({ items: [] as EmpRow[] })),
        apiGet<{ items: RoleItem[] }>('/roles').catch(() => ({ items: [] as RoleItem[] })),
      ])
      setItems(q.items || [])
      setUsers(u.items || [])
      setWorkstations(w.items || [])
      setEmployees(e.items || [])
      setRoles(r.items || [])
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const onSave = async (e: FormEvent) => {
    e.preventDefault()
    setMsg('')
    try {
      await apiPost('/quotas', {
        resource_type: resourceType,
        resource_id: resourceID,
        period_type: 'MONTHLY',
        token_limit: tokenLimit,
        request_limit: 0,
        concurrency_limit: 0,
        enabled: true,
      })
      setMsg(resourceType === 'ROLE' ? '角色预设已保存' : '策略已保存')
      await load()
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  const saveRolePreset = async (roleName: string, limit: number) => {
    setErr('')
    setMsg('')
    try {
      await apiPost('/quotas', {
        resource_type: 'ROLE',
        resource_id: roleName,
        period_type: 'MONTHLY',
        token_limit: limit,
        request_limit: 0,
        concurrency_limit: 0,
        enabled: true,
      })
      setMsg(`已更新 ${roleDisplayName(roleName)} 角色预设`)
      await load()
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <h1>Token / 配额</h1>
        <p>
          优先按「角色预设」统一限额；仅当个别用户需要例外时再配置个人策略。
          解析顺序：个人 USER &gt; 角色 ROLE（多角色取最高限额）&gt; 不限。
        </p>
      </div>

      {err ? <div className="error" style={{ marginBottom: '0.8rem' }}>{err}</div> : null}
      {msg ? <p className="muted">{msg}</p> : null}

      <div className="panel" style={{ marginBottom: '1rem' }}>
        <div className="panel-header">
          <div>
            <h2>角色预设配额</h2>
            <p>同一角色下所有用户共享同一月度上限（用量仍按人累计，不共用额度池）</p>
          </div>
        </div>
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>角色</th>
                <th>说明</th>
                <th>Token 限额 / 月</th>
                {canUpdate ? <th className="col-actions">操作</th> : null}
              </tr>
            </thead>
            <tbody>
              {(rolePresets.length
                ? rolePresets
                : [
                    { id: 'p-v', resource_type: 'ROLE', resource_id: 'VIEWER', period_type: 'MONTHLY', token_limit: 1000000, request_limit: 0, concurrency_limit: 0, enabled: true },
                    { id: 'p-o', resource_type: 'ROLE', resource_id: 'OPERATOR', period_type: 'MONTHLY', token_limit: 5000000, request_limit: 0, concurrency_limit: 0, enabled: true },
                    { id: 'p-a', resource_type: 'ROLE', resource_id: 'ADMIN', period_type: 'MONTHLY', token_limit: 50000000, request_limit: 0, concurrency_limit: 0, enabled: true },
                  ]
              ).map((p) => (
                <RolePresetRow
                  key={p.id || p.resource_id}
                  policy={p}
                  label={resolveLabel(p)}
                  hint={PRESET_HINTS[p.resource_id] || ''}
                  canUpdate={canUpdate}
                  onSave={(limit) => void saveRolePreset(p.resource_id, limit)}
                />
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {canUpdate ? (
      <div className="panel" style={{ marginBottom: '1rem' }}>
        <div className="panel-header">
          <div>
            <h2>新增 / 更新策略</h2>
            <p>角色预设改上面表格即可；此处用于例外个人 / 工作站 / 数字员工，或新增自定义角色预设</p>
          </div>
        </div>
        <form className="stack-form" onSubmit={onSave}>
          <label>
            <span className="field-caption">资源类型</span>
            <SearchableSelect
              value={resourceType}
              onChange={(v) => {
                setResourceType(v)
                setResourceID(v === 'ROLE' ? 'VIEWER' : '')
              }}
              options={typeOptions}
              placeholder="选择资源类型…"
              required
            />
          </label>
          <label>
            <span className="field-caption">资源</span>
            <SearchableSelect
              value={resourceID}
              onChange={setResourceID}
              options={resourceOptions}
              placeholder="输入筛选或粘贴 ID…"
              allowCustom={resourceType !== 'ROLE'}
              required
            />
          </label>
          <label>
            <span className="field-caption">Token 限额 / 月</span>
            <input
              type="number"
              value={tokenLimit}
              onChange={(e) => setTokenLimit(Number(e.target.value))}
              min={0}
            />
          </label>
          <div className="form-actions">
            <button type="submit" className="btn-success btn-sm">保存策略</button>
          </div>
        </form>
      </div>
      ) : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>例外与其它策略</h2>
            <p>共 {otherPolicies.length} 条（不含角色预设）</p>
          </div>
        </div>
        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>类型</th>
                <th>资源</th>
                <th>周期</th>
                <th>Token 限额</th>
                <th>启用</th>
              </tr>
            </thead>
            <tbody>
              {otherPolicies.map((p) => (
                <tr key={p.id}>
                  <td>{RESOURCE_TYPE_LABEL[p.resource_type] || p.resource_type}</td>
                  <td>
                    <div style={{ fontWeight: 600 }}>{resolveLabel(p)}</div>
                    <code className="muted" style={{ fontSize: '0.75rem' }}>{p.resource_id}</code>
                  </td>
                  <td>{p.period_type === 'MONTHLY' ? '每月' : p.period_type}</td>
                  <td>{p.token_limit}</td>
                  <td>{p.enabled ? '是' : '否'}</td>
                </tr>
              ))}
              {!otherPolicies.length ? (
                <tr><td colSpan={5} className="empty-tip">暂无个人/工作站例外策略（多数场景只需改角色预设）</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

function RolePresetRow({
  policy,
  label,
  hint,
  canUpdate,
  onSave,
}: {
  policy: Policy
  label: string
  hint: string
  canUpdate: boolean
  onSave: (limit: number) => void
}) {
  const [limit, setLimit] = useState(policy.token_limit)
  useEffect(() => {
    setLimit(policy.token_limit)
  }, [policy.token_limit])

  return (
    <tr>
      <td>
        <div style={{ fontWeight: 600 }}>{label}</div>
        <code className="muted" style={{ fontSize: '0.75rem' }}>{policy.resource_id}</code>
      </td>
      <td className="muted">{hint || '—'}</td>
      <td>
        {canUpdate ? (
          <input
            type="number"
            value={limit}
            min={0}
            onChange={(e) => setLimit(Number(e.target.value))}
            style={{ width: '9rem' }}
          />
        ) : (
          policy.token_limit
        )}
      </td>
      {canUpdate ? (
        <td className="col-actions">
          <button
            type="button"
            className="btn-success btn-sm"
            disabled={limit === policy.token_limit}
            onClick={() => onSave(limit)}
          >
            保存
          </button>
        </td>
      ) : null}
    </tr>
  )
}
