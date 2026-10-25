import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  listCredentials,
  queueOperation,
  saveCluster,
  saveHost,
  syncCluster,
  type Cluster,
  type Credential,
  type Host,
  type VMCreate,
} from '../../api/infrastructure'
import { CredentialsPanel } from './CredentialsPanel'
import {
  bytes,
  dateText,
  errorText,
  Field,
  Form,
  Modal,
  Notice,
  State,
  useInfrastructure,
  useProtectedAction,
} from './shared'

export function PVEPage() {
  const {
    data,
    error,
    setError,
    message,
    busy,
    reload,
    perform,
    canAll,
    allowed,
  } = useInfrastructure()
  const { protect, dialog } = useProtectedAction()
  const navigate = useNavigate()
  const [editing, setEditing] = useState<Partial<Cluster> | null>(null)
  const [external, setExternal] = useState(false)
  const [credentialsOpen, setCredentialsOpen] = useState(false)
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [filter, setFilter] = useState('')
  const [creating, setCreating] = useState<VMCreate | null>(null)
  const connectionOpen = editing !== null
  const canCredentials = canAll('infra.credential')
  const canManage = canAll('infra.manage')
  const canDeploy = canAll('infra.deploy')
  useEffect(() => {
    let current = true
    if (canCredentials)
      void listCredentials()
        .then((v) => {
          if (current) setCredentials(v.items)
        })
        .catch((e) => {
          if (current) setError(errorText(e))
        })
    else setCredentials([])
    return () => {
      current = false
    }
  }, [credentialsOpen, connectionOpen, canCredentials, setError])
  if (!allowed) return <p>需要基础设施查看权限，且权限范围为全部资源。</p>
  const hosts = data.hosts.filter(
    (v) =>
      !v.template &&
      (!filter ||
        [v.name, v.address, v.node, String(v.vmid)].some((x) =>
          x?.includes(filter),
        )),
  )
  const createVM = (c: Cluster) =>
    setCreating({
      cluster_id: c.id,
      template_node: '',
      template_id: 0,
      vmid: 100,
      node: c.nodes?.[0]?.node || '',
      name: '',
      cores: 2,
      memory_mb: 4096,
      disk_gb: 0,
      disk_device: 'scsi0',
      arch: 'amd64',
      storage: '',
      bridge: 'vmbr0',
      ip_config: 'ip=dhcp',
      ssh_user: 'ubuntu',
      public_key: '',
      address: '',
      secret_id: '',
      releases: [],
      server_url: '',
    })
  return (
    <section className="infra-page">
      <header className="page-header">
        <div>
          <h1>PVE 管理</h1>
          <p>管理集群资源、创建虚拟机，并将计算资源接入工作站。</p>
        </div>
      </header>
      <Notice error={error} message={message} />
      <div className="infra-toolbar">
        <div className="infra-actions">
          {canManage && (
            <>
              <button
                onClick={() =>
                  setEditing({ name: '', url: '', secret_id: '', ca_pem: '' })
                }
              >
                新增 PVE 连接
              </button>
              <button className="btn-ghost" onClick={() => setExternal(true)}>
                登记外部 Linux 主机
              </button>
            </>
          )}
          {canAll('infra.credential') && (
            <button
              className="btn-ghost"
              onClick={() => setCredentialsOpen(true)}
            >
              管理凭证
            </button>
          )}
        </div>
        <div className="infra-actions">
          <Link to="/infrastructure/deployments">部署任务</Link>
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() => void reload().catch((e) => setError(errorText(e)))}
          >
            刷新
          </button>
        </div>
      </div>
      <div className="infra-summary">
        <div>
          <strong>{data.clusters.length}</strong>
          <span>PVE 连接</span>
        </div>
        <div>
          <strong>{data.hosts.filter((h) => !h.template).length}</strong>
          <span>虚拟机 / 主机</span>
        </div>
        <div>
          <strong>
            {data.hosts.filter((h) => h.status === 'running').length}
          </strong>
          <span>运行中的虚拟机</span>
        </div>
        <div>
          <strong>{data.hosts.filter((h) => h.workstation_id).length}</strong>
          <span>已关联工作站</span>
        </div>
      </div>
      {data.clusters.length === 0 && (
        <div className="infra-panel infra-empty">
          先保存 PVE API Token，再添加 PVE 连接并同步资源。
        </div>
      )}
      {data.clusters.map((c) => (
        <section key={c.id} className="infra-panel">
          <header>
            <div>
              <h2>{c.name}</h2>
              <span className="infra-muted">
                {c.url} · 最近同步 {dateText(c.last_sync)}
              </span>
            </div>
            <div className="infra-actions">
              {canManage && (
                <>
                  <button className="btn-ghost" onClick={() => setEditing(c)}>
                    编辑连接
                  </button>
                  <button
                    disabled={busy}
                    onClick={() =>
                      void perform(
                        () => syncCluster(c.id),
                        'PVE 资源已同步',
                      ).catch(() => {})
                    }
                  >
                    同步 / 测试连接
                  </button>
                </>
              )}
              {canDeploy && (
                <button className="btn-ghost" onClick={() => createVM(c)}>
                  创建虚拟机
                </button>
              )}
            </div>
          </header>
          <Notice error={c.last_error} />
          {!!c.nodes?.length && (
            <div className="infra-table-wrap">
              <table className="infra-table">
                <thead>
                  <tr>
                    <th>节点</th>
                    <th>状态</th>
                    <th>CPU 使用率</th>
                    <th>内存</th>
                  </tr>
                </thead>
                <tbody>
                  {c.nodes.map((n) => (
                    <tr key={n.node}>
                      <td>{n.node}</td>
                      <td>
                        <State value={n.status} />
                      </td>
                      <td>{(n.cpu * 100).toFixed(1)}%</td>
                      <td>
                        {bytes(n.mem)} / {bytes(n.maxmem)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      ))}
      <section className="infra-panel">
        <header>
          <h2>虚拟机与主机</h2>
          <input
            aria-label="搜索虚拟机"
            placeholder="名称、IP、节点、VMID"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </header>
        <div className="infra-table-wrap">
          <table className="infra-table">
            <thead>
              <tr>
                <th>名称</th>
                <th>电源状态</th>
                <th>资源</th>
                <th>SSH 地址</th>
                <th>工作站</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {hosts.map((h) => (
                <tr key={h.id}>
                  <td>
                    <Link to={`/pve/hosts/${h.id}`}>{h.name}</Link>
                    <small>
                      {h.node || '外部主机'} {h.vmid ? ` / ${h.vmid}` : ''}
                    </small>
                  </td>
                  <td>
                    <State value={h.status} />
                  </td>
                  <td>
                    {h.cores || '—'} 核 · {bytes(h.max_memory)}
                    <small>
                      磁盘 {bytes(h.max_disk)} · CPU {(h.cpu * 100).toFixed(1)}%
                    </small>
                  </td>
                  <td>
                    {h.address || '待配置'}
                    <small>{h.ssh_user || '待配置用户'}</small>
                  </td>
                  <td>
                    {h.workstation_id ? (
                      <>
                        <Link to={`/workstations/${h.workstation_id}`}>
                          查看工作站
                        </Link>
                        <small>
                          <State
                            value={
                              data.workstation_states[h.workstation_id] ||
                              'UNKNOWN'
                            }
                          />
                        </small>
                      </>
                    ) : (
                      '尚未接入'
                    )}
                  </td>
                  <td>
                    <Link to={`/pve/hosts/${h.id}`}>详情 / 部署 / SSH</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {hosts.length === 0 && (
            <div className="infra-empty">
              尚无匹配的主机。同步 PVE 或登记外部主机后显示在这里。
            </div>
          )}
        </div>
      </section>
      {editing && (
        <Modal
          title={editing.id ? '编辑 PVE 连接' : '新增 PVE 连接'}
          onClose={() => setEditing(null)}
        >
          <Form
            onSubmit={async () => {
              await perform(
                () => saveCluster(editing),
                '连接已保存，请同步测试',
              )
              setEditing(null)
            }}
          >
            <Field label="名称">
              <input
                required
                value={editing.name}
                onChange={(e) =>
                  setEditing({ ...editing, name: e.target.value })
                }
              />
            </Field>
            <Field label="PVE 地址">
              <input
                required
                type="url"
                placeholder="https://pve.example.com:8006"
                value={editing.url}
                onChange={(e) =>
                  setEditing({ ...editing, url: e.target.value })
                }
              />
            </Field>
            <Field wide label="保险箱中的 PVE Token">
              <select
                required
                value={editing.secret_id}
                onChange={(e) =>
                  setEditing({ ...editing, secret_id: e.target.value })
                }
              >
                <option value="">
                  {canCredentials ? '选择凭证' : '需要基础设施凭证权限'}
                </option>
                {credentials
                  .filter((v) => v.kind === 'pve')
                  .map((v) => (
                    <option value={v.id} key={v.id}>
                      {v.name}
                    </option>
                  ))}
              </select>
            </Field>
            {canCredentials && !credentials.some((v) => v.kind === 'pve') && (
              <p>尚无 PVE 凭证。可在“管理凭证”中新增，或在保险箱以 user@realm!tokenid 命名保存。</p>
            )}
            <Field wide label="私有 CA 证书 PEM（使用公共证书时留空）">
              <textarea
                value={editing.ca_pem}
                onChange={(e) =>
                  setEditing({ ...editing, ca_pem: e.target.value })
                }
              />
            </Field>
          </Form>
        </Modal>
      )}
      {external && (
        <ExternalHostForm
          credentials={credentials}
          onClose={() => setExternal(false)}
          onSaved={async (h) => {
            await reload()
            navigate(`/pve/hosts/${h.id}`)
          }}
        />
      )}
      {creating && (
        <Modal title="从模板创建虚拟机" onClose={() => setCreating(null)}>
          <p>
            选择已准备 Cloud-init 与 QEMU Guest Agent 的 Linux
            模板。创建后如需安装软件，请在主机详情核对 SSH
            指纹，队列会继续执行。
          </p>
          <Form
            submit="验证并创建"
            onSubmit={() =>
              protect('创建虚拟机', async () => {
                await queueOperation({ action: 'clone', create: creating })
                setCreating(null)
                navigate('/infrastructure/deployments')
              })
            }
          >
            <Field label="系统模板">
              <select
                required
                value={creating.template_id || ''}
                onChange={(e) => {
                  const h = data.hosts.find(
                    (v) =>
                      v.cluster_id === creating.cluster_id &&
                      v.vmid === Number(e.target.value),
                  )
                  setCreating({
                    ...creating,
                    template_id: h?.vmid || 0,
                    template_node: h?.node || '',
                  })
                }}
              >
                <option value="">选择模板</option>
                {data.hosts
                  .filter(
                    (h) => h.cluster_id === creating.cluster_id && h.template,
                  )
                  .map((h) => (
                    <option key={h.id} value={h.vmid}>
                      {h.name} ({h.node} / {h.vmid})
                    </option>
                  ))}
              </select>
            </Field>
            <Field label="目标节点">
              <select
                required
                value={creating.node}
                onChange={(e) =>
                  setCreating({ ...creating, node: e.target.value })
                }
              >
                {data.clusters
                  .find((c) => c.id === creating.cluster_id)
                  ?.nodes?.map((n) => (
                    <option key={n.node}>{n.node}</option>
                  ))}
              </select>
            </Field>
            <Field label="虚拟机名称">
              <input
                required
                pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,79}"
                value={creating.name}
                onChange={(e) =>
                  setCreating({ ...creating, name: e.target.value })
                }
              />
            </Field>
            <Field label="VMID（未占用）">
              <input
                required
                type="number"
                min={100}
                value={creating.vmid}
                onChange={(e) =>
                  setCreating({ ...creating, vmid: Number(e.target.value) })
                }
              />
            </Field>
            <Field label="CPU 核数">
              <input
                required
                type="number"
                min={1}
                max={512}
                value={creating.cores}
                onChange={(e) =>
                  setCreating({ ...creating, cores: Number(e.target.value) })
                }
              />
            </Field>
            <Field label="内存 MiB">
              <input
                required
                type="number"
                min={256}
                value={creating.memory_mb}
                onChange={(e) =>
                  setCreating({
                    ...creating,
                    memory_mb: Number(e.target.value),
                  })
                }
              />
            </Field>
            <Field label="系统架构">
              <select
                value={creating.arch || 'amd64'}
                onChange={(e) =>
                  setCreating({
                    ...creating,
                    arch: e.target.value,
                    releases: [],
                  })
                }
              >
                <option>amd64</option>
                <option>arm64</option>
              </select>
            </Field>
            <Field label="目标磁盘 GiB（0 表示沿用模板，仅支持扩容）">
              <input
                type="number"
                min={0}
                value={creating.disk_gb || 0}
                onChange={(e) =>
                  setCreating({ ...creating, disk_gb: Number(e.target.value) })
                }
              />
            </Field>
            <Field label="模板中的根磁盘设备">
              <input
                value={creating.disk_device || 'scsi0'}
                onChange={(e) =>
                  setCreating({ ...creating, disk_device: e.target.value })
                }
              />
            </Field>
            <Field label="存储（留空沿用模板）">
              <input
                value={creating.storage}
                onChange={(e) =>
                  setCreating({ ...creating, storage: e.target.value })
                }
              />
            </Field>
            <Field label="网桥">
              <input
                value={creating.bridge}
                onChange={(e) =>
                  setCreating({ ...creating, bridge: e.target.value })
                }
              />
            </Field>
            <Field
              wide
              label="Cloud-init 网络（例如 ip=dhcp 或 ip=192.168.1.20/24,gw=192.168.1.1）"
            >
              <input
                required
                value={creating.ip_config}
                onChange={(e) =>
                  setCreating({ ...creating, ip_config: e.target.value })
                }
              />
            </Field>
            <Field label="SSH 用户">
              <input
                required
                value={creating.ssh_user}
                onChange={(e) =>
                  setCreating({ ...creating, ssh_user: e.target.value })
                }
              />
            </Field>
            <Field label="SSH 地址（可在创建后探测）">
              <input
                value={creating.address}
                onChange={(e) =>
                  setCreating({ ...creating, address: e.target.value })
                }
              />
            </Field>
            <Field wide label="SSH 公钥（或沿用模板已配置的密钥）">
              <input
                value={creating.public_key}
                onChange={(e) =>
                  setCreating({ ...creating, public_key: e.target.value })
                }
              />
            </Field>
            <Field wide label="SSH 凭证">
              <select
                value={creating.secret_id}
                onChange={(e) =>
                  setCreating({ ...creating, secret_id: e.target.value })
                }
              >
                <option value="">创建后配置</option>
                {credentials
                  .filter((v) => v.name.startsWith('infra.ssh.'))
                  .map((v) => (
                    <option key={v.id} value={v.id}>
                      {v.name}
                    </option>
                  ))}
              </select>
            </Field>
            <Field wide label="创建后安装的软件（按所选架构筛选）">
              <div className="infra-selection">
                {data.releases
                  .filter(
                    (r) =>
                      r.os === 'linux' && r.arch === (creating.arch || 'amd64'),
                  )
                  .map((r) => (
                    <label className="infra-check" key={r.id}>
                      <input
                        type="checkbox"
                        checked={creating.releases.includes(r.id)}
                        onChange={(e) =>
                          setCreating({
                            ...creating,
                            releases: e.target.checked
                              ? [
                                  ...creating.releases.filter(
                                    (id) =>
                                      data.releases.find((v) => v.id === id)
                                        ?.software_id !== r.software_id,
                                  ),
                                  r.id,
                                ]
                              : creating.releases.filter((id) => id !== r.id),
                          })
                        }
                      />
                      {data.software.find((s) => s.id === r.software_id)?.name}{' '}
                      {r.version}
                    </label>
                  ))}
                {data.releases.length === 0 &&
                  '请先在软件管理中上传或同步软件版本。'}
              </div>
            </Field>
            <Field
              wide
              label="虚拟机可访问的 Control Plane HTTP 地址（首次安装 aew 必填）"
            >
              <input
                type="url"
                value={creating.server_url}
                onChange={(e) =>
                  setCreating({ ...creating, server_url: e.target.value })
                }
              />
            </Field>
          </Form>
        </Modal>
      )}
      {credentialsOpen && (
        <CredentialsPanel onClose={() => setCredentialsOpen(false)} />
      )}
      {dialog}
    </section>
  )
}

function ExternalHostForm({
  credentials,
  onClose,
  onSaved,
}: {
  credentials: Credential[]
  onClose: () => void
  onSaved: (h: Host) => Promise<void>
}) {
  const [v, setV] = useState({
    name: '',
    address: '',
    ssh_user: 'ubuntu',
    ssh_port: 22,
    secret_id: '',
    host_key: '',
    os: 'linux',
    arch: 'amd64',
  })
  return (
    <Modal title="登记外部 Linux 主机" onClose={onClose}>
      <Form
        onSubmit={async () => {
          const h = await saveHost(v)
          onClose()
          await onSaved(h)
        }}
      >
        <Field label="主机名称">
          <input
            required
            value={v.name}
            onChange={(e) => setV({ ...v, name: e.target.value })}
          />
        </Field>
        <Field label="IP / 主机名">
          <input
            required
            value={v.address}
            onChange={(e) => setV({ ...v, address: e.target.value })}
          />
        </Field>
        <Field label="SSH 用户">
          <input
            required
            value={v.ssh_user}
            onChange={(e) => setV({ ...v, ssh_user: e.target.value })}
          />
        </Field>
        <Field label="SSH 端口">
          <input
            required
            type="number"
            min={1}
            max={65535}
            value={v.ssh_port}
            onChange={(e) => setV({ ...v, ssh_port: Number(e.target.value) })}
          />
        </Field>
        <Field label="架构">
          <select
            value={v.arch}
            onChange={(e) => setV({ ...v, arch: e.target.value })}
          >
            <option>amd64</option>
            <option>arm64</option>
          </select>
        </Field>
        <Field label="SSH 凭证">
          <select
            value={v.secret_id}
            onChange={(e) => setV({ ...v, secret_id: e.target.value })}
          >
            <option value="">稍后配置</option>
            {credentials
              .filter((c) => c.name.startsWith('infra.ssh.'))
              .map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
          </select>
        </Field>
      </Form>
    </Modal>
  )
}
