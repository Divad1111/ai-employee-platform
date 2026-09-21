/**
 * Workspaces 项目工作区管理与绑定。
 * 创建流程：先选工作站节点，再填写该节点上的本机绝对路径。
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiDelete, apiGet, apiPost } from '../api/client'
import { IconFolder, IconPlus, IconRefresh, IconUsers, IconServer } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { PageFeatureGuide } from '../components/PageFeatureGuide'

type Workspace = {
  id: string
  workstation_id: string
  employee_id: string
  path: string
  repository: string
  branch: string
  created_at: string
  updated_at: string
}

type Employee = {
  id: string
  name: string
}

type Workstation = {
  id: string
  name: string
  status: string
}

export function WorkspacesPage() {
  const [items, setItems] = useState<Workspace[]>([])
  const [employees, setEmployees] = useState<Employee[]>([])
  const [workstations, setWorkstations] = useState<Workstation[]>([])
  const [workstationId, setWorkstationId] = useState('')
  const [path, setPath] = useState('')
  const [repository, setRepository] = useState('')
  const [branch, setBranch] = useState('main')
  const [employeeId, setEmployeeId] = useState('')
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')
  const [loading, setLoading] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const [wData, empData, nodeData] = await Promise.all([
        apiGet<{ items: Workspace[] }>('/workspaces'),
        apiGet<{ items: Employee[] }>('/employees').catch(() => ({ items: [] })),
        apiGet<{ items: Workstation[] }>('/workstations').catch(() => ({ items: [] })),
      ])
      setItems(wData.items ?? [])
      setEmployees(empData.items ?? [])
      setWorkstations(nodeData.items ?? [])
    } finally {
      setLoading(false)
    }
  }

  const empMap = Object.fromEntries(employees.map((e) => [e.id, e.name]))
  const nodeMap = Object.fromEntries(workstations.map((n) => [n.id, n.name || n.id]))

  useEffect(() => {
    void load().catch((e) => setError(String(e)))
  }, [])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    if (!workstationId.trim()) {
      setError('请先选择工作站节点，再填写该节点上的本机路径')
      return
    }
    try {
      await apiPost('/workspaces', {
        workstation_id: workstationId.trim(),
        path: path.trim(),
        repository: repository.trim(),
        branch: branch.trim() || 'main',
        employee_id: employeeId.trim() || undefined,
      })
      setWorkstationId('')
      setPath('')
      setRepository('')
      setBranch('main')
      setEmployeeId('')
      setMsg('项目工作区创建成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建工作区失败')
    }
  }

  async function onBind(wsId: string, empId: string) {
    setError('')
    setMsg('')
    try {
      await apiPost(`/workspaces/${wsId}/bind`, { employee_id: empId })
      setMsg('工作区绑定更新成功')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '绑定员工失败')
    }
  }

  async function onDelete(wsId: string) {
    if (!confirm(`确定要删除工作区 ${wsId} 吗？此操作不会删除本地文件代码。`)) return
    setError('')
    setMsg('')
    try {
      await apiDelete(`/workspaces/${wsId}`)
      setMsg('工作区已删除')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除工作区失败')
    }
  }

  const selectedNode = workstations.find((n) => n.id === workstationId)

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>项目工作区 (Workspaces)</h1>
          <p>管理数字员工执行任务时挂载的本地代码工程目录、Git 仓库与执行环境</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => void load()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新列表</span>
        </button>
      </header>

      <PageFeatureGuide
        title="项目工作区挂载与代码安全隔离架构指引"
        summary="Workspace 显式声明宿主机上的绝对路径与代码仓库属性，确保 AI Agent 仅在授权工程目录内读写代码。"
        steps={[
          {
            step: '1',
            title: '选定宿主计算节点 (Node)',
            desc: '指定工作区所属的 Workstation，保证路径解析严格在对应目标节点有效。',
            tag: '节点归属',
          },
          {
            step: '2',
            title: '配置绝对物理路径 (Path)',
            desc: '填写真实磁盘路径（如 F:\\ai-employee-test 或 /home/project），禁止隐式随机落盘。',
            tag: '路径挂载',
          },
          {
            step: '3',
            title: '专属数字员工绑定 (Owner)',
            desc: '与数字员工建立一对一绑定关系，飞书派单给该员工时自动锁定并加载本工作区。',
            tag: '上下文锁定',
          },
        ]}
      />

      {error ? <div className="error">{error}</div> : null}
      {msg ? <div className="ok-msg">{msg}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>新建项目工作区</h2>
            <p>先选择工作站节点，再填写该节点上的本机绝对路径；路径只在所选节点上有效</p>
          </div>
        </div>
        <form className="inline-form" onSubmit={onCreate}>
          <select
            value={workstationId}
            onChange={(e) => setWorkstationId(e.target.value)}
            style={{ minWidth: '200px' }}
            required
          >
            <option value="">① 选择工作站节点...</option>
            {workstations.map((n) => (
              <option key={n.id} value={n.id}>
                {n.name || n.id} ({n.status || 'UNKNOWN'})
              </option>
            ))}
          </select>
          <input
            placeholder={
              selectedNode
                ? `② ${selectedNode.name || selectedNode.id} 上的本机绝对路径`
                : '② 先选工作站，再填本机绝对路径'
            }
            value={path}
            onChange={(e) => setPath(e.target.value)}
            style={{ flex: 2, minWidth: '280px' }}
            disabled={!workstationId}
            required
          />
          <input
            placeholder="Git 仓库名 (可选)"
            value={repository}
            onChange={(e) => setRepository(e.target.value)}
            style={{ flex: 1, minWidth: '160px' }}
          />
          <input
            placeholder="默认分支"
            value={branch}
            onChange={(e) => setBranch(e.target.value)}
            style={{ width: '100px' }}
          />
          <input
            list="ws-create-emp-options"
            placeholder="归属员工 (可选)"
            value={employeeId}
            onChange={(e) => setEmployeeId(e.target.value)}
            style={{ width: '160px' }}
          />
          <datalist id="ws-create-emp-options">
            {employees.map((e) => (
              <option key={e.id} value={e.id}>
                {e.name} ({e.id})
              </option>
            ))}
          </datalist>
          <button type="submit" disabled={!workstationId || !path.trim()}>
            <IconPlus size={15} />
            <span>创建工作区</span>
          </button>
        </form>
        {workstations.length === 0 ? (
          <p style={{ margin: '0.75rem 0 0', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
            尚无工作站节点。请先到「工作站节点」完成接入后再创建工作区。
          </p>
        ) : null}
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>工作区目录列表</h2>
            <p>已登记 {items.length} 个代码工程工作区（每位员工独占绑定一个工作区）</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>工作区工程</th>
                <th>工作站节点</th>
                <th>本地物理路径 (Path)</th>
                <th>版本控制 (Git)</th>
                <th>绑定数字员工</th>
                <th>创建时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((w) => (
                <tr key={w.id}>
                  <td>
                    <EntityName
                      name={w.repository || w.path.split('/').filter(Boolean).pop() || '代码工作区'}
                      id={w.id}
                      icon={<IconFolder size={16} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td>
                    {w.workstation_id ? (
                      <EntityName
                        name={nodeMap[w.workstation_id] || w.workstation_id}
                        id={w.workstation_id}
                        to="/workstations"
                        icon={<IconServer size={14} style={{ color: 'var(--brand-600)' }} />}
                      />
                    ) : (
                      <span style={{ color: 'var(--text-muted)' }}>—</span>
                    )}
                  </td>
                  <td>
                    <span className="mono" style={{ fontSize: '0.85rem' }}>{w.path}</span>
                  </td>
                  <td>
                    {w.repository ? (
                      <span className="badge">
                        {w.repository}{w.branch ? ` @ ${w.branch}` : ''}
                      </span>
                    ) : (
                      <span style={{ color: 'var(--text-muted)' }}>—</span>
                    )}
                  </td>
                  <td>
                    {w.employee_id ? (
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                        <EntityName
                          name={empMap[w.employee_id] || w.employee_id}
                          id={w.employee_id}
                          to={`/employees/${w.employee_id}`}
                          icon={<IconUsers size={14} style={{ color: 'var(--brand-600)' }} />}
                        />
                        <button
                          type="button"
                          className="btn-ghost btn-sm"
                          style={{ padding: '0.1rem 0.35rem', fontSize: '0.7rem' }}
                          onClick={() => void onBind(w.id, '')}
                          title="解除绑定"
                        >
                          解绑
                        </button>
                      </div>
                    ) : (
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.3rem' }}>
                        <select
                          style={{ fontSize: '0.8rem', padding: '0.2rem 0.4rem' }}
                          defaultValue=""
                          onChange={(e) => {
                            if (e.target.value) {
                              void onBind(w.id, e.target.value)
                            }
                          }}
                        >
                          <option value="">点击分配员工...</option>
                          {employees.map((e) => (
                            <option key={e.id} value={e.id}>
                              {e.name} ({e.id})
                            </option>
                          ))}
                        </select>
                      </div>
                    )}
                  </td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {w.created_at ? new Date(w.created_at).toLocaleString() : '—'}
                  </td>
                  <td>
                    <button
                      type="button"
                      className="btn-ghost btn-sm"
                      style={{ color: '#dc2626' }}
                      onClick={() => void onDelete(w.id)}
                    >
                      删除
                    </button>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={7} style={{ textAlign: 'center', padding: '2.5rem' }}>
                    <div style={{ color: 'var(--text-muted)' }}>暂无登记的项目工作区，请在上方创建</div>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}
