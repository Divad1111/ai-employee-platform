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

export function permLabel(code: string, description?: string) {
  if (description && description.trim() && description !== code) {
    return `${description}（${code}）`
  }
  return code
}
