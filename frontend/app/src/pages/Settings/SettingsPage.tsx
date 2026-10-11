import { ComingSoonPage } from "../../components/ui/ComingSoonPage";
import { usePageTitle } from '../../lib/usePageTitle';

function ConfigRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "12px 0", borderBottom: "1px solid var(--ag-border-soft)" }}>
      <span style={{ fontSize: 13.5, color: "var(--ag-text-secondary)" }}>{label}</span>
      <span className="ag-mono" style={{ color: "var(--ag-text-primary)" }}>{value}</span>
    </div>
  );
}

export function SettingsPage() {
  usePageTitle('Settings');
  return (
    <ComingSoonPage
      title="Settings"
      subtitle="System configuration."
      note="Read-only preview. Auth currently uses a single static admin bearer token (AGENTGATE_ADMIN_TOKEN), not per-user login — a real Settings/Auth UI needs a team decision first, not just UI work."
    >
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 24 }}>
        <div className="ag-card">
          <div className="ag-card-header">
            <h2 className="ag-card-title">Cedar policy engine</h2>
          </div>
          <div className="ag-card-body" style={{ paddingTop: 4 }}>
            <ConfigRow label="Policy path" value="./config/agentgate.cedar" />
            <ConfigRow label="Auto reload" value="enabled" />
            <ConfigRow label="Cached engine" value="active" />
          </div>
        </div>

        <div className="ag-card">
          <div className="ag-card-header">
            <h2 className="ag-card-title">Gateway</h2>
          </div>
          <div className="ag-card-body" style={{ paddingTop: 4 }}>
            <ConfigRow label="ext_authz endpoint" value="agentgate:50051" />
            <ConfigRow label="Protocol" value="Envoy v3 gRPC" />
            <ConfigRow label="TLS / mTLS" value="enabled" />
          </div>
        </div>

        <div className="ag-card">
          <div className="ag-card-header">
            <h2 className="ag-card-title">Database</h2>
          </div>
          <div className="ag-card-body" style={{ paddingTop: 4 }}>
            <ConfigRow label="Host" value="postgres:5432" />
            <ConfigRow label="Connection pool" value="20" />
            <ConfigRow label="Privilege model" value="app / migrator split" />
          </div>
        </div>

        <div className="ag-card">
          <div className="ag-card-header">
            <h2 className="ag-card-title">Audit</h2>
          </div>
          <div className="ag-card-body" style={{ paddingTop: 4 }}>
            <ConfigRow label="Persistence" value="PostgreSQL, append-only" />
            <ConfigRow label="Chain integrity" value="SHA-256 row chaining" />
            <ConfigRow label="Fail-closed" value="enabled" />
          </div>
        </div>
      </div>
    </ComingSoonPage>
  );
}
