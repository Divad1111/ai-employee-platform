/**
 * 配额管理（§12–13）
 * 角色预设：同一角色统一月度限额。
 * 用户例外：替换该用户的角色基数。用户额外：在基数上再加。
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { apiDelete, apiGet, apiPost } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { SearchableSelect, type SearchOption } from '../components/SearchableSelect'
import { RESOURCE_TYPE_LABEL, RESOURCE_TYPE_OPTIONS, roleDisplayName } from '../lib/rbacLabels'
import { usePerm } from '../stores/permissions'
import { IconRefresh } from '../components/Icons'
import './QuotasPage.css'

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

type MyQuota = {
  period_key?: string
  token_limit: number
  tokens_used: number
  requests_used: number
  remaining: number
  unlimited: boolean
  source_role: string
  role_token_limit: number
  has_exception: boolean
  exception_token_limit: number
  extra_token_limit: number
}

const formatTokens = (value: number) => value.toLocaleString('zh-CN')
const formatLimit = (value: number) => value > 0 ? formatTokens(value) : '不限'

const PRESET_HINTS: Record<string, string> = {
  VIEWER: '只读角色默认月度 Token',
  USER: '普通用户角色默认月度 Token',
  OPERATOR: '操作员角色默认月度 Token',
  ADMIN: '管理员角色默认月度 Token',
}


export function QuotasPage() {
  const { user, ready, can, canAll } = usePerm()
  const canReadAll = canAll('quota.read')
  const canReadPolicies = can('quota.read')
  const canUpdate = canAll('quota.update')
  const [myQuota, setMyQuota] = useState<MyQuota | null>(null)
  const [loading, setLoading] = useState(false)
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
  const [deleteTarget, setDeleteTarget] = useState<Policy | null>(null)
  const [deleting, setDeleting] = useState(false)

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
    if (resourceType === 'USER' || resourceType === 'USER_BONUS') {
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
            { name: 'USER', description: '普通用户' },
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
    if (p.resource_type === 'USER' || p.resource_type === 'USER_BONUS') {
      if (p.resource_id === user?.id) return user.username
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
    setLoading(true)
    try {
      const [mine, q, u, w, e, r] = await Promise.all([
        apiGet<MyQuota>('/quotas/me'),
        canReadPolicies ? apiGet<{ items: Policy[] }>('/quotas') : Promise.resolve({ items: [] as Policy[] }),
        canReadAll && can('user.read') ? apiGet<{ items: UserRow[] }>('/users').catch(() => ({ items: [] as UserRow[] })) : Promise.resolve({ items: [] as UserRow[] }),
        can('workstation.read') ? apiGet<{ items: WSRow[] }>('/workstations').catch(() => ({ items: [] as WSRow[] })) : Promise.resolve({ items: [] as WSRow[] }),
        can('employee.read') ? apiGet<{ items: EmpRow[] }>('/employees').catch(() => ({ items: [] as EmpRow[] })) : Promise.resolve({ items: [] as EmpRow[] }),
        canReadAll && can('role.read') ? apiGet<{ items: RoleItem[] }>('/roles').catch(() => ({ items: [] as RoleItem[] })) : Promise.resolve({ items: [] as RoleItem[] }),
      ])
      setMyQuota(mine)
      setItems(q.items || [])
      setUsers(u.items || [])
      setWorkstations(w.items || [])
      setEmployees(e.items || [])
      setRoles(r.items || [])
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (ready) void load()
  }, [ready, canReadAll, canReadPolicies])

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
      setMsg(
        resourceType === 'ROLE'
          ? '角色预设已保存'
          : resourceType === 'USER_BONUS'
            ? '用户额外配额已保存（将加在角色或例外之上）'
            : resourceType === 'USER'
              ? '用户例外已保存（替换该用户的角色基数）'
              : '策略已保存',
      )
      await load()
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    }
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setErr('')
    setMsg('')
    try {
      const q = new URLSearchParams({
        resource_type: deleteTarget.resource_type,
        resource_id: deleteTarget.resource_id,
        period_type: deleteTarget.period_type || 'MONTHLY',
      })
      await apiDelete(`/quotas?${q.toString()}`)
      setMsg(`已删除 ${RESOURCE_TYPE_LABEL[deleteTarget.resource_type] || deleteTarget.resource_type}：${resolveLabel(deleteTarget)}`)
      setDeleteTarget(null)
      await load()
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    } finally {
      setDeleting(false)
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
        <div>
          <h1>Token / 配额</h1>
          {canReadAll ? <p>
          角色预设是默认月度上限（多角色取最高）。「用户例外」会单独指定该用户的基数并替换角色额度；「用户额外」再加在基数之上。
          有效限额 =（例外或角色）+ 额外。用量仍按人累计。
          </p> : <p>查看当前账号的月度 Token 用量、有效限额及配额来源。</p>}
        </div>
        <button type="button" className="btn-ghost btn-sm" disabled={!ready || loading} onClick={() => void load()}>
          <IconRefresh size={15} />{loading ? '刷新中…' : '刷新'}
        </button>
      </div>

      {err ? <div className="error" style={{ marginBottom: '0.8rem' }}>{err}</div> : null}
      {msg ? <p className="muted">{msg}</p> : null}

      {myQuota ? <section className="panel quota-summary" aria-labelledby="my-quota-title">
        <div className="panel-header">
          <div>
            <h2 id="my-quota-title">我的配额</h2>
            <p>{user?.username} · {(user?.roles || []).map(role => roleDisplayName(role)).join('、')} · {myQuota.period_key || '本月'}</p>
          </div>
        </div>
        <div className="quota-summary-grid">
          <div className="quota-summary-item"><span>有效 Token 限额 / 月</span><strong>{myQuota.unlimited ? '不限' : formatTokens(myQuota.token_limit)}</strong></div>
          <div className="quota-summary-item"><span>本月已用 Token</span><strong>{formatTokens(myQuota.tokens_used)}</strong></div>
          <div className="quota-summary-item"><span>剩余 Token</span><strong>{myQuota.unlimited ? '不限' : formatTokens(myQuota.remaining)}</strong></div>
          <div className="quota-summary-item"><span>本月请求次数</span><strong>{formatTokens(myQuota.requests_used)}</strong></div>
        </div>
        <dl className="quota-source-grid">
          <div><dt>角色默认额度{myQuota.source_role ? ` · ${roleDisplayName(myQuota.source_role)}` : ''}</dt><dd>{myQuota.source_role ? formatLimit(myQuota.role_token_limit) : '未配置'}</dd></div>
          <div><dt>个人例外额度</dt><dd>{myQuota.has_exception ? `${formatLimit(myQuota.exception_token_limit)}（替换角色额度）` : '未设置，使用角色默认额度'}</dd></div>
          <div><dt>个人额外额度</dt><dd>{formatTokens(myQuota.extra_token_limit)}</dd></div>
        </dl>
      </section> : !err ? <p className="muted">正在加载当前账号配额…</p> : null}

      {canReadAll && <div className="panel" style={{ marginBottom: '1rem' }}>
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
              {rolePresets.map((p) => (
                <RolePresetRow
                  key={p.id || p.resource_id}
                  policy={p}
                  label={resolveLabel(p)}
                  hint={PRESET_HINTS[p.resource_id] || ''}
                  canUpdate={canUpdate}
                  onSave={(limit) => void saveRolePreset(p.resource_id, limit)}
                />
              ))}
              {!rolePresets.length && <tr><td colSpan={canUpdate ? 4 : 3} className="empty-tip">暂无角色预设配额</td></tr>}
            </tbody>
          </table>
        </div>
      </div>}

      {canUpdate ? (
      <div className="panel" style={{ marginBottom: '1rem' }}>
        <div className="panel-header">
          <div>
            <h2>新增 / 更新策略</h2>
            <p>角色预设改上面表格。「用户例外」替换角色基数；「用户额外」在基数上相加</p>
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
            <h2>{canReadAll ? '例外、额外与其它策略' : '我的其他配额策略'}</h2>
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
                {canUpdate ? <th className="col-actions">操作</th> : null}
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
                  {canUpdate ? (
                    <td className="col-actions">
                      <button type="button" className="btn-ghost btn-sm" style={{ color: '#dc2626' }} onClick={() => setDeleteTarget(p)}>
                        删除
                      </button>
                    </td>
                  ) : null}
                </tr>
              ))}
              {!otherPolicies.length ? (
                <tr><td colSpan={canUpdate ? 6 : 5} className="empty-tip">{canReadAll ? '暂无个人例外、额外或工作站策略（多数场景只需改角色预设）' : '暂无个人例外、额外或工作站策略，当前账号按上方有效配额执行'}</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      <ConfirmDialog
        open={!!deleteTarget}
        title="删除配额策略"
        description="删除后该条不再参与限额计算。角色预设不会出现在这里，也不能从这里删除。"
        targetLabel={deleteTarget ? `${RESOURCE_TYPE_LABEL[deleteTarget.resource_type] || deleteTarget.resource_type} · ${resolveLabel(deleteTarget)}` : ''}
        targetMeta={deleteTarget ? `限额 ${deleteTarget.token_limit} Token / 月` : ''}
        confirmText={deleting ? '删除中…' : '删除'}
        busy={deleting}
        onConfirm={() => void confirmDelete()}
        onCancel={() => {
          if (!deleting) setDeleteTarget(null)
        }}
      />
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
