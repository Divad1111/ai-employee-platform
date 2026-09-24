/**
 * 通用确认弹窗（替代浏览器 confirm）
 */
import type { ReactNode } from 'react'
import { IconAlertTriangle } from './Icons'

export type ConfirmDialogProps = {
  open: boolean
  title: string
  description?: ReactNode
  /** 高亮展示的目标名称/摘要 */
  targetLabel?: string
  targetMeta?: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
  busy?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({
  open,
  title,
  description,
  targetLabel,
  targetMeta,
  confirmText = '确认',
  cancelText = '取消',
  danger = true,
  busy,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  if (!open) return null
  return (
    <div className="auto-modal-backdrop" role="presentation" onClick={onCancel}>
      <div
        className="auto-modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="auto-modal-head">
          {danger ? (
            <IconAlertTriangle size={22} style={{ color: '#ef4444', flexShrink: 0 }} />
          ) : null}
          <h3 id="confirm-dialog-title">{title}</h3>
        </div>
        {description ? <p className="auto-modal-desc">{description}</p> : null}
        {targetLabel || targetMeta ? (
          <div className="auto-modal-target">
            {targetLabel ? <strong>{targetLabel}</strong> : null}
            {targetMeta ? <code className="mono">{targetMeta}</code> : null}
          </div>
        ) : null}
        <div className="auto-modal-actions">
          <button type="button" className="btn-ghost" onClick={onCancel} disabled={busy}>
            {cancelText}
          </button>
          <button
            type="button"
            className={danger ? 'btn-danger' : 'btn-success'}
            onClick={onConfirm}
            disabled={busy}
          >
            {busy ? '处理中…' : confirmText}
          </button>
        </div>
      </div>
    </div>
  )
}
