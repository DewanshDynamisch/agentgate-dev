import { useCallback, useEffect, useMemo, useState } from "react";
import { usePageTitle } from '../../lib/usePageTitle';
import { Wrench } from "lucide-react";
import type { ToolDefinition } from "@contract/models/tools";
import { useGovernanceClient } from "../../state/GovernanceProvider";
import { StatusDot, type StatusTone } from "../../components/ui/StatusDot";
import { EmptyState } from "../../components/ui/EmptyState";
import { TableSkeleton } from "../../components/ui/Skeleton";
import { ToolEditDialog } from "./ToolEditDialog";
import { ToolDeleteDialog } from "./ToolDeleteDialog";

const COLUMN_COUNT = 6;

function riskTone(risk: string): StatusTone {
  if (risk === "destructive") return "deny";
  if (risk === "write") return "candidate";
  if (risk === "read") return "allow";
  return "neutral";
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

type DialogState =
  | { kind: "none" }
  | { kind: "register" }
  | { kind: "edit"; tool: ToolDefinition }
  | { kind: "delete"; tool: ToolDefinition };

export function ToolsPage() {
  usePageTitle('Tools');
  const { client } = useGovernanceClient();
  const [tools, setTools] = useState<ToolDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [dialog, setDialog] = useState<DialogState>({ kind: "none" });
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    client
      .listTools()
      .then((list) => {
        if (cancelled) return;
        setTools(list);
        setLoadError(null);
      })
      .catch((err: unknown) => {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : "Failed to load tools");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client, reloadKey]);

  const groups = useMemo(() => {
    const byBackend = new Map<string, ToolDefinition[]>();
    for (const t of tools) {
      const list = byBackend.get(t.backendId) ?? [];
      list.push(t);
      byBackend.set(t.backendId, list);
    }
    return [...byBackend.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([backendId, list]) => ({
        backendId,
        tools: [...list].sort((a, b) => a.toolName.localeCompare(b.toolName)),
      }));
  }, [tools]);

  const close = useCallback(() => setDialog({ kind: "none" }), []);
  const done = useCallback(() => {
    setDialog({ kind: "none" });
    setReloadKey((k) => k + 1);
  }, []);

  const showRows = !loading && !(loadError && tools.length === 0);

  return (
    <div className="ag-page">
      <div className="ag-page-header">
        <div>
          <h1 className="ag-page-title">Tools &amp; resources</h1>
          <p className="ag-page-subtitle">Manage tool classifications and governance.</p>
        </div>
        <button type="button" className="ag-btn ag-btn-primary" onClick={() => setDialog({ kind: "register" })}>
          Register tool
        </button>
      </div>

      {loadError && (
        <div className="ag-alert ag-alert-error" role="alert">
          {loadError}
        </div>
      )}

      {loading && <TableSkeleton rows={5} columns={COLUMN_COUNT} />}

      {showRows && tools.length === 0 && (
        <EmptyState
          icon={Wrench}
          title="No tools registered"
          subtitle="Register a tool to classify it for authorization."
        />
      )}

      {showRows &&
        groups.map((group) => (
          <section key={group.backendId} style={{ marginBottom: 24 }}>
            <h2 className="ag-mono" style={{ fontSize: 15, margin: "0 0 8px" }}>
              {group.backendId}
            </h2>
            <table className="ag-table">
              <thead>
                <tr>
                  <th>Tool</th>
                  <th>Risk</th>
                  <th>Arguments</th>
                  <th>Updated by</th>
                  <th>Updated at</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {group.tools.map((t) => (
                  <tr key={`${t.backendId}/${t.toolName}`}>
                    <td className="ag-mono" style={{ color: "var(--ag-text-primary)" }}>
                      {t.toolName}
                    </td>
                    <td>
                      <StatusDot tone={riskTone(t.risk)}>{t.risk}</StatusDot>
                    </td>
                    <td>{t.arguments.length}</td>
                    <td className="ag-mono">{t.updatedBy || "—"}</td>
                    <td className="ag-mono">{formatTime(t.updatedAt)}</td>
                    <td>
                      <div style={{ display: "flex", gap: 8 }}>
                      <button
                        type="button"
                        className="ag-btn ag-btn-secondary ag-btn-sm"
                        aria-label={`Edit ${t.toolName}`}
                        onClick={() => setDialog({ kind: "edit", tool: t })}
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        className="ag-btn ag-btn-danger ag-btn-sm"
                        aria-label={`Delete ${t.toolName}`}
                        onClick={() => setDialog({ kind: "delete", tool: t })}
                      >
                        Delete
                      </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        ))}

      {dialog.kind === "register" && <ToolEditDialog client={client} onClose={close} onSaved={done} />}
      {dialog.kind === "edit" && (
        <ToolEditDialog key={`${dialog.tool.backendId}/${dialog.tool.toolName}`} client={client} tool={dialog.tool} onClose={close} onSaved={done} />
      )}
      {dialog.kind === "delete" && (
        <ToolDeleteDialog client={client} tool={dialog.tool} onClose={close} onDeleted={done} />
      )}
    </div>
  );
}
