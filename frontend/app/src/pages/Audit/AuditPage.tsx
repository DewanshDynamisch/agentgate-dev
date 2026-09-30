import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ScrollText } from "lucide-react";
import { useGovernanceClient } from "../../state/GovernanceProvider";
import { StatusDot, type StatusTone } from "../../components/ui/StatusDot";
import { CopyableValue } from "../../components/ui/CopyableValue";
import { EmptyState } from "../../components/ui/EmptyState";
import { TableSkeleton } from "../../components/ui/Skeleton";
import type { AuditEvent } from "@contract/models/audit";

const PAGE_SIZE = 50;
const REFRESH_INTERVAL_MS = 5000;
const COLUMN_COUNT = 9;

function formatTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

function toolLabel(e: AuditEvent): string {
  if (!e.toolBackendId && !e.toolName) return "—";
  return `${e.toolBackendId}/${e.toolName}`;
}

function decisionTone(decision: string): StatusTone {
  if (decision === "ALLOW") return "allow";
  if (decision === "DENY") return "deny";
  return "neutral";
}

/** Merge events into an existing list, deduplicating by sequenceNumber, newest first. */
function mergeEvents(existing: AuditEvent[], incoming: AuditEvent[]): AuditEvent[] {
  const bySeq = new Map<number, AuditEvent>();
  for (const e of existing) bySeq.set(e.sequenceNumber, e);
  for (const e of incoming) bySeq.set(e.sequenceNumber, e);
  return [...bySeq.values()].sort((a, b) => b.sequenceNumber - a.sequenceNumber);
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : "Failed to load audit events";
}

export function AuditPage() {
  const { client, workspaceId } = useGovernanceClient();
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [nextBefore, setNextBefore] = useState<number | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [decisionFilter, setDecisionFilter] = useState("");
  const [toolFilter, setToolFilter] = useState("");
  const [agentFilter, setAgentFilter] = useState("");
  const [expanded, setExpanded] = useState<number | null>(null);
  const inFlight = useRef(false);

  // Initial load. The cancelled flag prevents state updates after unmount / client change.
  useEffect(() => {
    let cancelled = false;
    client
      .listAuditEvents(workspaceId, { limit: PAGE_SIZE })
      .then((res) => {
        if (cancelled) return;
        setEvents(mergeEvents([], res.events));
        setNextBefore(res.nextBeforeSequence);
        setError(null);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errorMessage(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client, workspaceId]);

  // Auto-refresh: refetch the first page and merge; the pagination cursor is left untouched.
  useEffect(() => {
    if (!autoRefresh) return;
    let cancelled = false;
    const id = setInterval(() => {
      if (inFlight.current) return;
      inFlight.current = true;
      client
        .listAuditEvents(workspaceId, { limit: PAGE_SIZE })
        .then((res) => {
          if (cancelled) return;
          setEvents((prev) => mergeEvents(prev, res.events));
          setError(null);
        })
        .catch((err: unknown) => {
          if (!cancelled) setError(errorMessage(err));
        })
        .finally(() => {
          inFlight.current = false;
        });
    }, REFRESH_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
      inFlight.current = false;
    };
  }, [autoRefresh, client, workspaceId]);

  const loadOlder = useCallback(async () => {
    if (nextBefore === undefined) return;
    setLoadingOlder(true);
    try {
      const res = await client.listAuditEvents(workspaceId, { limit: PAGE_SIZE, beforeSequence: nextBefore });
      setEvents((prev) => mergeEvents(prev, res.events));
      setNextBefore(res.nextBeforeSequence);
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoadingOlder(false);
    }
  }, [client, workspaceId, nextBefore]);

  const visible = useMemo(() => {
    const tool = toolFilter.trim().toLowerCase();
    const agent = agentFilter.trim().toLowerCase();
    return events.filter(
      (e) =>
        (!decisionFilter || e.decision === decisionFilter) &&
        (!tool || toolLabel(e).toLowerCase().includes(tool)) &&
        (!agent || e.principalAgentId.toLowerCase().includes(agent))
    );
  }, [events, decisionFilter, toolFilter, agentFilter]);

  // Rows are never shown alongside an error banner: no stale or fallback data on failure.
  const showRows = !loading && !error;

  return (
    <div className="ag-page">
      <div className="ag-page-header">
        <div>
          <h1 className="ag-page-title">Audit log</h1>
          <p className="ag-page-subtitle">Every authorization decision, allowed or denied.</p>
        </div>
        <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13.5 }}>
          <input
            type="checkbox"
            checked={autoRefresh}
            onChange={(e) => setAutoRefresh(e.target.checked)}
          />
          Auto-refresh (5s)
        </label>
      </div>

      {error && (
        <div className="ag-alert ag-alert-error" role="alert">
          {error}
        </div>
      )}

      <div style={{ display: "flex", gap: 12, marginBottom: 16, flexWrap: "wrap" }}>
        <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12.5 }}>
          Decision
          <select
            className="ag-input"
            value={decisionFilter}
            onChange={(e) => setDecisionFilter(e.target.value)}
          >
            <option value="">All</option>
            <option value="ALLOW">ALLOW</option>
            <option value="DENY">DENY</option>
          </select>
        </label>
        <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12.5 }}>
          Tool
          <input
            className="ag-input"
            value={toolFilter}
            onChange={(e) => setToolFilter(e.target.value)}
            placeholder="backend/tool"
          />
        </label>
        <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12.5 }}>
          Agent
          <input
            className="ag-input"
            value={agentFilter}
            onChange={(e) => setAgentFilter(e.target.value)}
            placeholder="agent id"
          />
        </label>
      </div>

      {loading && <TableSkeleton rows={5} columns={COLUMN_COUNT} />}

      {showRows && events.length === 0 && (
        <EmptyState
          icon={ScrollText}
          title="No audit events yet"
          subtitle="Authorization decisions will appear here as agents call tools."
        />
      )}

      {showRows && events.length > 0 && (
        <>
          {visible.length === 0 ? (
            <EmptyState
              icon={ScrollText}
              title="No matching events"
              subtitle="No loaded events match the current filters."
            />
          ) : (
            <table className="ag-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Decision</th>
                  <th>Reason</th>
                  <th>Agent</th>
                  <th>On behalf of</th>
                  <th>Tool</th>
                  <th>Risk</th>
                  <th>Policy</th>
                  <th>Execution</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((e) => {
                  const args = Object.entries(e.redactedArguments ?? {});
                  return (
                    <Fragment key={e.sequenceNumber}>
                      <tr
                        onClick={() => setExpanded(expanded === e.sequenceNumber ? null : e.sequenceNumber)}
                        style={{ cursor: "pointer" }}
                      >
                        <td className="ag-mono">{formatTime(e.timestamp)}</td>
                        <td>
                          <StatusDot tone={decisionTone(e.decision)}>{e.decision || e.eventType}</StatusDot>
                        </td>
                        <td className="ag-mono">{e.reason || "—"}</td>
                        <td className="ag-mono">{e.principalAgentId || "—"}</td>
                        <td className="ag-mono">{e.principalOnBehalfOf || "—"}</td>
                        <td className="ag-mono">{toolLabel(e)}</td>
                        <td>{e.toolRisk || "—"}</td>
                        <td className="ag-mono">{e.policyVersion || "—"}</td>
                        <td>{e.executionId ? <CopyableValue value={e.executionId} /> : "—"}</td>
                      </tr>
                      {expanded === e.sequenceNumber && (
                        <tr>
                          <td colSpan={COLUMN_COUNT} data-testid={`audit-args-${e.sequenceNumber}`}>
                            {args.length === 0 ? (
                              <span style={{ color: "var(--ag-text-secondary)" }}>No arguments recorded</span>
                            ) : (
                              <dl style={{ margin: 0, fontSize: 13 }}>
                                {args.map(([k, v]) => (
                                  <div key={k} style={{ display: "flex", gap: 16, padding: "4px 0" }}>
                                    <dt className="ag-mono" style={{ color: "var(--ag-text-secondary)" }}>{k}</dt>
                                    <dd className="ag-mono" style={{ margin: 0 }}>{v}</dd>
                                  </div>
                                ))}
                              </dl>
                            )}
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          )}
          {nextBefore !== undefined && (
            <div style={{ marginTop: 16, textAlign: "center" }}>
              <button className="ag-btn ag-btn-secondary" onClick={loadOlder} disabled={loadingOlder}>
                {loadingOlder ? "Loading…" : "Load older"}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
