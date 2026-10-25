/** 基础设施 REST 客户端。令牌只放请求头，SSH 使用单次短时票据。 */
import { apiGet, apiPost, apiPut, apiRequest, getToken } from './client'

export type Cluster = {
  id: string
  name: string
  url: string
  secret_id: string
  ca_pem?: string
  nodes: Array<{
    node: string
    status: string
    cpu: number
    mem: number
    maxmem: number
  }>
  last_sync?: string
  last_error?: string
}
export type Host = {
  id: string
  name: string
  cluster_id?: string
  vmid?: number
  node?: string
  status: string
  template: boolean
  cpu: number
  cores: number
  memory: number
  max_memory: number
  disk: number
  max_disk: number
  address: string
  ssh_port: number
  ssh_user: string
  secret_id: string
  host_key: string
  os: string
  arch: string
  workstation_id?: string
}
export type Software = {
  id: string
  name: string
  description: string
  recipe: string
  binary: string
  version_args: string[]
  health_command?: string
  source_type: string
  source_url: string
  source_secret_id?: string
  asset_pattern?: string
  source_os?: string
  source_arch?: string
  last_checked?: string
  last_error?: string
}
export type Release = {
  id: string
  software_id: string
  version: string
  os: string
  arch: string
  format: string
  entry?: string
  url?: string
  sha256: string
  signature?: string
  size: number
  cached: boolean
  notes?: string
  created_at: string
}
export type Installation = {
  id: string
  host_id: string
  software_id: string
  release_id: string
  previous_release_id?: string
  version: string
  detected_version?: string
  status: string
  auth_status: string
  updated_at: string
}
export type Policy = {
  id: string
  host_id: string
  software_id: string
  enabled: boolean
  auto_install: boolean
  pinned_release_id: string
  start: string
  end: string
  timezone: string
  interval_minutes: number
  last_checked?: string
  available_release_id?: string
  last_error?: string
}
export type VMCreate = {
  cluster_id: string
  template_node: string
  template_id: number
  vmid: number
  node: string
  name: string
  cores: number
  memory_mb: number
  disk_gb?: number
  disk_device?: string
  arch?: string
  storage: string
  bridge: string
  ip_config: string
  ssh_user: string
  public_key: string
  address: string
  secret_id: string
  releases: string[]
  server_url: string
}
export type Spec = {
  action: string
  host_id?: string
  release_id?: string
  software_id?: string
  secret_id?: string
  server_url?: string
  create?: VMCreate
}
export type Operation = {
  id: string
  host_id: string
  actor: string
  spec: Spec
  status: string
  step: string
  log: string
  error?: string
  created_at: string
  updated_at: string
}
export type Credential = { id: string; name: string; kind: 'pve' | 'ssh' | 'api' }
export type Infrastructure = {
  clusters: Cluster[]
  hosts: Host[]
  software: Software[]
  releases: Release[]
  installations: Installation[]
  policies: Policy[]
  operations: Operation[]
  workstation_states: Record<string, string>
}
export const emptyInfrastructure: Infrastructure = {
  clusters: [],
  hosts: [],
  software: [],
  releases: [],
  installations: [],
  policies: [],
  operations: [],
  workstation_states: {},
}
const root = '/infrastructure'
export const getInfrastructure = () => apiGet<Infrastructure>(root)
export const saveCluster = (v: Partial<Cluster>) =>
  v.id
    ? apiPut<Cluster>(`${root}/clusters/${v.id}`, v)
    : apiPost<Cluster>(`${root}/clusters`, v)
export const syncCluster = (id: string) =>
  apiPost(`${root}/clusters/${id}/sync`)
export const saveHost = (v: Partial<Host>) =>
  v.id
    ? apiPut<Host>(`${root}/hosts/${v.id}`, v)
    : apiPost<Host>(`${root}/hosts`, v)
export const discoverAddress = (id: string) =>
  apiPost<Host>(`${root}/hosts/${id}/discover-address`)
export const probeHostKey = (id: string) =>
  apiPost<{ fingerprint: string }>(`${root}/hosts/${id}/probe-key`)
export const saveSoftware = (v: Partial<Software>) =>
  v.id
    ? apiPut<Software>(`${root}/software/${v.id}`, v)
    : apiPost<Software>(`${root}/software`, v)
export const checkSoftware = (id: string) =>
  apiPost(`${root}/software/${id}/check`)
export const savePolicy = (v: Policy) => apiPut<Policy>(`${root}/policies`, v)
export const queueOperation = (v: Spec, key = crypto.randomUUID()) =>
  apiRequest<Operation>(`${root}/operations`, {
    method: 'POST',
    json: v,
    headers: { 'Idempotency-Key': key },
  })
export const retryOperation = (id: string) =>
  apiPost<Operation>(`${root}/operations/${id}/retry`)
export const cancelOperation = (id: string) =>
  apiPost(`${root}/operations/${id}/cancel`)
export const listCredentials = () =>
  apiGet<{ items: Credential[] }>(`${root}/credentials`)
export const createCredential = (kind: string, name: string, value: string) =>
  apiPost(`${root}/credentials`, { kind, name, value })
export const revealCredential = (id: string) =>
  apiPost<{ value: string }>(`${root}/credentials/${id}/reveal`)
export const terminalTicket = (id: string) =>
  apiPost<{ ticket: string }>(`${root}/hosts/${id}/terminal-ticket`)
export async function addRelease(v: Partial<Release>, file?: File) {
  if (!file) return apiPost<Release>(`${root}/releases`, v)
  const body = new FormData()
  body.set('metadata', JSON.stringify(v))
  body.set('file', file)
  return apiRequest<Release>(`${root}/releases`, { method: 'POST', body })
}
export async function downloadRelease(v: Release) {
  const res = await fetch(`/api${root}/releases/${v.id}/download`, {
    headers: { Authorization: `Bearer ${getToken() || ''}` },
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || '下载失败')
  }
  const url = URL.createObjectURL(await res.blob())
  const a = document.createElement('a')
  a.href = url
  a.download = `${v.software_id}-${v.version}-${v.os}-${v.arch}${v.format === 'binary' ? '' : `.${v.format}`}`
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
