// Package toolcatalog persists operator-defined tool definitions (risk,
// schema, policy-relevant argument declarations) keyed by backend and tool.
package toolcatalog

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`)

// ToolDefinition is one catalog entry.
type ToolDefinition struct {
	BackendID   string                 `json:"backend_id"`
	ToolName    string                 `json:"tool_name"`
	Risk        toolregistry.RiskLevel `json:"risk"`
	SchemaJSON  json.RawMessage        `json:"schema_json"`
	Arguments   []argdecl.Declaration  `json:"arguments"`
	Description string                 `json:"description"`
	UpdatedAt   time.Time              `json:"updated_at"`
	UpdatedBy   string                 `json:"updated_by"`
}

// Validate fails closed on any malformed field.
func (d ToolDefinition) Validate() error {
	if !(toolregistry.ToolID{BackendID: d.BackendID, ToolName: d.ToolName}).Valid() {
		return fmt.Errorf("toolcatalog: backend_id and tool_name are required")
	}
	if !toolNamePattern.MatchString(d.ToolName) {
		return fmt.Errorf("toolcatalog: invalid tool_name %q", d.ToolName)
	}
	if !toolregistry.ValidRisk(d.Risk) {
		return fmt.Errorf("toolcatalog: invalid risk %q", d.Risk)
	}
	if len(d.SchemaJSON) == 0 || !json.Valid(d.SchemaJSON) {
		return fmt.Errorf("toolcatalog: schema_json must be valid JSON")
	}
	var schemaObj map[string]json.RawMessage
	if err := json.Unmarshal(d.SchemaJSON, &schemaObj); err != nil || schemaObj == nil {
		return fmt.Errorf("toolcatalog: schema_json must be a JSON object")
	}
	if _, err := toolregistry.FingerprintSchema(d.SchemaJSON); err != nil {
		return fmt.Errorf("toolcatalog: schema_json: %w", err)
	}
	seen := make(map[string]struct{}, len(d.Arguments))
	for _, a := range d.Arguments {
		if a.Name == "" {
			return fmt.Errorf("toolcatalog: argument name is required")
		}
		if !argdecl.ValidArgType(a.Type) {
			return fmt.Errorf("toolcatalog: argument %q has invalid type %q", a.Name, a.Type)
		}
		if _, dup := seen[a.Name]; dup {
			return fmt.Errorf("toolcatalog: duplicate argument %q", a.Name)
		}
		seen[a.Name] = struct{}{}
	}
	return nil
}

func (d ToolDefinition) clone() ToolDefinition {
	c := d
	c.SchemaJSON = append(json.RawMessage(nil), d.SchemaJSON...)
	if d.Arguments != nil {
		c.Arguments = append([]argdecl.Declaration(nil), d.Arguments...)
	}
	return c
}
