import { ShieldAlert } from "lucide-react";
import { useAuth } from "../../state/AuthProvider";
import { usePageTitle } from "../../lib/usePageTitle";

export function NotAuthorizedPage() {
  const { user, logout } = useAuth();
  usePageTitle("Not authorized");
  return (
    <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", background: "var(--ag-bg-canvas)" }}>
      <div style={{ width: 380, background: "var(--ag-bg-surface)", border: "1px solid var(--ag-border)", borderRadius: 10, padding: "32px 28px" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 9, fontWeight: 600, fontSize: 15, marginBottom: 8 }}>
          <ShieldAlert size={20} strokeWidth={2} />
          Not authorized
        </div>
        <p style={{ margin: "0 0 20px", fontSize: 13, color: "var(--ag-text-secondary)" }}>
          {user ? `${user.username} is` : "You are"} signed in but lack the <code>agentgate-admin</code> role
          required to use the admin console.
        </p>
        <button type="button" className="ag-btn" onClick={() => void logout()}>
          Sign out
        </button>
      </div>
    </div>
  );
}
