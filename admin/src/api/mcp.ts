import { apiDelete, apiGet, apiPost, apiPut } from './client'

export type MCPServer = {
  id: string
  name: string
  description: string
  server_type: 'builtin' | 'custom'
  transport: 'http' | 'sse' | 'stdio'
  endpoint: string
  config: Record<string, any>
  status: 'active' | 'disabled'
  created_at: string
  updated_at: string
  bound_employee_count?: number
}

export type Credential = {
  id: string
  owner_type: 'USER' | 'ORGANIZATION' | 'SERVICE_ACCOUNT'
  owner_id: string
  provider: string
  auth_type: 'oauth' | 'api_key' | 'bearer' | 'pat' | 'basic'
  credential_name: string
  secret_ref?: string
  masked_value: string
  expires_at?: string
  metadata?: Record<string, any>
  status: 'active' | 'expired' | 'revoked'
  created_by?: string
  created_at: string
  updated_at: string
  bound_employee_count?: number
}

export type EmployeeMCPBinding = {
  id: string
  employee_id: string
  mcp_server_id: string
  credential_id?: string
  enabled: boolean
  allowed_tools: string[]
  denied_tools: string[]
  config: Record<string, any>
  created_at: string
  updated_at: string

  mcp_server_name?: string
  mcp_server_type?: string
  mcp_transport?: string
  credential_name?: string
  credential_provider?: string
  credential_masked?: string
}

// ---------- MCP Server API ----------

export async function listMCPServers(): Promise<{ items: MCPServer[] }> {
  return apiGet<{ items: MCPServer[] }>('/mcp-servers')
}

export async function getMCPServer(id: string): Promise<{ server: MCPServer }> {
  return apiGet<{ server: MCPServer }>(`/mcp-servers/${encodeURIComponent(id)}`)
}

export async function createMCPServer(data: {
  id?: string
  name: string
  description?: string
  transport: 'http' | 'sse' | 'stdio'
  endpoint: string
  config?: Record<string, any>
}): Promise<{ server: MCPServer }> {
  return apiPost<{ server: MCPServer }>('/mcp-servers', data)
}

export async function updateMCPServer(
  id: string,
  data: Partial<MCPServer>
): Promise<{ server: MCPServer }> {
  return apiPut<{ server: MCPServer }>(`/mcp-servers/${encodeURIComponent(id)}`, data)
}

export async function deleteMCPServer(id: string): Promise<{ deleted: string }> {
  return apiDelete<{ deleted: string }>(`/mcp-servers/${encodeURIComponent(id)}`)
}

// ---------- Credential API ----------

export async function listCredentials(params?: {
  owner_type?: string
  owner_id?: string
}): Promise<{ items: Credential[] }> {
  const q = new URLSearchParams()
  if (params?.owner_type) q.set('owner_type', params.owner_type)
  if (params?.owner_id) q.set('owner_id', params.owner_id)
  const qs = q.toString() ? `?${q.toString()}` : ''
  return apiGet<{ items: Credential[] }>(`/credentials${qs}`)
}

export async function createCredential(data: {
  credential_name: string
  owner_type: 'USER' | 'ORGANIZATION' | 'SERVICE_ACCOUNT'
  owner_id?: string
  provider: string
  auth_type: string
  secret_value: string
  expires_at?: string
  metadata?: Record<string, any>
}): Promise<{ credential: Credential }> {
  return apiPost<{ credential: Credential }>('/credentials', data)
}

export async function updateCredential(
  id: string,
  data: Partial<CreateCredentialInput>
): Promise<{ credential: Credential }> {
  return apiPut<{ credential: Credential }>(`/credentials/${encodeURIComponent(id)}`, data)
}

export async function deleteCredential(id: string): Promise<{ deleted: string }> {
  return apiDelete<{ deleted: string }>(`/credentials/${encodeURIComponent(id)}`)
}

type CreateCredentialInput = {
  credential_name: string
  owner_type: string
  owner_id: string
  provider: string
  auth_type: string
  secret_value?: string
  expires_at?: string
}

// ---------- Employee MCP Bindings API ----------

export async function listEmployeeMCPBindings(
  employeeId: string
): Promise<{ items: EmployeeMCPBinding[] }> {
  return apiGet<{ items: EmployeeMCPBinding[] }>(
    `/employees/${encodeURIComponent(employeeId)}/mcp-bindings`
  )
}

export async function bindEmployeeMCP(
  employeeId: string,
  data: {
    mcp_server_id: string
    credential_id?: string
    enabled?: boolean
    allowed_tools?: string[]
    denied_tools?: string[]
    config?: Record<string, any>
  }
): Promise<{ binding: EmployeeMCPBinding }> {
  return apiPost<{ binding: EmployeeMCPBinding }>(
    `/employees/${encodeURIComponent(employeeId)}/mcp-bindings`,
    data
  )
}

export async function updateEmployeeMCPBinding(
  employeeId: string,
  bindingId: string,
  data: {
    credential_id?: string
    enabled?: boolean
    allowed_tools?: string[]
    denied_tools?: string[]
    config?: Record<string, any>
  }
): Promise<{ binding: EmployeeMCPBinding }> {
  return apiPut<{ binding: EmployeeMCPBinding }>(
    `/employees/${encodeURIComponent(employeeId)}/mcp-bindings/${encodeURIComponent(bindingId)}`,
    data
  )
}

export async function unbindEmployeeMCP(
  employeeId: string,
  bindingId: string
): Promise<{ deleted: string }> {
  return apiDelete<{ deleted: string }>(
    `/employees/${encodeURIComponent(employeeId)}/mcp-bindings/${encodeURIComponent(bindingId)}`
  )
}
