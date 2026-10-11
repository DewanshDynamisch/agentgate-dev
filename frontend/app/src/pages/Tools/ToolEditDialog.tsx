import { useEffect, useMemo, useRef, useState } from "react";
import { X } from "lucide-react";
import type { GovernanceClient } from "@contract/api/governanceClient";
import type { ToolArgument, ToolDefinition } from "@contract/models/tools";

const RISKS = ["read", "write", "destructive"] as const;
const ARG_TYPES = ["string", "int64", "bool"] as const;
const DESTRUCTIVE_CONFIRM_LABEL = "I understand this changes authorization for all agents";
const IDENT_RE = /^[a-zA-Z0-9_.-]{1,128}$/;
const DEFAULT_SCHEMA = JSON.stringify({ type: "object", properties: {} }, null, 2);

/** Returns an error message, or null when the text is a JSON object. Objects only: null, arrays and scalars are rejected. */
function validateSchemaText(text: string): { error: string | null; value?: Record<string, unknown> } {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { error: "Schema must be valid JSON." };
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    return { error: "Schema must be a JSON object." };
  }
  return { error: null, value: parsed as Record<string, unknown> };
}

interface Props {
  client: GovernanceClient;
  /** Existing tool to edit; omit to register a new one. */
  tool?: ToolDefinition;
  onClose: () => void;
  /** Called after a successful save. */
  onSaved: () => void;
}

export function ToolEditDialog({ client, tool, onClose, onSaved }: Props) {
  const editing = tool !== undefined;
  const [backendId, setBackendId] = useState(tool?.backendId ?? "");
  const [toolName, setToolName] = useState(tool?.toolName ?? "");
  const [risk, setRisk] = useState(tool?.risk ?? "read");
  const [description, setDescription] = useState(tool?.description ?? "");
  const [schemaText, setSchemaText] = useState(
    tool ? JSON.stringify(tool.schema ?? {}, null, 2) : DEFAULT_SCHEMA
  );
  const [args, setArgs] = useState<ToolArgument[]>(tool ? tool.arguments.map((a) => ({ ...a })) : []);
  const [confirmed, setConfirmed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [serverError, setServerError] = useState<string | null>(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !saving) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose, saving]);

  const schema = useMemo(() => validateSchemaText(schemaText), [schemaText]);
  // Destructive is a governed escalation: required whenever the saved risk is not already destructive.
  const needsConfirm = risk === "destructive" && tool?.risk !== "destructive";
  const backendIdError =
    editing || IDENT_RE.test(backendId) ? null : "Backend ID may only contain letters, digits, '_', '.', '-' (1-128 characters).";
  const toolNameError =
    editing || IDENT_RE.test(toolName) ? null : "Tool name may only contain letters, digits, '_', '.', '-' (1-128 characters).";
  const argError = useMemo(() => {
    const seen = new Set<string>();
    for (const a of args) {
      const n = a.name.trim();
      if (n === "") return "Argument names must not be empty.";
      if (seen.has(n)) return `Duplicate argument name: ${n}`;
      seen.add(n);
    }
    return null;
  }, [args]);
  const argsValid = argError === null;
  const identityValid = backendIdError === null && toolNameError === null;
  const canSave =
    !saving && schema.error === null && argsValid && identityValid && (!needsConfirm || confirmed);

  const updateArg = (i: number, patch: Partial<ToolArgument>) =>
    setArgs((prev) => prev.map((a, idx) => (idx === i ? { ...a, ...patch } : a)));

  const save = async () => {
    if (!canSave || schema.value === undefined) return;
    setSaving(true);
    setServerError(null);
    try {
      await client.upsertTool(backendId, toolName, {
        risk,
        schema: schema.value,
        arguments: args.map((a) => ({ ...a, name: a.name.trim() })),
        description,
      });
      if (!mounted.current) return;
      onSaved();
    } catch (err) {
      if (mounted.current) setServerError(err instanceof Error ? err.message : "Failed to save tool");
    } finally {
      if (mounted.current) setSaving(false);
    }
  };

  return (
    <div className="ag-modal-backdrop" role="presentation" onClick={() => !saving && onClose()}>
      <section
        className="ag-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="tool-edit-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="ag-modal-header">
          <div>
            <div className="ag-eyebrow">Tool catalog</div>
            <h2 id="tool-edit-title" className="ag-modal-title">
              {editing ? `Edit ${tool.toolName}` : "Register tool"}
            </h2>
          </div>
          <button type="button" className="ag-icon-button" onClick={onClose} aria-label="Close" title="Close">
            <X size={17} />
          </button>
        </div>

        <div className="ag-modal-section" style={{ display: "grid", gap: 12 }}>
          {serverError && (
            <div className="ag-alert ag-alert-error" role="alert">
              {serverError}
            </div>
          )}

          <label style={fieldStyle}>
            Backend ID
            <input
              className="ag-input ag-mono"
              value={backendId}
              readOnly={editing}
              onChange={(e) => setBackendId(e.target.value)}
              aria-invalid={backendIdError !== null && backendId !== ""}
            />
          </label>
          {backendIdError && backendId !== "" && <div style={errorStyle}>{backendIdError}</div>}
          <label style={fieldStyle}>
            Tool name
            <input
              className="ag-input ag-mono"
              value={toolName}
              readOnly={editing}
              onChange={(e) => setToolName(e.target.value)}
              aria-invalid={toolNameError !== null && toolName !== ""}
            />
          </label>
          {toolNameError && toolName !== "" && <div style={errorStyle}>{toolNameError}</div>}
          <label style={fieldStyle}>
            Risk
            <select className="ag-input" value={risk} onChange={(e) => {
                setRisk(e.target.value);
                setConfirmed(false);
              }}>
              {RISKS.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </label>
          {needsConfirm && (
            <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13.5 }}>
              <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} />
              {DESTRUCTIVE_CONFIRM_LABEL}
            </label>
          )}
          <label style={fieldStyle}>
            Description
            <input className="ag-input" value={description} onChange={(e) => setDescription(e.target.value)} />
          </label>
          <label style={fieldStyle}>
            Schema (JSON)
            <textarea
              className="ag-input ag-mono"
              rows={8}
              value={schemaText}
              onChange={(e) => setSchemaText(e.target.value)}
              aria-invalid={schema.error !== null}
              aria-describedby={schema.error ? "tool-schema-error" : undefined}
            />
          </label>
          {schema.error && (
            <div id="tool-schema-error" style={errorStyle}>
              {schema.error}
            </div>
          )}

          <fieldset style={{ border: "none", padding: 0, margin: 0, display: "grid", gap: 8 }}>
            <legend style={{ fontSize: 12.5, padding: 0, marginBottom: 4 }}>Arguments</legend>
            {args.map((a, i) => (
              <div key={i} style={{ display: "flex", gap: 8, alignItems: "center" }}>
                <input
                  className="ag-input ag-mono"
                  aria-label={`Argument ${i + 1} name`}
                  placeholder="name"
                  value={a.name}
                  onChange={(e) => updateArg(i, { name: e.target.value })}
                />
                <select
                  className="ag-input"
                  aria-label={`Argument ${i + 1} type`}
                  value={a.type}
                  onChange={(e) => updateArg(i, { type: e.target.value })}
                >
                  {ARG_TYPES.map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))}
                </select>
                <label style={{ display: "flex", alignItems: "center", gap: 4, fontSize: 13 }}>
                  <input
                    type="checkbox"
                    aria-label={`Argument ${i + 1} required`}
                    checked={a.required}
                    onChange={(e) => updateArg(i, { required: e.target.checked })}
                  />
                  required
                </label>
                <button
                  type="button"
                  className="ag-btn ag-btn-ghost ag-btn-sm"
                  aria-label={`Remove argument ${i + 1}`}
                  onClick={() => setArgs((prev) => prev.filter((_, idx) => idx !== i))}
                >
                  Remove
                </button>
              </div>
            ))}
            {argError && <div style={errorStyle}>{argError}</div>}
            <div>
              <button
                type="button"
                className="ag-btn ag-btn-secondary ag-btn-sm"
                onClick={() => setArgs((prev) => [...prev, { name: "", type: "string", required: false }])}
              >
                Add argument
              </button>
            </div>
          </fieldset>
        </div>

        <div className="ag-modal-section" style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button type="button" className="ag-btn ag-btn-secondary" onClick={onClose} disabled={saving}>
            Cancel
          </button>
          <button type="button" className="ag-btn ag-btn-primary" onClick={save} disabled={!canSave}>
            Save
          </button>
        </div>
      </section>
    </div>
  );
}

const fieldStyle = { display: "flex", flexDirection: "column", gap: 4, fontSize: 12.5 } as const;
const errorStyle = { color: "var(--ag-deny, #b42318)", fontSize: 12.5 } as const;
