import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  discoverAddress,
  listCredentials,
  probeHostKey,
  queueOperation,
  saveHost,
  savePolicy,
  terminalTicket,
  type Credential,
  type Host,
  type Policy,
} from '../../api/infrastructure'
import { SSHConsole } from './SSHConsole'
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

export function HostDetailPage() {
  const { id } = useParams()
  const { data, error, setError, message, perform, canAll, allowed } =
    useInfrastructure()
  const { protect, dialog } = useProtectedAction()
  const host = data.hosts.find((h) => h.id === id)
  const [editing, setEditing] = useState<Host | null>(null)
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [ticket, setTicket] = useState('')
  const [installing, setInstalling] = useState(false)
  const [releaseID, setReleaseID] = useState('')
  const [serverURL, setServerURL] = useState('')
  const [policy, setPolicy] = useState<Policy | null>(null)
  const [loginSoftware, setLoginSoftware] = useState('')
  const [loginSecret, setLoginSecret] = useState('')
  useEffect(() => {
    if (canAll('infra.credential'))
      void listCredentials()
        .then((v) => setCredentials(v.items))
        .catch((e) => setError(errorText(e)))
  }, [canAll, setError])
  if (!allowed) return <p>需要基础设施查看权限。</p>
  if (!host)
    return (
      <section className="infra-page">
        <Link to="/pve">返回 PVE 管理</Link>
        <Notice error={error} />
        <p>正在加载主机；如果资源已被删除，请返回列表刷新。</p>
      </section>
    )
  const installs = data.installations.filter((v) => v.host_id === host.id)
  const compatible = data.releases.filter(
    (r) => r.os === host.os && r.arch === host.arch,
  )
  const run = (action: string, extra = {}) =>
    protect(
      `${({ start: '启动', shutdown: '关闭', reboot: '重启', rollback: '回滚软件', inspect: '探测软件' } as Record<string, string>)[action] || action} · ${host.name}`,
      () =>
        perform(
          () => queueOperation({ action, host_id: host.id, ...extra }),
          '任务已加入部署队列',
        ),
    )
  const editPolicy = (software: string) =>
    setPolicy(
      data.policies.find(
        (p) => p.host_id === host.id && p.software_id === software,
      ) || {
        id: '',
        host_id: host.id,
        software_id: software,
        enabled: true,
        auto_install: false,
        pinned_release_id: '',
        start: '02:00',
        end: '04:00',
        timezone: 'Asia/Shanghai',
        interval_minutes: 60,
      },
    )
  return (
    <section className="infra-page">
      <Link to="/pve">← PVE 管理</Link>
      <header className="infra-toolbar">
        <div>
          <h1>{host.name}</h1>
          <p>
            {host.node || '外部主机'} {host.vmid ? ` / VMID ${host.vmid}` : ''}{' '}
            · {host.os} / {host.arch}
          </p>
        </div>
        <div className="infra-actions">
          {canAll('infra.manage') && (
            <button
              className="btn-ghost"
              onClick={() => setEditing({ ...host })}
            >
              SSH 配置
            </button>
          )}
          {canAll('infra.console') && (
            <button
              onClick={() =>
                protect('打开 SSH 终端', async () =>
                  setTicket((await terminalTicket(host.id)).ticket),
                )
              }
            >
              SSH 登录
            </button>
          )}
        </div>
      </header>
      <Notice error={error} message={message} />
      <section className="infra-panel">
        <div className="infra-detail-grid">
          <div>
            虚拟机状态 <State value={host.status} />
          </div>
          <div>
            资源 {host.cores || '—'} 核 / {bytes(host.max_memory)} /{' '}
            {bytes(host.max_disk)}
          </div>
          <div>
            SSH {host.ssh_user || '待配置'}@{host.address || '待配置'}:
            {host.ssh_port}
          </div>
          <div>
            工作站{' '}
            {host.workstation_id ? (
              <>
                <Link to={`/workstations/${host.workstation_id}`}>
                  {host.workstation_id}
                </Link>{' '}
                <State
                  value={
                    data.workstation_states[host.workstation_id] || 'UNKNOWN'
                  }
                />
              </>
            ) : (
              '尚未安装并注册 aew'
            )}
          </div>
        </div>
        {host.cluster_id && canAll('infra.deploy') && (
          <div className="infra-actions">
            <button className="btn-ghost" onClick={() => run('start')}>
              启动
            </button>
            <button className="btn-ghost" onClick={() => run('shutdown')}>
              正常关机
            </button>
            <button className="btn-ghost" onClick={() => run('reboot')}>
              重启
            </button>
          </div>
        )}
        {!host.host_key && (
          <p>使用 SSH 前，请先配置地址和凭证，探测并核对主机指纹。</p>
        )}
      </section>
      {ticket && <SSHConsole ticket={ticket} onClose={() => setTicket('')} />}
      <section className="infra-panel">
        <header>
          <h2>软件安装与认证</h2>
          {canAll('infra.deploy') && (
            <button
              onClick={() => {
                setReleaseID('')
                setInstalling(true)
              }}
            >
              部署软件
            </button>
          )}
        </header>
        <p>
          程序安装、账号认证与工作站在线状态分别检查。Codex 也可在 SSH
          终端执行设备码登录。
        </p>
        <div className="infra-table-wrap">
          <table className="infra-table">
            <thead>
              <tr>
                <th>软件</th>
                <th>版本 / 实际探测</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {installs.map((i) => (
                <tr key={i.id}>
                  <td>
                    <Link to="/software">
                      {data.software.find((s) => s.id === i.software_id)
                        ?.name || i.software_id}
                    </Link>
                  </td>
                  <td>
                    {i.version || '外部安装'}
                    <small className="infra-code">{i.detected_version}</small>
                  </td>
                  <td>
                    <State value={i.status} /> <State value={i.auth_status} />
                  </td>
                  <td>
                    <div className="infra-actions">
                      {canAll('infra.deploy') && (
                        <>
                          <button
                            className="btn-ghost"
                            onClick={() =>
                              run('inspect', { software_id: i.software_id })
                            }
                          >
                            重新探测
                          </button>
                          {i.previous_release_id && (
                            <button
                              className="btn-ghost"
                              onClick={() =>
                                run('rollback', {
                                  release_id: i.previous_release_id,
                                })
                              }
                            >
                              回滚上一版
                            </button>
                          )}
                          {data.software.find((s) => s.id === i.software_id)
                            ?.recipe === 'codex' && (
                            <button
                              className="btn-ghost"
                              onClick={() => {
                                setLoginSoftware(i.software_id)
                                setLoginSecret('')
                              }}
                            >
                              API Key 登录
                            </button>
                          )}
                        </>
                      )}
                      {canAll('infra.manage') && (
                        <button
                          className="btn-ghost"
                          onClick={() => editPolicy(i.software_id)}
                        >
                          更新策略
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {installs.length === 0 && (
            <div className="infra-empty">
              尚无安装记录。可部署软件，或探测已在主机上安装的软件。
            </div>
          )}
        </div>
        {canAll('infra.deploy') && (
          <div className="infra-actions">
            <span className="infra-muted">探测外部安装：</span>
            {data.software.map((s) => (
              <button
                key={s.id}
                className="btn-ghost"
                onClick={() => run('inspect', { software_id: s.id })}
              >
                {s.name}
              </button>
            ))}
          </div>
        )}
      </section>
      <section className="infra-panel">
        <h2>自动维护</h2>
        {data.policies
          .filter((p) => p.host_id === host.id)
          .map((p) => (
            <div key={p.id}>
              <div className="infra-toolbar">
                <div>
                  <strong>
                    {data.software.find((s) => s.id === p.software_id)?.name}
                  </strong>
                  <p>
                    {p.enabled
                      ? p.auto_install
                        ? '自动检查并部署'
                        : '只检查新版本'
                      : '已停用'}{' '}
                    · 每 {p.interval_minutes} 分钟 · {p.start}–{p.end} (
                    {p.timezone}){p.pinned_release_id && ' · 已锁定目标版本'}
                  </p>
                  <small>
                    最近检查 {dateText(p.last_checked)} · 可用版本{' '}
                    {data.releases.find((r) => r.id === p.available_release_id)
                      ?.version || '尚未发现'}
                  </small>
                </div>
                {canAll('infra.manage') && (
                  <button className="btn-ghost" onClick={() => setPolicy(p)}>
                    编辑
                  </button>
                )}
              </div>
              <Notice error={p.last_error} />
            </div>
          ))}
        {data.policies.every((p) => p.host_id !== host.id) && (
          <p>从已安装软件的“更新策略”配置检查周期、版本锁定与维护窗口。</p>
        )}
      </section>
      <section className="infra-panel">
        <header>
          <h2>最近部署</h2>
          <Link to="/infrastructure/deployments">查看所有任务</Link>
        </header>
        {data.operations
          .filter((o) => o.host_id === host.id)
          .slice(0, 5)
          .map((o) => (
            <div className="infra-toolbar" key={o.id}>
              <span>
                <State value={o.status} /> {o.step}
              </span>
              <small>{dateText(o.updated_at)}</small>
            </div>
          ))}
      </section>
      {editing && (
        <Modal title="SSH 连接与主机配置" onClose={() => setEditing(null)}>
          <Form
            onSubmit={async () => {
              await perform(() => saveHost(editing), '主机连接已保存')
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
            <Field label="IP / 主机名">
              <input
                required
                value={editing.address}
                onChange={(e) =>
                  setEditing({ ...editing, address: e.target.value })
                }
              />
            </Field>
            <Field label="SSH 用户">
              <input
                required
                value={editing.ssh_user}
                onChange={(e) =>
                  setEditing({ ...editing, ssh_user: e.target.value })
                }
              />
            </Field>
            <Field label="端口">
              <input
                required
                type="number"
                min={1}
                max={65535}
                value={editing.ssh_port}
                onChange={(e) =>
                  setEditing({ ...editing, ssh_port: Number(e.target.value) })
                }
              />
            </Field>
            <Field label="系统">
              <select
                value={editing.os}
                onChange={(e) => setEditing({ ...editing, os: e.target.value })}
              >
                <option>linux</option>
              </select>
            </Field>
            <Field label="架构">
              <select
                value={editing.arch}
                onChange={(e) =>
                  setEditing({ ...editing, arch: e.target.value })
                }
              >
                <option>amd64</option>
                <option>arm64</option>
              </select>
            </Field>
            <Field wide label="SSH 凭证">
              <select
                required
                value={editing.secret_id}
                onChange={(e) =>
                  setEditing({ ...editing, secret_id: e.target.value })
                }
              >
                <option value="">选择凭证</option>
                {credentials
                  .filter((c) => c.name.startsWith('infra.ssh.'))
                  .map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
              </select>
            </Field>
            <Field wide label="信任的 SSH 主机指纹 SHA256">
              <input
                value={editing.host_key}
                onChange={(e) =>
                  setEditing({ ...editing, host_key: e.target.value })
                }
              />
            </Field>
            <div className="wide">
              <p>
                探测前需先保存当前地址与端口。请通过 PVE
                控制台等可信渠道核对指纹，再保存信任。
              </p>
              <div className="infra-actions">
                <button
                  type="button"
                  className="btn-ghost"
                  onClick={() =>
                    void perform(async () => {
                      await saveHost(editing)
                      const key = await probeHostKey(host.id)
                      setEditing({ ...editing, host_key: key.fingerprint })
                    }, '已探测指纹，请核对后保存').catch(() => {})
                  }
                >
                  保存地址并探测指纹
                </button>
                {host.cluster_id && (
                  <button
                    type="button"
                    className="btn-ghost"
                    onClick={() =>
                      void perform(async () => {
                        const h = await discoverAddress(host.id)
                        setEditing({ ...editing, address: h.address })
                      }, '已读取 Guest Agent 地址').catch(() => {})
                    }
                  >
                    从 Guest Agent 读取 IP
                  </button>
                )}
              </div>
            </div>
          </Form>
        </Modal>
      )}
      {installing && (
        <Modal title="部署软件到此主机" onClose={() => setInstalling(false)}>
          <Form
            submit="验证并部署"
            onSubmit={() => {
              if (!releaseID) throw new Error('请选择软件版本')
              protect('部署软件', async () => {
                await perform(
                  () =>
                    queueOperation({
                      action: 'install',
                      host_id: host.id,
                      release_id: releaseID,
                      server_url: serverURL,
                    }),
                  '部署任务已排队',
                )
                setInstalling(false)
              })
            }}
          >
            <Field wide label="软件版本">
              <select
                required
                value={releaseID}
                onChange={(e) => setReleaseID(e.target.value)}
              >
                <option value="">选择版本</option>
                {compatible.map((r) => (
                  <option key={r.id} value={r.id}>
                    {data.software.find((s) => s.id === r.software_id)?.name}{' '}
                    {r.version} ({r.arch})
                  </option>
                ))}
              </select>
            </Field>
            <Field wide label="Control Plane HTTP 地址（aew 首次注册使用）">
              <input
                type="url"
                placeholder="http://control-plane.example.com:8080"
                value={serverURL}
                onChange={(e) => setServerURL(e.target.value)}
              />
            </Field>
            <p className="wide">
              安装 aew 需要 systemd；非 root 用户需具备免密
              sudo，以启用用户常驻服务。
            </p>
          </Form>
        </Modal>
      )}
      {policy && (
        <Modal title="软件更新策略" onClose={() => setPolicy(null)}>
          <Form
            onSubmit={() =>
              protect('保存更新策略', async () => {
                await perform(() => savePolicy(policy), '更新策略已保存')
                setPolicy(null)
              })
            }
          >
            <label className="infra-check">
              <input
                type="checkbox"
                checked={policy.enabled}
                onChange={(e) =>
                  setPolicy({ ...policy, enabled: e.target.checked })
                }
              />
              启用定期检查
            </label>
            <label className="infra-check">
              <input
                type="checkbox"
                checked={policy.auto_install}
                onChange={(e) =>
                  setPolicy({ ...policy, auto_install: e.target.checked })
                }
              />
              在维护窗口自动部署
            </label>
            <Field label="检查周期（分钟）">
              <input
                required
                type="number"
                min={1}
                max={10080}
                value={policy.interval_minutes}
                onChange={(e) =>
                  setPolicy({
                    ...policy,
                    interval_minutes: Number(e.target.value),
                  })
                }
              />
            </Field>
            <Field label="时区">
              <input
                required
                value={policy.timezone}
                onChange={(e) =>
                  setPolicy({ ...policy, timezone: e.target.value })
                }
              />
            </Field>
            <Field label="维护窗口开始">
              <input
                required
                type="time"
                value={policy.start}
                onChange={(e) =>
                  setPolicy({ ...policy, start: e.target.value })
                }
              />
            </Field>
            <Field label="维护窗口结束（相同表示全天）">
              <input
                required
                type="time"
                value={policy.end}
                onChange={(e) => setPolicy({ ...policy, end: e.target.value })}
              />
            </Field>
            <Field wide label="目标版本">
              <select
                value={policy.pinned_release_id}
                onChange={(e) =>
                  setPolicy({ ...policy, pinned_release_id: e.target.value })
                }
              >
                <option value="">追踪平台最新稳定版本</option>
                {compatible
                  .filter((r) => r.software_id === policy.software_id)
                  .map((r) => (
                    <option key={r.id} value={r.id}>
                      锁定 {r.version}
                    </option>
                  ))}
              </select>
            </Field>
            <p className="wide">
              没有在线更新源时，仍可检查平台手动上传的新版本。升级会等待已有工作站任务结束。
            </p>
          </Form>
        </Modal>
      )}
      {loginSoftware && (
        <Modal
          title="使用保险箱 API Key 登录 Codex"
          onClose={() => setLoginSoftware('')}
        >
          <Form
            submit="验证并登录"
            onSubmit={() =>
              protect('登录 Codex CLI', async () => {
                await perform(
                  () =>
                    queueOperation({
                      action: 'login',
                      host_id: host.id,
                      software_id: loginSoftware,
                      secret_id: loginSecret,
                    }),
                  '认证任务已排队',
                )
                setLoginSoftware('')
              })
            }
          >
            <Field wide label="保险箱 API Key">
              <select
                required
                value={loginSecret}
                onChange={(e) => setLoginSecret(e.target.value)}
              >
                <option value="">选择凭证</option>
                {credentials
                  .filter((c) => c.name.startsWith('infra.api.'))
                  .map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
              </select>
            </Field>
            <p className="wide">
              如需使用 ChatGPT 账号，可在 SSH 终端运行 codex login
              --device-auth，完成后重新探测认证状态。
            </p>
          </Form>
        </Modal>
      )}
      {dialog}
    </section>
  )
}
