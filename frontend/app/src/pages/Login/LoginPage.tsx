import { useState } from "react";
import { Navigate } from "react-router-dom";
import { ShieldCheck } from "lucide-react";
import { useAuth } from "../../state/AuthProvider";

export function LoginPage() {
  const { user, loading, ssoConfigured, login } = useAuth();
  const [error, setError] = useState<string | null>(null);

  if (!loading && user) {
    return <Navigate to="/" replace />;
  }

  const handleSignIn = () => {
    login().catch(() => setError("Could not start sign-in. Please try again."));
  };

  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "var(--ag-bg-canvas)",
      }}
    >
      <div
        style={{
          width: 360,
          background: "var(--ag-bg-surface)",
          border: "1px solid var(--ag-border)",
          borderRadius: 10,
          padding: "32px 28px",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 9, fontWeight: 600, fontSize: 15, marginBottom: 4 }}>
          <ShieldCheck size={20} strokeWidth={2} />
          AgentGate
        </div>
        <p style={{ margin: "0 0 24px", fontSize: 13, color: "var(--ag-text-secondary)" }}>
          Sign in with your organization account to manage policies.
        </p>

        {!ssoConfigured ? (
          <div className="ag-alert ag-alert-error">
            <strong>SSO not configured.</strong> Set <code>oidcAuthority</code> and <code>oidcClientId</code> in{" "}
            <code>/config.js</code>.
          </div>
        ) : (
          <>
            {error && <div className="ag-alert ag-alert-error">{error}</div>}
            <button
              type="button"
              className="ag-btn ag-btn-primary"
              style={{ width: "100%", justifyContent: "center" }}
              onClick={handleSignIn}
            >
              Sign in with SSO
            </button>
          </>
        )}
      </div>
    </div>
  );
}
