/**
 * 轻量提示弹窗（替代浏览器 alert）
 */
export type AlertDialogProps = {
  open: boolean
  title?: string
  message: string
  onClose: () => void
}

export function AlertDialog({ open, title = '提示', message, onClose }: AlertDialogProps) {
  if (!open) return null
  return (
    <div className="auto-modal-backdrop" role="presentation" onClick={onClose}>
      <div
        className="auto-modal-card"
        role="dialog"
        aria-modal="true"
        aria-labelledby="alert-dialog-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="auto-modal-head">
          <h3 id="alert-dialog-title">{title}</h3>
        </div>
        <p className="auto-modal-desc">{message}</p>
        <div className="auto-modal-actions">
          <button type="button" className="btn-success" onClick={onClose}>
            知道了
          </button>
        </div>
      </div>
    </div>
  )
}
