import { useState } from "react";
import { usePageTitle } from '../../lib/usePageTitle';
import { useEffect } from "react";
import { Link } from "react-router-dom";
import { Plus, ArrowRight, ShieldCheck, X } from "lucide-react";
import { useGovernance } from "../../state/GovernanceProvider";
import { StatusDot, type StatusTone } from "../../components/ui/StatusDot";
import { CopyableValue } from "../../components/ui/CopyableValue";
import { EmptyState } from "../../components/ui/EmptyState";
import { TableSkeleton } from "../../components/ui/Skeleton";
import type { PolicyRecord } from "@contract/models/governance";

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

const STATE_TONE: Record<PolicyRecord["state"], StatusTone> = {
  active: "allow",
  candidate: "candidate",
  historical: "neutral",
};

const STATE_LABEL: Record<PolicyRecord["state"], string> = {
  active: "Active",
  candidate: "Candidate",
  historical: "Historical",
};

export function PoliciesListPage() {
  usePageTitle('Policies');
  const { state, ready } = useGovernance();
  const [selectedTab, setSelectedTab] = useState<"all" | PolicyRecord["state"]>("all");
  const [selectedPolicy, setSelectedPolicy] = useState<PolicyRecord | null>(null);
  const sorted = [...state.policies].sort((a, b) =>
    a.state === b.state ? 0 : a.state === "active" ? -1 : b.state === "active" ? 1 : 0
  );
  const visiblePolicies = selectedTab === "all"
    ? sorted
    : sorted.filter((policy) => policy.state === selectedTab);
  const counts = {
    active: state.policies.filter((p) => p.state === "active").length,
    candidate: state.policies.filter((p) => p.state === "candidate").length,
    historical: state.policies.filter((p) => p.state === "historical").length,
  };

  useEffect(() => {
    if (!selectedPolicy) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setSelectedPolicy(null);
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [selectedPolicy]);

  return (
    <div className="ag-page">
      <div className="ag-eyebrow">Workspace &nbsp;/&nbsp; {state.workspaceId}</div>

      <div className="ag-page-header">
        <div>
          <h1 className="ag-page-title">Policies</h1>
          <p className="ag-page-subtitle">Every version, who changed it, and what's live right now.</p>
        </div>
        <Link to="/policies/new" className="ag-btn ag-btn-primary">
          <Plus size={14} />
          Create policy
        </Link>
      </div>

      <div className="ag-tabs">
        <button
          type="button"
          className={`ag-tab ${selectedTab === "all" ? "active" : ""}`}
          onClick={() => setSelectedTab("all")}
          aria-selected={selectedTab === "all"}
        >
          All <span className="ag-tab-count">{state.policies.length}</span>
        </button>
        <button
          type="button"
          className={`ag-tab ${selectedTab === "active" ? "active" : ""}`}
          onClick={() => setSelectedTab("active")}
          aria-selected={selectedTab === "active"}
        >
          Active <span className="ag-tab-count">{counts.active}</span>
        </button>
        <button
          type="button"
          className={`ag-tab ${selectedTab === "candidate" ? "active" : ""}`}
          onClick={() => setSelectedTab("candidate")}
          aria-selected={selectedTab === "candidate"}
        >
          Candidates <span className="ag-tab-count">{counts.candidate}</span>
        </button>
        <button
          type="button"
          className={`ag-tab ${selectedTab === "historical" ? "active" : ""}`}
          onClick={() => setSelectedTab("historical")}
          aria-selected={selectedTab === "historical"}
        >
          History <span className="ag-tab-count">{counts.historical}</span>
        </button>
      </div>

      {!ready ? (
        <TableSkeleton rows={3} columns={5} />
      ) : visiblePolicies.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title={selectedTab === "all" ? "No policies yet" : `No ${selectedTab} policies`}
          subtitle={selectedTab === "all" ? "Create your first candidate policy to start governing what agents can do." : "There are no policies in this category yet."}
          action={
            <Link to="/policies/new" className="ag-btn ag-btn-primary">
              <Plus size={14} />
              Create policy
            </Link>
          }
        />
      ) : (
        <table className="ag-table ag-policy-table">
          <thead>
            <tr>
              <th>Version</th>
              <th>Status</th>
              <th>Description</th>
              <th>Updated</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
                    {visiblePolicies.map((p) => (
              <tr key={p.version}>
                <td
                  className="ag-mono"
                  onClick={() => setSelectedPolicy(p)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      setSelectedPolicy(p);
                    }
                  }}
                  tabIndex={0}
                  role="button"
                  aria-label={`View details for policy ${p.version}`}
                >
                  <CopyableValue value={p.version} />
                </td>
                <td
                  className="ag-policy-click-cell"
                  onClick={() => setSelectedPolicy(p)}
                >
                  <StatusDot tone={STATE_TONE[p.state]}>{STATE_LABEL[p.state]}</StatusDot>
                </td>
                <td className="ag-policy-description ag-policy-click-cell" onClick={() => setSelectedPolicy(p)}>{p.description}</td>
                <td className="ag-policy-click-cell" style={{ color: "var(--ag-text-secondary)" }} onClick={() => setSelectedPolicy(p)}>
                  {formatDate(p.activated_at ?? p.created_at)}
                </td>
                <td className="ag-td-right">
                  {p.state === "candidate" ? (
                    <Link onClick={(event) => event.stopPropagation()} to={`/policies/${encodeURIComponent(p.version)}/dry-run`} className="ag-chip">
                      Dry run
                      <ArrowRight size={12} />
                    </Link>
                  ) : p.state === "historical" ? (
                    <Link onClick={(event) => event.stopPropagation()} to={`/policies/${encodeURIComponent(p.version)}/rollback`} className="ag-chip">
                      Rollback to this
                      <ArrowRight size={12} />
                    </Link>
                  ) : (
                    <span className="ag-mono" style={{ color: "var(--ag-text-muted)" }}>current</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {selectedPolicy && (
        <div className="ag-modal-backdrop" role="presentation" onClick={() => setSelectedPolicy(null)}>
          <section
            className="ag-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="policy-details-title"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="ag-modal-header">
              <div>
                <div className="ag-eyebrow">Policy governance</div>
                <h2 id="policy-details-title" className="ag-modal-title">Policy details</h2>
              </div>
              <button
                type="button"
                className="ag-icon-button"
                onClick={() => setSelectedPolicy(null)}
                aria-label="Close policy details"
                title="Close"
              >
                <X size={17} />
              </button>
            </div>

            <div className="ag-modal-meta">
              <div>
                <span>Version</span>
                <strong className="ag-mono">{selectedPolicy.version}</strong>
              </div>
              <div>
                <span>Workspace</span>
                <strong className="ag-mono">{selectedPolicy.workspace_id}</strong>
              </div>
              <div>
                <span>Status</span>
                <StatusDot tone={STATE_TONE[selectedPolicy.state]}>{STATE_LABEL[selectedPolicy.state]}</StatusDot>
              </div>
              <div>
                <span>Updated</span>
                <strong>{formatDate(selectedPolicy.activated_at ?? selectedPolicy.created_at)}</strong>
              </div>
            </div>

            <div className="ag-modal-section">
              <h3>Description</h3>
              <p>{selectedPolicy.description || "No description provided."}</p>
            </div>

            <div className="ag-modal-section">
              <h3>Cedar policy source</h3>
              <pre className="ag-policy-source">{selectedPolicy.content}</pre>
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
