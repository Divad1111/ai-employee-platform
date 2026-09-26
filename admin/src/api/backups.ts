/**
 * 备份与容灾恢复 API 客户端
 * 设计依据：docs/AI_Employee_Center_Server_Backup_Design.md §51
 */
import { apiDelete, apiGet, apiPost, apiPut } from './client'

export type DestinationType = 'LOCAL' | 'S3' | 'SFTP' | 'SMB'

export type BackupDestination = {
  id: string
  name: string
  type: DestinationType
  enabled: boolean
  config?: any
  last_test_at?: string
  last_test_status?: 'SUCCESS' | 'FAILED' | ''
  last_test_message?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export type BackupPolicy = {
  id: string
  name: string
  enabled: boolean
  scope: string
  schedule_type: 'MANUAL' | 'CRON'
  cron_expression: string
  timezone: string
  retention_keep_last: number
  compression_algorithm: 'zstd' | 'gzip'
  compression_level: number
  encryption_enabled: boolean // 是否加密开关
  encryption_algorithm: string
  encryption_key_version: number
  destination_ids: string[]
  next_run_at?: string
  last_run_at?: string
  last_run_status?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export type RunDestination = {
  id: string
  backup_run_id: string
  destination_id: string
  destination_name?: string
  destination_type?: DestinationType
  status: 'PENDING' | 'RUNNING' | 'UPLOADING' | 'VERIFYING' | 'SUCCESS' | 'FAILED'
  remote_path: string
  remote_size: number
  uploaded_at?: string
  verified_at?: string
  error_code?: string
  error_message?: string
}

export type BackupRun = {
  id: string
  policy_id?: string
  policy_name?: string
  backup_id: string
  scope: string
  status:
    | 'PENDING'
    | 'RUNNING'
    | 'PACKAGING'
    | 'ENCRYPTING'
    | 'UPLOADING'
    | 'VERIFYING'
    | 'SUCCESS'
    | 'PARTIAL_SUCCESS'
    | 'FAILED'
    | 'CANCELLED'
  trigger_type: 'MANUAL' | 'SCHEDULED' | 'EMERGENCY'
  started_at?: string
  completed_at?: string
  duration_ms: number
  artifact_size: number
  file_count: number
  checksum: string
  manifest?: any
  destinations?: RunDestination[]
  error_code?: string
  error_message?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export type RestoreJob = {
  id: string
  backup_run_id: string
  emergency_backup_run_id?: string
  status: 'PENDING' | 'RUNNING' | 'RESTORING_DB' | 'RESTORING_DATA' | 'VERIFYING' | 'SUCCESS' | 'FAILED'
  started_at?: string
  completed_at?: string
  created_by?: string
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
}

export type BackupOverview = {
  total_runs: number
  success_runs: number
  failed_runs: number
  partial_runs: number
  total_policies: number
  active_policies: number
  total_destinations: number
  total_artifact_size: number
  last_run_at?: string
  last_run_status?: string
  last_success_run_at?: string
  next_scheduled_at?: string
}

export type VerificationReport = {
  backup_id: string
  checksum: string
  expected_hash: string
  passed: boolean
  file_count: number
  server_version: string
  encryption: string
  compression: string
  details: string[]
}

// API 调用方法

export function getBackupOverview(): Promise<BackupOverview> {
  return apiGet<BackupOverview>('/backups/overview')
}

// Destinations
export function listBackupDestinations(): Promise<{ items: BackupDestination[] }> {
  return apiGet<{ items: BackupDestination[] }>('/backups/destinations')
}

export function createBackupDestination(data: {
  name: string
  type: DestinationType
  config: any
}): Promise<BackupDestination> {
  return apiPost<BackupDestination>('/backups/destinations', data)
}

export function updateBackupDestination(
  id: string,
  data: { name?: string; type?: DestinationType; enabled?: boolean; config?: any }
): Promise<BackupDestination> {
  return apiPut<BackupDestination>(`/backups/destinations/${id}`, data)
}

export function deleteBackupDestination(id: string): Promise<{ status: string }> {
  return apiDelete<{ status: string }>(`/backups/destinations/${id}`)
}

export function testBackupDestination(id: string): Promise<{ success: boolean; message?: string; error?: string }> {
  return apiPost<{ success: boolean; message?: string; error?: string }>(`/backups/destinations/${id}/test`)
}

// Policies
export function listBackupPolicies(): Promise<{ items: BackupPolicy[] }> {
  return apiGet<{ items: BackupPolicy[] }>('/backups/policies')
}

export function createBackupPolicy(data: Partial<BackupPolicy>): Promise<BackupPolicy> {
  return apiPost<BackupPolicy>('/backups/policies', data)
}

export function updateBackupPolicy(id: string, data: Partial<BackupPolicy>): Promise<BackupPolicy> {
  return apiPut<BackupPolicy>(`/backups/policies/${id}`, data)
}

export function deleteBackupPolicy(id: string): Promise<{ status: string }> {
  return apiDelete<{ status: string }>(`/backups/policies/${id}`)
}

export function enableBackupPolicy(id: string): Promise<{ status: string; enabled: boolean }> {
  return apiPost<{ status: string; enabled: boolean }>(`/backups/policies/${id}/enable`)
}

export function disableBackupPolicy(id: string): Promise<{ status: string; enabled: boolean }> {
  return apiPost<{ status: string; enabled: boolean }>(`/backups/policies/${id}/disable`)
}

export function runBackupPolicy(id: string): Promise<{ status: string; message: string }> {
  return apiPost<{ status: string; message: string }>(`/backups/policies/${id}/run`)
}

export function runManualBackup(data: {
  destination_ids: string[]
  encryption_enabled: boolean
}): Promise<{ status: string; message: string }> {
  return apiPost<{ status: string; message: string }>('/backups/manual', data)
}

// Runs
export function listBackupRuns(params?: {
  limit?: number
  offset?: number
  status?: string
  policy_id?: string
}): Promise<{ items: BackupRun[]; total: number }> {
  const q = new URLSearchParams()
  if (params?.limit) q.set('limit', String(params.limit))
  if (params?.offset) q.set('offset', String(params.offset))
  if (params?.status) q.set('status', params.status)
  if (params?.policy_id) q.set('policy_id', params.policy_id)
  return apiGet<{ items: BackupRun[]; total: number }>(`/backups/runs?${q.toString()}`)
}

export function getBackupRun(id: string): Promise<BackupRun> {
  return apiGet<BackupRun>(`/backups/runs/${id}`)
}

export function verifyBackupRun(id: string): Promise<{ passed: boolean; report: VerificationReport; error?: string }> {
  return apiPost<{ passed: boolean; report: VerificationReport; error?: string }>(`/backups/runs/${id}/verify`)
}

export function deleteBackupRun(id: string): Promise<{ status: string }> {
  return apiDelete<{ status: string }>(`/backups/runs/${id}`)
}

// Restore
export function restoreBackupRun(id: string): Promise<RestoreJob> {
  return apiPost<RestoreJob>(`/backups/runs/${id}/restore`)
}

export function listRestoreJobs(): Promise<{ items: RestoreJob[] }> {
  return apiGet<{ items: RestoreJob[] }>('/backups/restore-jobs')
}

export function getRestoreJob(id: string): Promise<RestoreJob> {
  return apiGet<RestoreJob>(`/backups/restore-jobs/${id}`)
}
