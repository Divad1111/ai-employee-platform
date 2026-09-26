/**
 * RBAC / 配额相关中文展示文案
 */

export const SCOPE_OPTIONS = [
  { value: 'ALL', label: '全部资源 (ALL)' },
  { value: 'OWN', label: '仅本人 (OWN)' },
  { value: 'ASSIGNED', label: '已分配 (ASSIGNED)' },
  { value: 'NONE', label: '无权限 (NONE)' },
] as const

export const SCOPE_LABEL: Record<string, string> = {
  ALL: '全部资源',
  OWN: '仅本人',
  ASSIGNED: '已分配',
  NONE: '无权限',
}

export const RESOURCE_TYPE_OPTIONS = [
  { value: 'ROLE', label: '角色预设' },
  { value: 'USER', label: '用户（例外）' },
  { value: 'USER_BONUS', label: '用户（额外）' },
  { value: 'WORKSTATION', label: '工作站' },
  { value: 'DIGITAL_EMPLOYEE', label: '数字员工' },
] as const

export const RESOURCE_TYPE_LABEL: Record<string, string> = {
  ROLE: '角色预设',
  USER: '用户（例外）',
  USER_BONUS: '用户（额外）',
  WORKSTATION: '工作站',
  DIGITAL_EMPLOYEE: '数字员工',
}

export function roleDisplayName(name: string, description?: string) {
  if (description && description.trim()) return description
  const map: Record<string, string> = {
    SUPER_ADMIN: '超级管理员',
    ADMIN: '管理员',
    OPERATOR: '操作员',
    VIEWER: '只读',
  }
  return map[name] || name
}

const PERM_DESCRIPTIONS: Record<string, string> = {
  'backup.view': '查看备份策略、存储目标与记录',
  'backup.create': '手动执行系统备份',
  'backup.manage': '管理备份策略与调度设置',
  'backup.destination': '管理备份存储目标',
  'backup.verify': '校验备份文件完整性',
  'backup.delete': '删除备份历史产物',
  'backup.restore': '执行系统全量容灾恢复',
}

export function permLabel(code: string, description?: string) {
  if (description && description.trim() && description !== code) {
    return `${description}（${code}）`
  }
  if (PERM_DESCRIPTIONS[code]) {
    return `${PERM_DESCRIPTIONS[code]}（${code}）`
  }
  return code
}

