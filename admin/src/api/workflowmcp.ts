/** 工作流MCP 前端类型与 API */
import { apiDelete, apiGet, apiPost, apiPut, getToken, setToken } from './client'
import { rebuildSavedIndex } from './savedIndex'

export type Workflow = {
  id: string
  name: string
  version: string
  description: string
  when_to_use?: string[]
  inputs?: string[]
  skills?: string[]
  knowledge?: string[]
  steps?: { id: string; description: string }[]
  approval?: Record<string, boolean>
  status?: string
}

export type SkillPackage = {
  id: string
  cursor_name: string
  name: string
  version: string
  description: string
  content_hash?: string
  revision?: number
  files?: { path: string; sha256?: string; size_bytes?: number }[]
}

export type KnowledgeDoc = {
  id: string
  path: string
  namespace: string
  title: string
  source: string
  content: string
  version: string
}

export type MCPToken = {
  id: string
  subject_type: string
  subject_id: string
  scope: string
  label: string
  created_at: string
  revoked_at?: string
}

export async function listWorkflows() {
  return apiGet<{ items: Workflow[] }>('/workflow-mcp/workflows')
}

export async function getWorkflow(id: string) {
  return apiGet<{ workflow: Workflow; yaml: string }>(`/workflow-mcp/workflows/${encodeURIComponent(id)}`)
}

export async function upsertWorkflow(yaml: string, bump = 'patch') {
  const saved = await apiPost<{ status: string; action: string; workflow: Workflow }>('/workflow-mcp/workflows', { yaml, bump })
  return rebuildSavedIndex(saved, reindexKnowledge)
}

export async function deleteWorkflow(id: string) {
  return apiDelete(`/workflow-mcp/workflows/${encodeURIComponent(id)}`)
}

export async function listSkills() {
  return apiGet<{ items: SkillPackage[] }>('/workflow-mcp/skills')
}

export async function getSkill(id: string) {
  return apiGet<{ skill: SkillPackage; skill_md: string }>(`/workflow-mcp/skills/${encodeURIComponent(id)}`)
}

export async function upsertSkill(skill_md: string, bump = 'patch') {
  const saved = await apiPost<{ status: string; skill: SkillPackage }>('/workflow-mcp/skills', { skill_md, bump })
  return rebuildSavedIndex(saved, reindexKnowledge)
}

export async function deleteSkill(id: string) {
  return apiDelete(`/workflow-mcp/skills/${encodeURIComponent(id)}`)
}

export type SkillAsset = { path: string; sha256: string; size_bytes: number }
export type SkillAssetIndex = { skill: SkillPackage; files: SkillAsset[] }

export class SkillAssetConflict extends Error {}

export async function listSkillAssets(id: string): Promise<SkillAssetIndex> {
  return apiGet(`/workflow-mcp/skills/${encodeURIComponent(id)}/files`)
}

export async function readSkillAsset(id: string, path: string): Promise<{ blob: Blob; revision: number }> {
  const headers = new Headers()
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(`/api/workflow-mcp/skills/${encodeURIComponent(id)}/file?path=${encodeURIComponent(path)}`, { headers })
  if (!res.ok) {
    const body = await res.json().catch(() => ({})) as { error?: string }
    if (res.status === 401) setToken(null)
    if (res.status === 409) throw new SkillAssetConflict('资产已被其他人更新，请比较最新内容后重试')
    throw new Error(body.error || `读取文件失败 (${res.status})`)
  }
  return { blob: await res.blob(), revision: Number(res.headers.get('X-Skill-Revision')) }
}

export async function patchSkillAssets(id: string, revision: number, writes: { path: string; blob: Blob }[], deletes: string[]) {
  const body = new FormData()
  const operations = writes.map((file, index) => {
    const part = `file_${index}`
    body.append(part, file.blob, 'asset')
    return { path: file.path, part }
  })
  body.append('manifest', JSON.stringify({ revision, writes: operations, deletes }))
  // Keep the status so the editor can preserve drafts and offer conflict resolution.
  const headers = new Headers()
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const res = await fetch(`/api/workflow-mcp/skills/${encodeURIComponent(id)}/assets`, { method: 'PATCH', headers, body })
  const result = await res.json() as { error?: string; changed: boolean; revision: number; content_hash: string; skill: SkillPackage; files: SkillAsset[] }
  if (!res.ok) {
    if (res.status === 401) setToken(null)
    if (res.status === 409) throw new SkillAssetConflict('技能包已被其他人更新，本地草稿已保留')
    throw new Error(result.error || `保存失败 (${res.status})`)
  }
  return result.changed ? rebuildSavedIndex(result, reindexKnowledge) : { ...result, reindexed: undefined, index_warning: undefined }
}

export async function listKnowledge(namespace = '') {
  const q = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
  return apiGet<{ items: KnowledgeDoc[] }>(`/workflow-mcp/knowledge${q}`)
}

export async function getKnowledge(id: string) {
  return apiGet<{ doc: KnowledgeDoc }>(`/workflow-mcp/knowledge/${encodeURIComponent(id)}`)
}

export async function upsertKnowledge(path: string, content: string, bump = 'patch') {
  const saved = await apiPost<{ status: string; doc: KnowledgeDoc }>('/workflow-mcp/knowledge', { path, content, bump })
  return rebuildSavedIndex(saved, reindexKnowledge)
}

export async function deleteKnowledge(id: string) {
  return apiDelete(`/workflow-mcp/knowledge/${encodeURIComponent(id)}`)
}

export async function reindexKnowledge() {
  return apiPost<{ status: string; reindexed: number }>('/workflow-mcp/knowledge/reindex', {})
}

export async function unifiedSearch(q: string) {
  return apiGet<{
    workflows: Array<{
      id: string
      name?: string
      version?: string
      description?: string
      score?: number
    }>
    skills: Array<{
      id: string
      name?: string
      version?: string
      description?: string
      cursor_name?: string
      score?: number
    }>
    knowledge: Array<{
      id: string
      title?: string
      path?: string
      source?: string
      content?: string
      score?: number
    }>
  }>(`/workflow-mcp/search?q=${encodeURIComponent(q)}`)
}

export async function listEmployeeWorkflows(empId: string) {
  return apiGet<{ workflows: Workflow[]; effective_skills: SkillPackage[] }>(
    `/employees/${encodeURIComponent(empId)}/workflows`,
  )
}

export async function grantEmployeeWorkflow(empId: string, workflow_id: string) {
  return apiPost(`/employees/${encodeURIComponent(empId)}/workflows`, { workflow_id })
}

export async function revokeEmployeeWorkflow(empId: string, workflow_id: string) {
  return apiDelete(`/employees/${encodeURIComponent(empId)}/workflows?workflow_id=${encodeURIComponent(workflow_id)}`)
}

export async function listMCPTokens(empId: string) {
  return apiGet<{ items: MCPToken[] }>(`/employees/${encodeURIComponent(empId)}/mcp-tokens`)
}

export async function issueMCPToken(empId: string, label = '') {
  return apiPost<{ token: MCPToken; secret: string; mcp_json_hint: unknown }>(
    `/employees/${encodeURIComponent(empId)}/mcp-tokens`,
    { label },
  )
}

export async function revokeMCPToken(id: string) {
  return apiDelete(`/mcp-tokens/${encodeURIComponent(id)}`)
}

export async function syncSkills(workstation_id: string, employee_id = '', skill_ids: string[] = []) {
  return apiPost('/workflow-mcp/skills/sync', { workstation_id, employee_id, skill_ids })
}

/** 上传 PersonalWorkMCP data zip（multipart field=file） */
export async function importWorkflowMCPZip(file: File) {
  const fd = new FormData()
  fd.append('file', file)
  const headers = new Headers({ Accept: 'application/json' })
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  // 不要手动设 Content-Type，由浏览器带 multipart boundary
  const res = await fetch('/api/workflow-mcp/import', { method: 'POST', headers, body: fd })
  if (res.status === 401) setToken(null)
  if (!res.ok) {
    let msg = `${res.status}`
    try {
      const err = (await res.json()) as { error?: string }
      if (err.error) msg = err.error
    } catch {
      /* ignore */
    }
    throw new Error(msg)
  }
  return res.json() as Promise<{
    status: string
    stats: { workflows: number; skills: number; knowledge: number; errors: number }
  }>
}

// 避免未使用告警
void apiPut
