import { useEffect, useMemo, useState } from "react";
import { Users } from "lucide-react";
import { useGovernanceClient } from "../../state/GovernanceProvider";
import { EmptyState } from "../../components/ui/EmptyState";
import { TableSkeleton } from "../../components/ui/Skeleton";
import { observedIdentities } from "../../lib/auditStats";
import { DECISION_LIMIT, loadRecentDecisions } from "../../lib/loadDecisions";
import type { AuditEvent } from "@contract/models/audit";

function formatTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

export function IdentitiesPage() {
  const { client, workspaceId } = useGovernanceClient();
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    loadRecentDecisions(client, workspaceId)
      .then((res) => {
        if (!cancelled) setEvents(res);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load audit events");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client, workspaceId]);

  const rows = useMemo(() => observedIdentities(events), [events]);

  return (
    <div className="ag-page">
      <div className="ag-page-header">
        <div>
          <h1 className="ag-page-title">Identities</h1>
          <p className="ag-page-subtitle">
            Observed identities (from audit): agents seen in the last {DECISION_LIMIT} decisions. This is not an
            identity directory.
          </p>
        </div>
      </div>

      {error && (
        <div className="ag-alert ag-alert-error" role="alert">
          {error}
        </div>
      )}

      {loading && <TableSkeleton rows={4} columns={5} />}

      {!loading && !error && rows.length === 0 && (
        <EmptyState
          icon={Users}
          title="No identities observed yet"
          subtitle="Agents appear here after they make authorization decisions."
        />
      )}

      {!loading && rows.length > 0 && (
        <table className="ag-table">
          <thead>
            <tr>
              <th>Agent</th>
              <th>Roles</th>
              <th>Last seen</th>
              <th>Allowed</th>
              <th>Denied</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.agentId}>
                <td className="ag-mono" style={{ color: "var(--ag-text-primary)" }}>{row.agentId}</td>
                <td>{row.roles.length > 0 ? row.roles.join(", ") : "—"}</td>
                <td className="ag-mono">{formatTime(row.lastSeen)}</td>
                <td data-testid="allowed">{row.allowed}</td>
                <td data-testid="denied">{row.denied}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
