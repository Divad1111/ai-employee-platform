import { useState } from 'react'
import { Link } from 'react-router-dom'
import { cancelOperation, retryOperation } from '../../api/infrastructure'
import {
  dateText,
  errorText,
  Modal,
  Notice,
  State,
  stateLabel,
  useInfrastructure,
  useProtectedAction,
} from './shared'

const actions: Record<string, string> = {
  clone: '创建虚拟机',
  install: '安装 / 升级',
  rollback: '回滚',
  inspect: '软件探测',
  login: '软件登录',
  start: '开机',
  shutdown: '关机',
  reboot: '重启',
}
export function DeploymentsPage() {
  const { data, error, setError, message, reload, perform, canAll, allowed } =
    useInfrastructure()
  const { protect, dialog } = useProtectedAction()
  const [selected, setSelected] = useState('')
  const [status, setStatus] = useState('')
  if (!allowed) return <p>需要基础设施查看权限。</p>
  const operation = data.operations.find((o) => o.id === selected)
  return (
    <section className="infra-page">
      <header className="infra-toolbar">
        <div>
          <h1>部署任务</h1>
          <p>
            任务在服务端持续执行。网络中断后，过期的执行租约会标记为中断，核实远端状态后可重试。
          </p>
        </div>
        <div className="infra-actions">
          <Link to="/pve">PVE 管理</Link>
          <Link to="/software">软件管理</Link>
          <button
            className="btn-ghost"
            onClick={() => void reload().catch((e) => setError(errorText(e)))}
          >
            刷新
          </button>
        </div>
      </header>
      <Notice error={error} message={message} />
      <div className="infra-actions">
        <label>
          状态{' '}
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">全部</option>
            {[
              'QUEUED',
              'RUNNING',
              'WAITING',
              'SUCCESS',
              'FAILED',
              'INTERRUPTED',
              'CANCELLED',
            ].map((s) => (
              <option key={s} value={s}>
                {stateLabel(s)}
              </option>
            ))}
          </select>
        </label>
        <span className="infra-muted">
          自动刷新 · 每台主机同时只执行一个任务
        </span>
      </div>
      <section className="infra-panel">
        <div className="infra-table-wrap">
          <table className="infra-table">
            <thead>
              <tr>
                <th>任务</th>
                <th>主机</th>
                <th>状态 / 步骤</th>
                <th>创建时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {data.operations
                .filter((o) => !status || o.status === status)
                .map((o) => (
                  <tr key={o.id}>
                    <td>
                      {actions[o.spec.action] || o.spec.action}
                      <small>{o.spec.software_id}</small>
                    </td>
                    <td>
                      <Link to={`/pve/hosts/${o.host_id}`}>
                        {data.hosts.find((h) => h.id === o.host_id)?.name ||
                          o.spec.create?.name ||
                          o.host_id}
                      </Link>
                    </td>
                    <td>
                      <State value={o.status} />
                      <small>{o.step}</small>
                      {o.error && (
                        <small style={{ color: '#9f1239' }}>{o.error}</small>
                      )}
                    </td>
                    <td>{dateText(o.created_at)}</td>
                    <td>
                      <div className="infra-actions">
                        <button
                          className="btn-ghost"
                          onClick={() => setSelected(o.id)}
                        >
                          日志
                        </button>
                        {canAll('infra.deploy') &&
                          ['FAILED', 'INTERRUPTED'].includes(o.status) && (
                            <button
                              className="btn-ghost"
                              onClick={() =>
                                protect('核对远端状态后重试任务', () =>
                                  perform(
                                    () => retryOperation(o.id),
                                    '任务已重新排队',
                                  ),
                                )
                              }
                            >
                              重试
                            </button>
                          )}
                        {canAll('infra.deploy') &&
                          ['QUEUED', 'WAITING'].includes(o.status) && (
                            <button
                              className="btn-ghost"
                              onClick={() =>
                                protect('取消等待中的部署任务', () =>
                                  perform(
                                    () => cancelOperation(o.id),
                                    '任务已取消，远端资源保留',
                                  ),
                                )
                              }
                            >
                              取消
                            </button>
                          )}
                      </div>
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
          {data.operations.length === 0 && (
            <div className="infra-empty">
              尚无部署任务。从虚拟机详情或软件版本页发起部署。
            </div>
          )}
        </div>
      </section>
      {operation && (
        <Modal
          title={`部署日志 · ${actions[operation.spec.action] || operation.spec.action}`}
          onClose={() => setSelected('')}
        >
          <State value={operation.status} />
          <p>{operation.step}</p>
          <pre className="infra-log">
            {operation.log || '任务尚未开始执行。'}
            {operation.error ? `\n${operation.error}` : ''}
          </pre>
          <small>任务 ID {operation.id}</small>
        </Modal>
      )}
      {dialog}
    </section>
  )
}
