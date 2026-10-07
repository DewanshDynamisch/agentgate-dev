import { Route, Routes } from "react-router-dom";
import { AppLayout } from "./components/layout/AppLayout";
import { RequireAuth } from "./components/auth/RequireAuth";
import { AuthProvider } from "./state/AuthProvider";
import { GovernanceProvider } from "./state/GovernanceProvider";
import { ToastProvider } from "./state/ToastProvider";
import { AuthCallbackPage } from "./pages/AuthCallback";
import { LoginPage } from "./pages/Login/LoginPage";
import { DashboardPage } from "./pages/Dashboard/DashboardPage";
import { PoliciesListPage } from "./pages/Policies/PoliciesListPage";
import { CreatePolicyPage } from "./pages/Policies/CreatePolicyPage";
import { DryRunPage } from "./pages/Policies/DryRunPage";
import { ActivatePolicyPage } from "./pages/Policies/ActivatePolicyPage";
import { RollbackPolicyPage } from "./pages/Policies/RollbackPolicyPage";
import { ToolsPage } from "./pages/Tools/ToolsPage";
import { IdentitiesPage } from "./pages/Identities/IdentitiesPage";
import { AuditPage } from "./pages/Audit/AuditPage";
import { SettingsPage } from "./pages/Settings/SettingsPage";
import { NotFoundPage } from "./pages/NotFound/NotFoundPage";

export default function App() {
  return (
    <AuthProvider>
      <ToastProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/auth/callback" element={<AuthCallbackPage />} />

          <Route
            element={
              <RequireAuth>
                <GovernanceProvider>
                  <AppLayout />
                </GovernanceProvider>
              </RequireAuth>
            }
          >
            <Route path="/" element={<DashboardPage />} />
            <Route path="/policies" element={<PoliciesListPage />} />
            <Route path="/policies/new" element={<CreatePolicyPage />} />
            <Route path="/policies/:version/dry-run" element={<DryRunPage />} />
            <Route path="/policies/:version/activate" element={<ActivatePolicyPage />} />
            <Route path="/policies/:version/rollback" element={<RollbackPolicyPage />} />
            <Route path="/tools" element={<ToolsPage />} />
            <Route path="/identities" element={<IdentitiesPage />} />
            <Route path="/audit" element={<AuditPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<NotFoundPage />} />
          </Route>
        </Routes>
      </ToastProvider>
    </AuthProvider>
  );
}
