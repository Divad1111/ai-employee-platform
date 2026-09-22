/** 工作流MCP 前端类型与 API */
import { apiDelete, apiGet, apiPost, apiPut, getToken, setToken } from './client'

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
  return apiPost<{ status: string; action: string; workflow: Workflow }>('/workflow-mcp/workflows', { yaml, bump })
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
  return apiPost<{ status: string; skill: SkillPackage }>('/workflow-mcp/skills', { skill_md, bump })
}

export async function deleteSkill(id: string) {
  return apiDelete(`/workflow-mcp/skills/${encodeURIComponent(id)}`)
}

export async function listKnowledge(namespace = '') {
  const q = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
  return apiGet<{ items: KnowledgeDoc[] }>(`/workflow-mcp/knowledge${q}`)
}

export async function getKnowledge(id: string) {
  return apiGet<{ doc: KnowledgeDoc }>(`/workflow-mcp/knowledge/${encodeURIComponent(id)}`)
}

export async function upsertKnowledge(path: string, content: string, bump = 'patch') {
  return apiPost<{ status: string; doc: KnowledgeDoc }>('/workflow-mcp/knowledge', { path, content, bump })
}

export async function deleteKnowledge(id: string) {
  return apiDelete(`/workflow-mcp/knowledge/${encodeURIComponent(id)}`)
}

export async function reindexKnowledge() {
  return apiPost<{ status: string; reindexed: number }>('/workflow-mcp/knowledge/reindex', {})
}

export async function unifiedSearch(q: string) {
  return apiGet<{ workflows: unknown[]; skills: unknown[]; knowledge: unknown[] }>(
    `/workflow-mcp/search?q=${encodeURIComponent(q)}`,
  )
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
