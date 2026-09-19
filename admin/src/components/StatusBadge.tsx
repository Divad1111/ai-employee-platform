interface StatusBadgeProps {
  status: string
  className?: string
}

const STATUS_MAP: Record<string, { label: string; type: 'success' | 'warning' | 'danger' | 'info' | 'neutral' }> = {
  // 通用状态
  ONLINE: { label: '在线', type: 'success' },
  OFFLINE: { label: '离线', type: 'danger' },
  READY: { label: '就绪', type: 'success' },
  BUSY: { label: '繁忙', type: 'warning' },
  ACTIVE: { label: '有效', type: 'success' },
  DISABLED: { label: '已停用', type: 'neutral' },
  REVOKED: { label: '已吊销', type: 'danger' },
  UNKNOWN: { label: '未知状态', type: 'neutral' },

  // 任务 (Job) 状态
  CREATED: { label: '已创建', type: 'neutral' },
  QUEUED: { label: '排队中', type: 'info' },
  ASSIGNED: { label: '已分配节点', type: 'info' },
  STARTING: { label: '启动中', type: 'warning' },
  RUNNING: { label: '运行中', type: 'success' },
  SUCCESS: { label: '执行成功', type: 'success' },
  FAILED: { label: '执行失败', type: 'danger' },
  CANCELLED: { label: '已取消', type: 'neutral' },
  TIMEOUT: { label: '执行超时', type: 'danger' },
  BLOCKED: { label: '已受阻', type: 'warning' },
  WAITING_APPROVAL: { label: '待审批', type: 'warning' },

  // 权限与审批状态
  ALLOW: { label: '允许 (ALLOW)', type: 'success' },
  DENY: { label: '拒绝 (DENY)', type: 'danger' },
  ASK: { label: '需审批 (ASK)', type: 'warning' },
  PENDING: { label: '待审核', type: 'warning' },
  APPROVED: { label: '已批准', type: 'success' },
  REJECTED: { label: '已驳回', type: 'danger' },
  CRITICAL: { label: '高风险', type: 'danger' },
}

export function StatusBadge({ status, className = '' }: StatusBadgeProps) {
  const normalized = (status || '').toUpperCase()
  const cfg = STATUS_MAP[normalized] || { label: status || '—', type: 'neutral' }

  return (
    <span className={`status-pill status-${cfg.type} ${className}`}>
      <span className="status-dot" />
      {cfg.label}
    </span>
  )
}
