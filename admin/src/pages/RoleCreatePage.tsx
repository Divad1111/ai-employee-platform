/**
 * 新建角色：填写名称并勾选权限（可搜索中文）
 */
import { FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { SearchableSelect } from '../components/SearchableSelect'
import { permLabel, SCOPE_LABEL, SCOPE_OPTIONS } from '../lib/rbacLabels'

type PermInfo = { code: string; description: string }
type DraftGrant = { code: string; scope: string }

export function RoleCreatePage() {
  const nav = useNavigate()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [perms, setPerms] = useState<PermInfo[]>([])
  const [grants, setGrants] = useState<DraftGrant[]>([])
  const [pickPerm, setPickPerm] = useState('')
  const [pickScope, setPickScope] = useState('OWN')
  const [err, setErr] = useState('')
  const [saving, setSaving] = useState(false)

  const permOptions = useMemo(
    () =>
      perms
        .filter((p) => !grants.some((g) => g.code === p.code))
        .map((p) => ({
          value: p.code,
          label: permLabel(p.code, p.description),
          keywords: `${p.code} ${p.description}`,
        })),
    [perms, grants],
  )

  const scopeOptions = useMemo(
    () => SCOPE_OPTIONS.map((o) => ({ value: o.value, label: o.label })),
    [],
  )

  const permDesc = useMemo(() => {
    const m: Record<string, string> = {}
    for (const p of perms) m[p.code] = p.description
    return m
  }, [perms])

  useEffect(() => {
    apiGet<{ items: PermInfo[] }>('/permissions')
      .then((r) => setPerms(r.items || []))
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : String(e)))
  }, [])

  const addGrant = () => {
    if (!pickPerm) {
      setErr('请先选择一项权限')
      return
    }
    setErr('')
    setGrants((prev) => [...prev, { code: pickPerm, scope: pickScope }])
    setPickPerm('')
    setPickScope('OWN')
  }

  const removeGrant = (code: string) => {
    setGrants((prev) => prev.filter((g) => g.code !== code))
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setErr('')
    setSaving(true)
    try {
      await apiPost('/roles', {
        name: name.trim().toUpperCase(),
        description: description.trim() || name.trim(),
        grants: grants.map((g) => ({ permission: g.code, scope: g.scope })),
      })
      nav('/roles', { replace: true })
    } catch (ex: unknown) {
      setErr(ex instanceof Error ? ex.message : String(ex))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="page">
      <div className="page-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1rem' }}>
        <div>
          <h1>新建角色</h1>
          <p>填写角色标识与显示名，再添加该角色拥有的权限</p>
        </div>
        <Link to="/roles" className="btn-ghost btn-sm" style={{ textDecoration: 'none' }}>
          ← 返回角色列表
        </Link>
      </div>

      {err ? <div className="error" style={{ marginBottom: '0.8rem' }}>{err}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>基本信息</h2>
            <p>角色标识用于系统内引用，显示名用于界面展示</p>
          </div>
        </div>
        <form className="stack-form" onSubmit={onSubmit} style={{ maxWidth: 640 }}>
          <label>
            <span className="field-caption">角色标识</span>
            <input
              value={name}
              onChange={(e) => setName(e.target.value.toUpperCase())}
              required
              minLength={2}
              maxLength={32}
              pattern="[A-Z0-9_]+"
              placeholder="例：CUSTOM_OPS"
              title="仅大写字母、数字与下划线"
            />
          </label>
          <label>
            <span className="field-caption">显示名称</span>
            <input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="例：自定义运维"
            />
          </label>

          <div>
            <span className="field-caption">添加权限</span>
            <div className="perm-add-row">
              <SearchableSelect
                value={pickPerm}
                onChange={setPickPerm}
                options={permOptions}
                placeholder="输入筛选权限…"
              />
              <SearchableSelect
                value={pickScope}
                onChange={setPickScope}
                options={scopeOptions}
                placeholder="作用范围…"
              />
              <button type="button" className="btn-ghost btn-sm" onClick={addGrant}>
                加入列表
              </button>
            </div>
            <div className="perm-chip-list">
              {grants.map((g) => (
                <div key={g.code} className="perm-chip">
                  <div className="perm-chip-meta">
                    <strong>{permLabel(g.code, permDesc[g.code])}</strong>
                    <span className="muted" style={{ fontSize: '0.78rem' }}>
                      范围：{SCOPE_LABEL[g.scope] || g.scope}
                    </span>
                  </div>
                  <button type="button" className="btn-ghost btn-sm" onClick={() => removeGrant(g.code)}>
                    移除
                  </button>
                </div>
              ))}
              {!grants.length ? (
                <p className="muted" style={{ margin: 0, fontSize: '0.86rem' }}>
                  尚未添加权限。可先创建空角色，稍后在列表中展开配置。
                </p>
              ) : null}
            </div>
          </div>

          <div className="form-actions">
            <button type="submit" className="btn-success btn-sm" disabled={saving}>
              {saving ? '创建中…' : '创建角色'}
            </button>
            <Link to="/roles" className="btn-ghost btn-sm" style={{ textDecoration: 'none' }}>
              取消
            </Link>
          </div>
        </form>
      </div>
    </div>
  )
}
