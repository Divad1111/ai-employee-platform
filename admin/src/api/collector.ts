import { apiDelete, apiGet, apiPost, apiPut } from './client'

export type CollectorProfile = {
  user_id?: string; display_name: string; aliases: string[]; timezone: string
  jira_url: string; jira_account: string; jira_auth: string; meego_user_key: string; meego_url: string
  feishu_app_id: string; feishu_open_id?: string; feishu_name?: string; scopes: string
  expires_at?: string; refresh_expires_at?: string; notify_enabled: boolean; notify_chat_id: string
  authorized?: boolean; needs_reauthorization?: boolean; has_refresh_token?: boolean
  has_app_secret?: boolean; has_jira_token?: boolean; has_meego_token?: boolean; has_bot_webhook?: boolean
  platform_app_available?: boolean; redirect_uri?: string
  granted_scopes?: string; capabilities?: { name: string; status: string }[]
  meego_plugin_id?: string; has_meego_secret?: boolean
}
export type CollectorTarget = {
  id: string; owner_id?: string; name: string; source: string; enabled: boolean; url: string
  interval_minutes: number; cron: string; timezone: string; notify_baseline: boolean
  config: Record<string, any>; version: number; updated_at?: string
}
export type CollectorCondition = { field: string; operator: string; value: string }
export type CollectorRule = {
  id: string; name: string; enabled: boolean; priority: number; source: string; target_id: string
  match: string; conditions: CollectorCondition[]; action: string; aggregate_minutes: number
}
export type WorkItem = {
  id: string; target_id: string; external_id?: string; source: string; title: string; url: string
  status: string; done: boolean; due_date: string; assignees: string[]; fields: Record<string, unknown>
  updated_at: string; content?: string
}
export type WorkEvent = {
  id: string; item: WorkItem; event_kind: string; changed_fields: string[]
  occurred_at: string; reason: string; action: string
}
export type CollectorRun = {
  id: string; target_id: string; target_name: string; source: string; status: string; preview: boolean
  started_at: string; finished_at: string; next_at: string; count: number; changes: number
  error?: string; events: WorkEvent[]; samples: WorkItem[]
}
export type Delivery = { id: string; status: string; attempts: number; next_at: string; error?: string; events: WorkEvent[] }

const ownerQuery = (owner?: string) => owner ? `?owner_id=${encodeURIComponent(owner)}` : ''
export const getCollectorProfile = () => apiGet<CollectorProfile>('/collector/profile')
export const saveCollectorProfile = (profile: Record<string, unknown>) => apiPut<CollectorProfile>('/collector/profile', profile)
export const beginFeishuOAuth = () => apiPost<{ authorize_url: string; redirect_uri: string }>('/collector/oauth/start')
export const collectorRecords = <T>(kind: string, owner?: string) =>
  apiGet<{ items: T[]; total: number }>(`/collector/records/${kind}${ownerQuery(owner)}`)
export const collectorItems = (owner?: string) => apiGet<{ items: WorkItem[] }>(`/collector/items${ownerQuery(owner)}`)
export const saveTarget = (target: CollectorTarget, owner?: string) => target.id
  ? apiPut<CollectorTarget>(`/collector/targets/${encodeURIComponent(target.id)}${ownerQuery(owner)}`, target)
  : apiPost<CollectorTarget>(`/collector/targets${ownerQuery(owner)}`, target)
export const saveRule = (rule: CollectorRule, owner?: string) => rule.id
  ? apiPut<CollectorRule>(`/collector/rules/${encodeURIComponent(rule.id)}${ownerQuery(owner)}`, rule)
  : apiPost<CollectorRule>(`/collector/rules${ownerQuery(owner)}`, rule)
export const deleteCollectorRecord = (kind: string, id: string, owner?: string) =>
  apiDelete(`/collector/records/${kind}/${encodeURIComponent(id)}${ownerQuery(owner)}`)
export const previewTarget = (target: CollectorTarget, owner?: string) => apiPost<CollectorRun>(`/collector/preview${ownerQuery(owner)}`, target)
export const scanTarget = (id: string, owner?: string) => apiPost<CollectorRun>(`/collector/targets/${encodeURIComponent(id)}/scan${ownerQuery(owner)}`)
export const discoverTarget = (target: CollectorTarget, owner?: string) => apiPost<Record<string, any>>(`/collector/discover${ownerQuery(owner)}`, target)
export const previewRule = (rule: CollectorRule, owner?: string) => apiPost<{ items: WorkEvent[] }>(`/collector/rules/preview${ownerQuery(owner)}`, rule)
export const retryDelivery = (id: string, owner?: string) => apiPost(`/collector/deliveries/${encodeURIComponent(id)}/retry${ownerQuery(owner)}`)
export const importCollector = (kind: string, content: string, apply: boolean) =>
  apiPost<{ targets?: CollectorTarget[]; rules?: CollectorRule[]; person?: unknown; warnings?: string[]; applied: boolean }>('/collector/import', { kind, content, apply })
