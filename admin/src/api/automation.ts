/**
 * 自动化任务 API 客户端。
 */
import { apiDelete, apiGet, apiPatch, apiPost, apiPut } from './client'

export type Automation = {
  id: string
  name: string
  trigger_type: 'cron' | 'calendar' | 'webhook'
  enabled: boolean
  employee_id: string
  prompt: string
  timezone: string
  notify_chat_id: string
  trigger_config: Record<string, unknown>
  last_fired_at?: string
  created_by?: string
  created_at?: string
  updated_at?: string
}

export type CalendarItem = {
  id: string
  automation_id: string
  run_date: string
  seq: number
  employee_id: string
  prompt: string
  enabled: boolean
}

export type AutomationRun = {
  id: string
  automation_id: string
  calendar_item_id?: string
  job_id?: string
  status: string
  trigger_source: string
  idempotency_key: string
  error?: string
  created_at: string
  finished_at?: string
}

export function listAutomations(type?: string) {
  const q = type ? `?type=${encodeURIComponent(type)}` : ''
  return apiGet<{ items: Automation[] }>(`/automations${q}`)
}

export function createAutomation(body: Record<string, unknown>) {
  return apiPost<{ item: Automation; secrets?: Record<string, string> }>('/automations', body)
}

export function updateAutomation(id: string, body: Record<string, unknown>) {
  return apiPatch<Automation>(`/automations/${id}`, body)
}

export function deleteAutomation(id: string) {
  return apiDelete<{ status: string }>(`/automations/${id}`)
}

export function listCalendarItems(id: string, date?: string) {
  const q = date ? `?date=${encodeURIComponent(date)}` : ''
  return apiGet<{ items: CalendarItem[] }>(`/automations/${id}/calendar-items${q}`)
}

export function putCalendarItems(id: string, date: string, items: Array<{ employee_id: string; prompt: string; enabled?: boolean }>) {
  return apiPut<{ items: CalendarItem[] }>(`/automations/${id}/calendar-items`, { date, items })
}

export function listRuns(id: string, limit = 30) {
  return apiGet<{ items: AutomationRun[] }>(`/automations/${id}/runs?limit=${limit}`)
}

export function rotateSecrets(id: string) {
  return apiPost<{ secrets: Record<string, string> }>(`/automations/${id}/rotate-secrets`)
}
