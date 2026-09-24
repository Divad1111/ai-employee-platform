/**
 * 通用输入弹窗（替代浏览器 prompt）
 */
import { FormEvent, useEffect, useState } from 'react'

export type PromptDialogProps = {
  open: boolean
  title: string
  description?: string
  label?: string
  defaultValue?: string
  placeholder?: string
  confirmText?: string
  cancelText?: string
  busy?: boolean
  error?: string
  onConfirm: (value: string) => void
  onCancel: () => void
}

export function PromptDialog({
  open,
  title,
  description,
  label = '内容',
  defaultValue = '',
  placeholder,
  confirmText = '确认',
  cancelText = '取消',
  busy,
  error,
  onConfirm,
  onCancel,
}: PromptDialogProps) {
  const [value, setValue] = useState(defaultValue)

  useEffect(() => {
    if (open) setValue(defaultValue)
  }, [open, defaultValue])

  if (!open) return null

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const v = value.trim()
    if (!v) return
    onConfirm(v)
  }

  return (
    <div className="auto-modal-backdrop" role="presentation" onClick={onCancel}>
      <div
        className="auto-modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="prompt-dialog-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="auto-modal-head">
          <h3 id="prompt-dialog-title">{title}</h3>
        </div>
        {description ? <p className="auto-modal-desc">{description}</p> : null}
        <form className="stack-form" style={{ padding: 0, maxWidth: 'none' }} onSubmit={submit}>
          <label>
            <span className="field-caption">{label}</span>
            <input
              autoFocus
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder={placeholder}
              disabled={busy}
            />
          </label>
          {error ? <div className="error">{error}</div> : null}
          <div className="auto-modal-actions" style={{ marginTop: '0.25rem' }}>
            <button type="button" className="btn-ghost" onClick={onCancel} disabled={busy}>
              {cancelText}
            </button>
            <button type="submit" className="btn-success" disabled={busy || !value.trim()}>
              {busy ? '保存中…' : confirmText}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
