import { useEffect, useRef, useState } from "react";
import type { GovernanceClient } from "@contract/api/governanceClient";
import type { ToolDefinition } from "@contract/models/tools";

interface Props {
  client: GovernanceClient;
  tool: ToolDefinition;
  onClose: () => void;
  onDeleted: () => void;
}

export function ToolDeleteDialog({ client, tool, onClose, onDeleted }: Props) {
  const [typed, setTyped] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !deleting) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose, deleting]);

  const canDelete = typed === tool.toolName && !deleting;

  const confirm = async () => {
    if (!canDelete) return;
    setDeleting(true);
    setError(null);
    try {
      await client.deleteTool(tool.backendId, tool.toolName);
      if (!mounted.current) return;
      onDeleted();
    } catch (err) {
      if (mounted.current) setError(err instanceof Error ? err.message : "Failed to delete tool");
    } finally {
      if (mounted.current) setDeleting(false);
    }
  };

  return (
    <div className="ag-modal-backdrop" role="presentation" onClick={() => !deleting && onClose()}>
      <section
        className="ag-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="tool-delete-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="ag-modal-header">
          <div>
            <div className="ag-eyebrow">Tool catalog</div>
            <h2 id="tool-delete-title" className="ag-modal-title">
              Delete {tool.toolName}
            </h2>
          </div>
        </div>
        <div className="ag-modal-section" style={{ display: "grid", gap: 12 }}>
          {error && (
            <div className="ag-alert ag-alert-error" role="alert">
              {error}
            </div>
          )}
          <p style={{ margin: 0 }}>
            This removes <span className="ag-mono">{tool.backendId}/{tool.toolName}</span> from the catalog. Agents
            will no longer be authorized to call it.
          </p>
          <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12.5 }}>
            Type the tool name to confirm
            <input
              className="ag-input ag-mono"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
            />
          </label>
        </div>
        <div className="ag-modal-section" style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button type="button" className="ag-btn ag-btn-secondary" onClick={onClose} disabled={deleting}>
            Cancel
          </button>
          <button type="button" className="ag-btn ag-btn-danger" onClick={confirm} disabled={!canDelete}>
            Delete tool
          </button>
        </div>
      </section>
    </div>
  );
}
