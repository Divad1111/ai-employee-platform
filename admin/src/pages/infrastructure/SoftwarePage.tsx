import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  addRelease,
  checkSoftware,
  downloadRelease,
  listCredentials,
  queueOperation,
  saveSoftware,
  type Credential,
  type Release,
  type Software,
} from '../../api/infrastructure'
import {
  dateText,
  errorText,
  Field,
  Form,
  Modal,
  Notice,
  useInfrastructure,
  useProtectedAction,
} from './shared'

export function SoftwarePage() {
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
  const [selected, setSelected] = useState('aew')
  const [editing, setEditing] = useState<Partial<Software> | null>(null)
  const [versionArgs, setVersionArgs] = useState('--version')
  const [adding, setAdding] = useState(false)
  const [upload, setUpload] = useState(true)
  const [release, setRelease] = useState<Partial<Release>>({
    version: '',
    os: 'linux',
    arch: 'amd64',
    format: 'binary',
    sha256: '',
    url: '',
    entry: '',
    notes: '',
  })
  const [file, setFile] = useState<File | undefined>()
  const [deploying, setDeploying] = useState<Release | null>(null)
  const [hosts, setHosts] = useState<string[]>([])
  const [serverURL, setServerURL] = useState('')
  const [credentials, setCredentials] = useState<Credential[]>([])
  useEffect(() => {
    if (canAll('infra.credential'))
      void listCredentials()
        .then((v) => setCredentials(v.items))
        .catch((e) => setError(errorText(e)))
  }, [canAll, setError])
  if (!allowed) return <p>需要基础设施查看权限。</p>
  const software =
    data.software.find((s) => s.id === selected) || data.software[0]
  const versions = data.releases
    .filter((r) => r.software_id === software?.id)
    .sort((a, b) => b.created_at.localeCompare(a.created_at))
  const edit = (s?: Software) => {
    setEditing(
      s || {
        name: '',
        description: '',
        recipe: 'binary',
        binary: '',
        version_args: ['--version'],
        source_type: '',
        source_url: '',
        source_os: 'linux',
        source_arch: 'amd64',
        asset_pattern: '',
      },
    )
    setVersionArgs(s?.version_args?.join(' ') || '--version')
  }
  return (
    <section className="infra-page">
      <header className="infra-toolbar">
        <div>
          <h1>软件管理</h1>
          <p>
            统一保存软件版本，部署到虚拟机和工作站。更新链接可以留空，手动上传版本同样支持部署和维护。
          </p>
        </div>
        <div className="infra-actions">
          <Link to="/infrastructure/deployments">部署任务</Link>
          {canAll('infra.manage') && (
            <button onClick={() => edit()}>新增软件</button>
          )}
        </div>
      </header>
      <Notice error={error} message={message} />
      <div className="infra-two-column">
        <aside className="infra-software-select">
          {data.software.map((s) => (
            <button
              key={s.id}
              className={`btn-ghost ${software?.id === s.id ? 'selected' : ''}`}
              onClick={() => setSelected(s.id)}
            >
              <span>
                {s.name}
                <small>{s.source_url ? '在线更新源' : '手动维护版本'}</small>
              </span>
            </button>
          ))}
        </aside>
        <div>
          {software && (
            <>
              <section className="infra-panel">
                <header>
                  <h2>{software.name}</h2>
                  {canAll('infra.manage') && (
                    <button
                      className="btn-ghost"
                      onClick={() => edit(software)}
                    >
                      编辑软件
                    </button>
                  )}
                </header>
                <p>{software.description}</p>
                <div className="infra-detail-grid">
                  <span>
                    程序 {software.binary} · {software.recipe}
                  </span>
                  <span>
                    {software.source_url
                      ? `更新来源：${software.source_type}`
                      : '更新源为空 · 手动上传新版本'}
                  </span>
                  <span>
                    平台安装记录{' '}
                    {
                      data.installations.filter(
                        (i) => i.software_id === software.id,
                      ).length
                    }{' '}
                    台
                  </span>
                  <span>最近检查 {dateText(software.last_checked)}</span>
                </div>
                <Notice error={software.last_error} />
                <div className="infra-actions">
                  {canAll('infra.manage') && (
                    <>
                      <button
                        onClick={() => {
                          setAdding(true)
                          setFile(undefined)
                          setRelease({
                            version: '',
                            os: 'linux',
                            arch: 'amd64',
                            format: 'binary',
                            sha256: '',
                            url: '',
                            entry: '',
                            notes: '',
                          })
                        }}
                      >
                        新增版本 / 上传安装包
                      </button>
                      <button
                        className="btn-ghost"
                        disabled={busy}
                        onClick={() =>
                          void perform(
                            () => checkSoftware(software.id),
                            software.source_url
                              ? '已检查并导入可用版本'
                              : '本软件使用手动版本，无需访问在线更新源',
                          ).catch(() => {})
                        }
                      >
                        检查更新
                      </button>
                    </>
                  )}
                </div>
              </section>
              <section className="infra-panel">
                <h2>版本与安装包</h2>
                <div className="infra-table-wrap">
                  <table className="infra-table">
                    <thead>
                      <tr>
                        <th>版本</th>
                        <th>平台</th>
                        <th>安装包</th>
                        <th>操作</th>
                      </tr>
                    </thead>
                    <tbody>
                      {versions.map((r) => (
                        <tr key={r.id}>
                          <td>
                            {r.version}
                            <small>{dateText(r.created_at)}</small>
                            <small>{r.notes}</small>
                          </td>
                          <td>
                            {r.os} / {r.arch}
                          </td>
                          <td>
                            {r.format} ·{' '}
                            {r.cached
                              ? `${(r.size / 1024 ** 2).toFixed(1)} MiB 已缓存`
                              : '按需下载并校验'}
                            <small className="infra-code" title={r.sha256}>
                              SHA256 {r.sha256.slice(0, 18)}…
                            </small>
                          </td>
                          <td>
                            <div className="infra-actions">
                              <button
                                className="btn-ghost"
                                onClick={() =>
                                  void downloadRelease(r).catch((e) =>
                                    setError(errorText(e)),
                                  )
                                }
                              >
                                下载到本地
                              </button>
                              {canAll('infra.deploy') && (
                                <button
                                  onClick={() => {
                                    setDeploying(r)
                                    setHosts([])
                                  }}
                                >
                                  部署
                                </button>
                              )}
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  {versions.length === 0 && (
                    <div className="infra-empty">
                      尚无软件包。上传本地构建，或配置更新源后检查版本。
                    </div>
                  )}
                </div>
              </section>
              <section className="infra-panel">
                <h2>安装分布</h2>
                <div className="infra-table-wrap">
                  <table className="infra-table">
                    <thead>
                      <tr>
                        <th>主机</th>
                        <th>当前版本</th>
                        <th>更新策略</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.installations
                        .filter((i) => i.software_id === software.id)
                        .map((i) => {
                          const p = data.policies.find(
                            (p) =>
                              p.host_id === i.host_id &&
                              p.software_id === i.software_id,
                          )
                          return (
                            <tr key={i.id}>
                              <td>
                                <Link to={`/pve/hosts/${i.host_id}`}>
                                  {data.hosts.find((h) => h.id === i.host_id)
                                    ?.name || i.host_id}
                                </Link>
                              </td>
                              <td>{i.version || i.detected_version}</td>
                              <td>
                                {p?.enabled
                                  ? p.auto_install
                                    ? '自动检查并部署'
                                    : '只检查'
                                  : '手动更新'}
                                {p?.pinned_release_id && ' · 版本已锁定'}
                              </td>
                            </tr>
                          )
                        })}
                    </tbody>
                  </table>
                </div>
                <p>在主机详情中配置维护窗口、自动升级和版本锁定。</p>
              </section>
            </>
          )}
        </div>
      </div>
      {editing && (
        <Modal
          title={editing.id ? '编辑软件与安装方案' : '新增软件'}
          onClose={() => setEditing(null)}
        >
          <Form
            onSubmit={async () => {
              const s = await saveSoftware({
                ...editing,
                version_args: versionArgs.trim().split(/\s+/).filter(Boolean),
              })
              setSelected(s.id)
              setEditing(null)
              await reload()
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
            <Field label="程序名（不含路径）">
              <input
                required
                pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,79}"
                value={editing.binary}
                onChange={(e) =>
                  setEditing({ ...editing, binary: e.target.value })
                }
              />
            </Field>
            <Field wide label="说明">
              <textarea
                value={editing.description}
                onChange={(e) =>
                  setEditing({ ...editing, description: e.target.value })
                }
              />
            </Field>
            <Field label="安装方案">
              <select
                value={editing.recipe}
                onChange={(e) =>
                  setEditing({ ...editing, recipe: e.target.value })
                }
              >
                <option value="binary">独立程序</option>
                <option value="aew">aew 工作站</option>
                <option value="codex">Codex CLI</option>
              </select>
            </Field>
            <Field label="版本检测参数（空格分隔）">
              <input
                value={versionArgs}
                onChange={(e) => setVersionArgs(e.target.value)}
              />
            </Field>
            <Field wide label="安装后健康检查命令（可选）">
              <input
                value={editing.health_command || ''}
                onChange={(e) =>
                  setEditing({ ...editing, health_command: e.target.value })
                }
              />
            </Field>
            <Field label="更新来源">
              <select
                value={editing.source_type || ''}
                onChange={(e) =>
                  setEditing({
                    ...editing,
                    source_type: e.target.value,
                    ...(e.target.value === '' ? { source_url: '' } : {}),
                  })
                }
              >
                <option value="">无在线更新源</option>
                <option value="manifest">HTTPS JSON 版本清单</option>
                <option value="github">GitHub Release</option>
              </select>
            </Field>
            <Field label="更新链接（可为空）">
              <input
                type="url"
                value={editing.source_url}
                onChange={(e) =>
                  setEditing({ ...editing, source_url: e.target.value })
                }
                placeholder="https://…"
              />
            </Field>
            {editing.source_type === 'github' && (
              <>
                <Field
                  wide
                  label="GitHub 安装包名称（{arch} 会替换为 x86_64 或 aarch64）"
                >
                  <input
                    required
                    value={editing.asset_pattern || ''}
                    onChange={(e) =>
                      setEditing({ ...editing, asset_pattern: e.target.value })
                    }
                  />
                </Field>
                <Field label="更新目标系统">
                  <select
                    value={editing.source_os || 'linux'}
                    onChange={(e) =>
                      setEditing({ ...editing, source_os: e.target.value })
                    }
                  >
                    <option>linux</option>
                    <option>darwin</option>
                    <option>windows</option>
                  </select>
                </Field>
                <Field label="更新目标架构">
                  <select
                    value={editing.source_arch || 'amd64'}
                    onChange={(e) =>
                      setEditing({ ...editing, source_arch: e.target.value })
                    }
                  >
                    <option>amd64</option>
                    <option>arm64</option>
                  </select>
                </Field>
              </>
            )}
            <Field wide label="更新源凭证（可选）">
              <select
                value={editing.source_secret_id || ''}
                onChange={(e) =>
                  setEditing({ ...editing, source_secret_id: e.target.value })
                }
              >
                <option value="">无需认证</option>
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
              安装包支持独立二进制、tar.gz 和
              zip。平台提取其中一个程序文件；需要额外运行库的软件应先在系统模板中准备依赖。
            </p>
          </Form>
        </Modal>
      )}
      {adding && software && (
        <Modal
          title={`新增 ${software.name} 版本`}
          onClose={() => setAdding(false)}
        >
          <div className="infra-actions">
            <label className="infra-check">
              <input
                type="radio"
                checked={upload}
                onChange={() => setUpload(true)}
              />
              从本地上传
            </label>
            <label className="infra-check">
              <input
                type="radio"
                checked={!upload}
                onChange={() => setUpload(false)}
              />
              登记在线安装包
            </label>
          </div>
          <Form
            submit={upload ? '上传并发布版本' : '保存版本'}
            onSubmit={async () => {
              if (upload && !file) throw new Error('请选择安装包')
              await perform(
                () =>
                  addRelease(
                    { ...release, software_id: software.id },
                    upload ? file : undefined,
                  ),
                '版本已发布',
              )
              setAdding(false)
              setFile(undefined)
            }}
          >
            <Field label="版本号">
              <input
                required
                value={release.version}
                onChange={(e) =>
                  setRelease({ ...release, version: e.target.value })
                }
              />
            </Field>
            <Field label="包格式">
              <select
                value={release.format}
                onChange={(e) =>
                  setRelease({ ...release, format: e.target.value })
                }
              >
                <option value="binary">独立二进制</option>
                <option value="tar.gz">tar.gz</option>
                <option value="zip">zip</option>
              </select>
            </Field>
            <Field label="系统">
              <select
                value={release.os}
                onChange={(e) => setRelease({ ...release, os: e.target.value })}
              >
                <option>linux</option>
                <option>windows</option>
                <option>darwin</option>
              </select>
            </Field>
            <Field label="架构">
              <select
                value={release.arch}
                onChange={(e) =>
                  setRelease({ ...release, arch: e.target.value })
                }
              >
                <option>amd64</option>
                <option>arm64</option>
              </select>
            </Field>
            {upload ? (
              <Field wide label="安装包（最大 512 MiB）">
                <input
                  required
                  type="file"
                  onChange={(e) => setFile(e.target.files?.[0])}
                />
              </Field>
            ) : (
              <Field wide label="安装包 HTTPS 地址">
                <input
                  required
                  type="url"
                  value={release.url}
                  onChange={(e) =>
                    setRelease({ ...release, url: e.target.value })
                  }
                />
              </Field>
            )}
            <Field
              wide
              label={`SHA256${upload ? '（可选，上传后自动计算）' : '（必填）'}`}
            >
              <input
                required={!upload}
                value={release.sha256}
                onChange={(e) =>
                  setRelease({ ...release, sha256: e.target.value })
                }
              />
            </Field>
            <Field wide label="程序在归档内的路径（留空按程序名查找）">
              <input
                value={release.entry}
                onChange={(e) =>
                  setRelease({ ...release, entry: e.target.value })
                }
              />
            </Field>
            <Field wide label="远程发布签名（Base64 Ed25519）"><input value={release.signature || ''} onChange={e => setRelease({ ...release, signature: e.target.value })} /></Field>
            <Field wide label="发布说明">
              <textarea
                value={release.notes}
                onChange={(e) =>
                  setRelease({ ...release, notes: e.target.value })
                }
              />
            </Field>
          </Form>
        </Modal>
      )}
      {deploying && (
        <Modal
          title={`部署 ${software?.name} ${deploying.version}`}
          onClose={() => setDeploying(null)}
        >
          <Form
            submit="验证并部署到所选主机"
            onSubmit={() => {
              if (!hosts.length) throw new Error('请选择主机')
              protect('批量部署软件', async () => {
                const failed: string[] = []
                for (const id of hosts) {
                  try {
                    await queueOperation({
                      action: 'install',
                      host_id: id,
                      release_id: deploying.id,
                      server_url: serverURL,
                    })
                  } catch (e) {
                    failed.push(
                      `${data.hosts.find((h) => h.id === id)?.name}: ${errorText(e)}`,
                    )
                  }
                }
                await reload()
                if (failed.length)
                  throw new Error(
                    `部分主机未排队：${failed.join('；')}。其余任务已创建，可在部署任务查看。`,
                  )
                setDeploying(null)
              })
            }}
          >
            <Field wide label="目标主机">
              <div className="infra-selection">
                {data.hosts
                  .filter(
                    (h) =>
                      !h.template &&
                      h.status !== 'missing' &&
                      h.os === deploying.os &&
                      h.arch === deploying.arch,
                  )
                  .map((h) => (
                    <label className="infra-check" key={h.id}>
                      <input
                        type="checkbox"
                        checked={hosts.includes(h.id)}
                        onChange={(e) =>
                          setHosts(
                            e.target.checked
                              ? [...hosts, h.id]
                              : hosts.filter((id) => id !== h.id),
                          )
                        }
                      />
                      {h.name} · {h.address || 'SSH 待配置'}
                    </label>
                  ))}
              </div>
            </Field>
            <Field wide label="Control Plane HTTP 地址（aew 首次注册使用）">
              <input
                type="url"
                value={serverURL}
                onChange={(e) => setServerURL(e.target.value)}
              />
            </Field>
          </Form>
        </Modal>
      )}
      {dialog}
    </section>
  )
}
