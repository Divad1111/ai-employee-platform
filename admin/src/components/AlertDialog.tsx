/**
 * 轻量提示弹窗（替代浏览器 alert）
 */
import { useEffect, useId, useRef } from 'react'
import { createPortal } from 'react-dom'
import { IconAlertTriangle } from './Icons'

export type AlertDialogProps = {
  open: boolean
  title?: string
  message: string
  variant?: 'info' | 'error'
  onClose: () => void
}

export function AlertDialog({ open, title = '提示', message, variant = 'info', onClose }: AlertDialogProps) {
  const titleId = useId()
  const messageId = useId()
  const closeButton = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    const previousFocus = document.activeElement as HTMLElement | null
    closeButton.current?.focus()
    return () => { previousFocus?.focus() }
  }, [open])
  if (!open) return null
  return createPortal(
    <div className="auto-modal-backdrop" role="presentation" onClick={onClose}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.stopPropagation()
          onClose()
        }
        if (event.key === 'Tab') {
          event.preventDefault()
          closeButton.current?.focus()
        }
      }}>
      <div
        className={`auto-modal-card${variant === 'error' ? ' alert-dialog-error' : ''}`}
        role={variant === 'error' ? 'alertdialog' : 'dialog'}
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={messageId}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="auto-modal-head">
          {variant === 'error' && <span className="alert-dialog-icon"><IconAlertTriangle size={24} /></span>}
          <h3 id={titleId}>{title}</h3>
        </div>
        <p id={messageId} className="auto-modal-desc">{message}</p>
        <div className="auto-modal-actions">
          <button ref={closeButton} type="button" className={variant === 'error' ? 'btn-primary' : 'btn-success'} onClick={onClose}>
            知道了
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
