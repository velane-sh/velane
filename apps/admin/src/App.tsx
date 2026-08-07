import { BrowserRouter, Routes, Route, Navigate, useSearchParams } from 'react-router-dom'
import { InstanceProvider } from './contexts/InstanceContext'
import LoginPage from './pages/LoginPage'
import SSOLoginPage from './pages/SSOLoginPage'
import RegisterPage from './pages/RegisterPage'
import DashboardLayout from './components/DashboardLayout'
import SettingsLayout from './components/SettingsLayout'
import OverviewPage from './pages/OverviewPage'
import APIKeysPage from './pages/APIKeysPage'
import TeamPage from './pages/TeamPage'
import BrandingPage from './pages/BrandingPage'
import EgressPage from './pages/EgressPage'
import SnippetsPage from './pages/SnippetsPage'
import SnippetEditorPage from './pages/SnippetEditorPage'
import WorkflowSettingsPage from './pages/WorkflowSettingsPage'
import WorkflowExecutionsPage from './pages/WorkflowExecutionsPage'
import EmbedPage from './pages/EmbedPage'
import EmbedEntryPage from './pages/EmbedEntryPage'
import VariablesPage from './pages/VariablesPage'
import DataStorePage from './pages/DataStorePage'
import IntegrationsPage from './pages/IntegrationsPage'
import MCPPage from './pages/MCPPage'
import BillingPage from './pages/BillingPage'
import ProtectedRoute from './components/ProtectedRoute'
import SSOSettingsPage from './enterprise/SSOSettingsPage'

function RootRedirect() {
  const [params] = useSearchParams()
  if (params.get('token')?.startsWith('et_')) {
    return <EmbedEntryPage />
  }
  return <Navigate to="/dashboard/overview" replace />
}

export default function App() {
  return (
    <InstanceProvider>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<RootRedirect />} />
        <Route path="/login" element={<LoginPage />} />
        <Route path="/login/sso" element={<SSOLoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route
          path="/dashboard"
          element={
            <ProtectedRoute>
              <DashboardLayout />
            </ProtectedRoute>
          }
        >
          <Route index element={<Navigate to="overview" replace />} />
          <Route path="overview" element={<OverviewPage />} />
          <Route path="snippets" element={<SnippetsPage />} />
          <Route path="snippets/:id" element={<SnippetEditorPage />} />
          <Route path="snippets/:id/executions" element={<WorkflowExecutionsPage />} />
          <Route path="snippets/:id/settings" element={<WorkflowSettingsPage />} />
          <Route path="integrations" element={<IntegrationsPage />} />
          <Route path="mcp" element={<MCPPage />} />
          <Route path="variables" element={<VariablesPage />} />
          <Route path="data-store" element={<DataStorePage />} />
          <Route path="billing" element={<BillingPage />} />
          <Route path="settings" element={<SettingsLayout />}>
            <Route index element={<Navigate to="api-keys" replace />} />
            <Route path="api-keys" element={<APIKeysPage />} />
            <Route path="team" element={<TeamPage />} />
            <Route path="branding" element={<BrandingPage />} />
            <Route path="egress" element={<EgressPage />} />
            <Route path="embed" element={<EmbedPage />} />
            <Route path="sso" element={<SSOSettingsPage />} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
    </InstanceProvider>
  )
}
