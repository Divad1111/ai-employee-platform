import { useEffect, useState } from 'react'
import {
  createCredential,
  listCredentials,
  revealCredential,
  type Credential,
} from '../../api/infrastructure'
import { usePerm } from '../../stores/permissions'
import {
  errorText,
  Field,
  Form,
  Modal,
  Notice,
  useProtectedAction,
} from './shared'

/** 只展示基础设施专用凭证；明文查看需要独立权限和二次认证。 */
export function CredentialsPanel({ onClose }: { onClose: () => void }) {
  const { canAll } = usePerm()
  const { protect, dialog } = useProtectedAction()
  const [items, setItems] = useState<Credential[]>([])
  const [error, setError] = useState('')
  const [kind, setKind] = useState('pve')
  const [name, setName] = useState('')
  const [value, setValue] = useState('')
  const [privateKey, setPrivateKey] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [revealed, setRevealed] = useState<{
    name: string
    value: string
  } | null>(null)
  const reload = async () => setItems((await listCredentials()).items)
  useEffect(() => {
    void reload().catch((e) => setError(errorText(e)))
  }, [])
  useEffect(() => {
    if (!revealed) return
    const timer = setTimeout(() => setRevealed(null), 30000)
    return () => clearTimeout(timer)
  }, [revealed])
  if (!canAll('infra.credential')) return null
  return (
    <Modal title="基础设施凭证" onClose={onClose}>
      <p>
        凭证加密保存在现有保险箱中，连接和部署任务只保存引用。查看后的明文将在
        30 秒后隐藏。
      </p>
      <Notice error={error} />
      <div className="infra-table-wrap">
        <table className="infra-table">
          <thead>
            <tr>
              <th>名称</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {items.map((v) => (
              <tr key={v.id}>
                <td>
                  {v.name}
                  <small>{v.id}</small>
                </td>
                <td>
                  <button
                    className="btn-ghost"
                    onClick={() =>
                      protect('查看机密凭证', async () => {
                        const result = await revealCredential(v.id)
                        setRevealed({ name: v.name, value: result.value })
                      })
                    }
                  >
                    查看 / 复制
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <h3>新增凭证</h3>
      <Form
        submit="验证并保存到保险箱"
        onSubmit={() => {
          const payload =
            kind === 'ssh'
              ? JSON.stringify({
                  password: value,
                  private_key: privateKey,
                  passphrase,
                })
              : value
          protect('保存基础设施凭证', async () => {
            await createCredential(kind, name, payload)
            setValue('')
            setPrivateKey('')
            setPassphrase('')
            setName('')
            await reload()
          })
        }}
      >
        <Field label="凭证类型">
          <select
            value={kind}
            onChange={(e) => {
              setKind(e.target.value)
              setValue('')
              setPrivateKey('')
              setPassphrase('')
            }}
          >
            <option value="pve">PVE API Token</option>
            <option value="ssh">SSH 密码 / 私钥</option>
            <option value="api">软件源 / Codex API Key</option>
          </select>
        </Field>
        <Field label="名称（字母、数字、点、横线）">
          <input
            required
            pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,79}"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field
          wide
          label={
            kind === 'pve'
              ? 'Token（user@realm!tokenid=secret）'
              : kind === 'ssh'
                ? 'SSH 密码（使用私钥时可留空）'
                : 'API Key / Token'
          }
        >
          <input
            required={kind !== 'ssh'}
            type="password"
            autoComplete="new-password"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        </Field>
        {kind === 'ssh' && (
          <>
            <Field wide label="SSH 私钥（使用密码时可留空）">
              <textarea
                value={privateKey}
                onChange={(e) => setPrivateKey(e.target.value)}
              />
            </Field>
            <Field wide label="私钥口令（可选）">
              <input
                type="password"
                autoComplete="new-password"
                value={passphrase}
                onChange={(e) => setPassphrase(e.target.value)}
              />
            </Field>
          </>
        )}
      </Form>
      {dialog}
      {revealed && (
        <Modal title={revealed.name} onClose={() => setRevealed(null)}>
          <pre className="infra-log">{revealed.value}</pre>
          <button
            onClick={() =>
              void navigator.clipboard
                .writeText(revealed.value)
                .catch((e) => setError(errorText(e)))
            }
          >
            复制
          </button>
        </Modal>
      )}
    </Modal>
  )
}
