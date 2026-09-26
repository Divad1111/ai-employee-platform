/**
 * 应用路由：登录守卫 + 业务页（对齐设计文档 §8）。
 */
import { BrowserRouter, Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { AppShell } from '../components/AppShell'
import { ApprovalsPage } from '../pages/ApprovalsPage'
import { ArtifactsPage } from '../pages/ArtifactsPage'
import { BackupsPage } from '../pages/backups/BackupsPage'
import { SecretsPage } from '../pages/SecretsPage'
import { AuditPage, SettingsPage } from '../pages/MiscPages'
import { DashboardPage } from '../pages/DashboardPage'
import { EmployeeDetailPage } from '../pages/EmployeeDetailPage'
import { EmployeesPage } from '../pages/EmployeesPage'
import { FeishuPage } from '../pages/FeishuPage'
import { JobDetailPage, JobsPage } from '../pages/JobsPage'
import { AutomationsPage } from '../pages/AutomationsPage'
import { LoginPage } from '../pages/LoginPage'
import { SetupPage } from '../pages/SetupPage'
import { PermissionsPage } from '../pages/PermissionsPage'
import { SessionsPage } from '../pages/SessionsPage'
import { McpServersPage } from '../pages/mcp/McpServersPage'
import { WorkflowMcpPage } from '../pages/workflow/WorkflowMcpPage'
import { WorkstationsPage } from '../pages/WorkstationsPage'
import { WorkspacesPage } from '../pages/WorkspacesPage'
import { UsersPage } from '../pages/UsersPage'
import { UserDetailPage } from '../pages/UserDetailPage'
import { RolesPage } from '../pages/RolesPage'
import { RoleCreatePage } from '../pages/RoleCreatePage'
import { QuotasPage } from '../pages/QuotasPage'
import { isAuthenticated } from '../stores/session'
import { PermissionProvider } from '../stores/permissions'

function RequireAuth() {
  if (!isAuthenticated()) {
    return <Navigate to="/login" replace />
  }
  return (
    <PermissionProvider>
      <Outlet />
    </PermissionProvider>
  )
}

export function AppRouter() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/setup" element={<SetupPage />} />
        <Route path="/login" element={<LoginPage />} />
        <Route element={<RequireAuth />}>
          <Route element={<AppShell />}>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/employees" element={<EmployeesPage />} />
            <Route path="/employees/:id" element={<EmployeeDetailPage />} />
            <Route path="/workstations" element={<WorkstationsPage />} />
            <Route path="/workspaces" element={<WorkspacesPage />} />
            <Route path="/jobs" element={<JobsPage />} />
            <Route path="/jobs/:id" element={<JobDetailPage />} />
            <Route path="/automations" element={<AutomationsPage />} />
            <Route path="/sessions" element={<SessionsPage />} />
            <Route path="/feishu" element={<FeishuPage />} />
            <Route path="/mcp-servers" element={<McpServersPage />} />
            <Route path="/mcp-servers/workflow-mcp" element={<WorkflowMcpPage />} />
            <Route path="/workflows" element={<Navigate to="/mcp-servers/workflow-mcp" replace />} />
            <Route path="/skills" element={<Navigate to="/mcp-servers/workflow-mcp" replace />} />
            <Route path="/knowledge" element={<Navigate to="/mcp-servers/workflow-mcp" replace />} />
            <Route path="/permissions" element={<PermissionsPage />} />
            <Route path="/users" element={<UsersPage />} />
            <Route path="/users/:id" element={<UserDetailPage />} />
            <Route path="/roles" element={<RolesPage />} />
            <Route path="/roles/new" element={<RoleCreatePage />} />
            <Route path="/quotas" element={<QuotasPage />} />
            <Route path="/approvals" element={<ApprovalsPage />} />
            <Route path="/secrets" element={<SecretsPage />} />
            <Route path="/backups" element={<BackupsPage />} />
            <Route path="/artifacts" element={<ArtifactsPage />} />
            <Route path="/audit" element={<AuditPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
  )
}
