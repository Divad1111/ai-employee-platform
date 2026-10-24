/** 基础设施页面公共状态、表单和二次认证，避免在浏览器保存机密。 */
import {
  useCallback,
  useEffect,
  useState,
  type FormEvent,
  type ReactNode,
} from 'react'
import { apiPost } from '../../api/client'
import {
  emptyInfrastructure,
  getInfrastructure,
} from '../../api/infrastructure'
import { usePerm } from '../../stores/permissions'
import './infrastructure.css'

export function useInfrastructure() {
  const { canAll } = usePerm()
  const allowed = canAll('infra.read')
  const [data, setData] = useState(emptyInfrastructure)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const reload = useCallback(async () => {
    if (!allowed) return
    const next = await getInfrastructure()
    setData(next)
  }, [allowed])
  useEffect(() => {
    let current = true
    let loading = false
    const refresh = async () => {
      if (loading) return
      loading = true
      try {
        if (!allowed) return
        const next = await getInfrastructure()
        if (current) setData(next)
      } catch (e) {
        if (current) setError(errorText(e))
      } finally {
        loading = false
      }
    }
    void refresh()
    const timer = setInterval(() => void refresh(), 5000)
    return () => {
      current = false
      clearInterval(timer)
    }
  }, [allowed])
  const perform = async (fn: () => Promise<unknown>, msg = '操作已完成') => {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      await fn()
      await reload()
      setMessage(msg)
    } catch (e) {
      setError(errorText(e))
      throw e
    } finally {
      setBusy(false)
    }
  }
  return {
    data,
    error,
    setError,
    message,
    setMessage,
    busy,
    reload,
    perform,
    canAll,
    allowed,
  }
}
export function errorText(e: unknown) {
  return e instanceof Error ? e.message : String(e)
}
export function dateText(v?: string) {
  return v && !v.startsWith('0001-') ? new Date(v).toLocaleString() : '尚未执行'
}
export function bytes(v: number) {
  return v > 0 ? `${(v / 1024 ** 3).toFixed(1)} GiB` : '—'
}
const statuses: Record<string, string> = {
  QUEUED: '排队中',
  RUNNING: '执行中',
  WAITING: '等待条件',
  SUCCESS: '成功',
  FAILED: '失败',
  INTERRUPTED: '执行中断',
  CANCELLED: '已取消',
  running: '运行中',
  online: '在线',
  offline: '离线',
  stopped: '已停止',
  missing: 'PVE 中已不存在',
  external: '外部主机',
  ONLINE: '在线',
  OFFLINE: '离线',
  UNKNOWN: '未知',
  INSTALLED: '已安装',
  REQUIRED: '待登录',
  READY: '已认证',
  NOT_REQUIRED: '无需认证',
  ERROR: '安装异常',
}
export function stateLabel(value: string) {
  return statuses[value] || value || '未知'
}
export function State({ value }: { value: string }) {
  const good = ['SUCCESS', 'running', 'ONLINE', 'READY', 'INSTALLED'].includes(
    value,
  )
  const bad = ['FAILED', 'INTERRUPTED', 'missing', 'ERROR'].includes(value)
  return (
    <span className={`infra-state ${good ? 'good' : bad ? 'bad' : ''}`}>
      {stateLabel(value)}
    </span>
  )
}
export function Notice({
  error,
  message,
}: {
  error?: string
  message?: string
}) {
  return (
    <>
      {error && (
        <div className="infra-notice error" role="alert">
          {error}
        </div>
      )}
      {message && (
        <div className="infra-notice" role="status">
          {message}
        </div>
      )}
    </>
  )
}
export function Field({
  label,
  children,
  wide = false,
}: {
  label: string
  children: ReactNode
  wide?: boolean
}) {
  return (
    <label className={`infra-field ${wide ? 'wide' : ''}`}>
      <span>{label}</span>
      {children}
    </label>
  )
}
export function Modal({
  title,
  children,
  onClose,
}: {
  title: string
  children: ReactNode
  onClose: () => void
}) {
  return (
    <div className="infra-overlay">
      <section
        className="infra-modal"
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <header>
          <h2>{title}</h2>
          <button
            type="button"
            className="btn-ghost"
            onClick={onClose}
            aria-label="关闭"
          >
            关闭
          </button>
        </header>
        {children}
      </section>
    </div>
  )
}
export function Form({
  onSubmit,
  children,
  busy,
  submit = '保存',
}: {
  onSubmit: () => Promise<unknown> | void
  children: ReactNode
  busy?: boolean
  submit?: string
}) {
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const handle = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      await onSubmit()
    } catch (e) {
      setError(errorText(e))
    } finally {
      setSaving(false)
    }
  }
  return (
    <form onSubmit={(e) => void handle(e)}>
      <div className="infra-form-grid">{children}</div>
      <Notice error={error} />
      <footer>
        <button disabled={busy || saving} type="submit">
          {saving ? '处理中…' : submit}
        </button>
      </footer>
    </form>
  )
}

// 所有二次认证都由用户在管理后台交互完成；密码在请求完成后清除。
export function useProtectedAction() {
  const [action, setAction] = useState<{
    title: string
    run: () => Promise<unknown>
  } | null>(null)
  const [password, setPassword] = useState('')
  const [totp, setTotp] = useState('')
  const close = () => {
    setAction(null)
    setPassword('')
    setTotp('')
  }
  const protect = (title: string, run: () => Promise<unknown>) => {
    setPassword('')
    setTotp('')
    setAction({ title, run })
  }
  const dialog = action && (
    <Modal title={action.title} onClose={close}>
      <p>
        此操作需要验证管理员身份。已启用双因子认证时，请同时填写动态验证码。
      </p>
      <Form
        submit="验证并执行"
        onSubmit={async () => {
          await apiPost('/auth/step-up', {
            password,
            ...(totp ? { totp } : {}),
          })
          setPassword('')
          setTotp('')
          await action.run()
          close()
        }}
      >
        <Field label="管理员密码">
          <input
            required
            autoComplete="current-password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        <Field label="动态验证码（已启用时填写）">
          <input
            inputMode="numeric"
            autoComplete="one-time-code"
            value={totp}
            onChange={(e) => setTotp(e.target.value)}
          />
        </Field>
      </Form>
    </Modal>
  )
  return { protect, dialog }
}
