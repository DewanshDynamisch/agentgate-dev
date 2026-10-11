import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { useAuth } from "../../state/AuthProvider";
import { ADMIN_ROLE } from "../../auth/oidc";
import { NotAuthorizedPage } from "../../pages/NotAuthorized/NotAuthorizedPage";

export function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  const location = useLocation();

  if (loading) return null;

  if (!user) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  // UX gate only; the backend enforces the role on every request.
  if (!user.roles.includes(ADMIN_ROLE)) {
    return <NotAuthorizedPage />;
  }

  return <>{children}</>;
}
