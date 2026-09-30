/**
 * Runtime tool catalog models mirroring govapi.ToolDefinitionView and
 * govapi.PutToolRequest (agentgate/internal/govapi/tools_catalog.go).
 */
export interface ToolArgument {
  name: string;
  type: string;
  required: boolean;
}

export interface ToolDefinition {
  backendId: string;
  toolName: string;
  risk: string;
  /** JSON Schema document (opaque to the frontend). */
  schema: unknown;
  arguments: ToolArgument[];
  description: string;
  updatedAt: string;
  updatedBy: string;
}

/** Body of PUT /api/v1/tools/{backend_id}/{tool_name}; identity is in the path. */
export interface ToolUpsert {
  risk: string;
  schema: unknown;
  arguments: ToolArgument[];
  description: string;
}
