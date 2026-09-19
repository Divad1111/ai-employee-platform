/**
 * Employees 数字员工列表与创建。
 */
import { FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { StatusBadge } from '../components/StatusBadge'
import { EntityName } from '../components/EntityName'
import { IconPlus, IconUsers } from '../components/Icons'

type Emp = {
  id: string
  name: string
  status: string
  default_provider: string
  workstation_id: string
  workspace_id: string
}

type WsItem = {
  id: string
  path: string
  repository?: string
}

export function EmployeesPage() {
  const [items, setItems] = useState<Emp[]>([])
  const [workstations, setWorkstations] = useState<Array<{ id: string; name: string }>>([])
  const [workspaces, setWorkspaces] = useState<WsItem[]>([])
  const [name, setName] = useState('')
  const [error, setError] = useState('')

  async function load() {
    const [empData, wsData, wspData] = await Promise.all([
      apiGet<{ items: Emp[] }>('/employees'),
      apiGet<{ items: Array<{ id: string; name: string }> }>('/workstations').catch(() => ({ items: [] })),
      apiGet<{ items: WsItem[] }>('/workspaces').catch(() => ({ items: [] })),
    ])
    setItems(empData.items ?? [])
    setWorkstations(wsData.items ?? [])
    setWorkspaces(wspData.items ?? [])
  }

  const wsMap = Object.fromEntries(
    workstations.map((w) => [w.id, w.name && w.name !== w.id ? w.name : `工作站-${w.id.slice(-6)}`])
  )

  const wspMap = Object.fromEntries(
    workspaces.map((w) => [w.id, w.repository ? `${w.repository}` : (w.path ? w.path.split('/').pop() || w.path : w.id)])
  )

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    try {
      await apiPost('/employees', { name })
      setName('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建数字员工失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>数字员工档案管理 (Employees)</h1>
          <p>数字员工代表具备独立上下文与工作区绑定的 AI 协作实体</p>
        </div>
      </header>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>新增数字员工</h2>
            <p>输入员工名称快速创建，创建后可进入详情绑定工作站与代码工作区</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onCreate}>
          <input
            placeholder="员工姓名 / 代号 (例: 研发助理小智)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            style={{ width: '280px' }}
            required
          />
          <button type="submit">
            <IconPlus size={15} />
            <span>立即创建员工</span>
          </button>
        </form>
        {error ? <div className="error">{error}</div> : null}
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>员工名录列表</h2>
            <p>共登记 {items.length} 位数字员工</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>数字员工 (姓名 / 编号)</th>
                <th>运行状态</th>
                <th>默认驱动引擎</th>
                <th>绑定工作站</th>
                <th>绑定工作区</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((e) => (
                <tr key={e.id}>
                  <td>
                    <EntityName
                      name={e.name}
                      id={e.id}
                      to={`/employees/${e.id}`}
                      icon={<IconUsers size={16} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td>
                    <StatusBadge status={e.status} />
                  </td>
                  <td>
                    <span className="badge" style={{ textTransform: 'capitalize' }}>
                      {e.default_provider || '未指定'}
                    </span>
                  </td>
                  <td>
                    {e.workstation_id ? (
                      <EntityName
                        name={wsMap[e.workstation_id]}
                        id={e.workstation_id}
                        fallback={e.workstation_id}
                      />
                    ) : (
                      <span style={{ color: 'var(--text-muted)' }}>未绑定</span>
                    )}
                  </td>
                  <td>
                    {e.workspace_id ? (
                      <EntityName
                        name={wspMap[e.workspace_id]}
                        id={e.workspace_id}
                        fallback={e.workspace_id}
                      />
                    ) : (
                      <span style={{ color: 'var(--text-muted)' }}>未绑定</span>
                    )}
                  </td>
                  <td>
                    <Link to={`/employees/${e.id}`} className="btn-ghost btn-sm" style={{ display: 'inline-flex' }}>
                      配置详情 →
                    </Link>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={6} className="empty-tip">暂无数字员工记录，请在上方创建</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
