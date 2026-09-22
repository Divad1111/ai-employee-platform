/**
 * 应用路由：登录守卫 + 业务页（对齐设计文档 §8）。
 */
import { BrowserRouter, Navigate, Outlet, Route, Routes } from 'react-router-dom'
import { AppShell } from '../components/AppShell'
import { ApprovalsPage } from '../pages/ApprovalsPage'
import { ArtifactsPage } from '../pages/ArtifactsPage'
import { SecretsPage } from '../pages/SecretsPage'
import { AuditPage, SettingsPage } from '../pages/MiscPages'
import { DashboardPage } from '../pages/DashboardPage'
import { EmployeeDetailPage } from '../pages/EmployeeDetailPage'
import { EmployeesPage } from '../pages/EmployeesPage'
import { FeishuPage } from '../pages/FeishuPage'
import { JobDetailPage, JobsPage } from '../pages/JobsPage'
import { LoginPage } from '../pages/LoginPage'
import { SetupPage } from '../pages/SetupPage'
import { PermissionsPage } from '../pages/PermissionsPage'
import { SessionsPage } from '../pages/SessionsPage'
import { WorkflowMcpPage } from '../pages/workflow/WorkflowMcpPage'
import { WorkstationsPage } from '../pages/WorkstationsPage'
import { WorkspacesPage } from '../pages/WorkspacesPage'
import { isAuthenticated } from '../stores/session'

function RequireAuth() {
  if (!isAuthenticated()) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
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
            <Route path="/sessions" element={<SessionsPage />} />
            <Route path="/feishu" element={<FeishuPage />} />
            <Route path="/workflows" element={<WorkflowMcpPage />} />
            <Route path="/skills" element={<Navigate to="/workflows" replace />} />
            <Route path="/knowledge" element={<Navigate to="/workflows" replace />} />
            <Route path="/permissions" element={<PermissionsPage />} />
            <Route path="/approvals" element={<ApprovalsPage />} />
            <Route path="/secrets" element={<SecretsPage />} />
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
