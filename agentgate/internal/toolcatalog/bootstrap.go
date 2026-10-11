package toolcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

type bootstrapFile struct {
	Tools []bootstrapTool `json:"tools"`
}

type bootstrapTool struct {
	BackendID   string              `json:"backend_id"`
	ToolName    string              `json:"tool_name"`
	Risk        string              `json:"risk"`
	Schema      json.RawMessage     `json:"schema"`
	Arguments   []bootstrapArgument `json:"arguments"`
	Description string              `json:"description"`
}

type bootstrapArgument struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// LoadBootstrapFile reads and validates a bootstrap tool file. Unknown fields,
// invalid definitions and duplicate backend/tool pairs are errors.
func LoadBootstrapFile(path string) ([]ToolDefinition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("toolcatalog: read bootstrap file: %w", err)
	}
	var f bootstrapFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("toolcatalog: parse bootstrap file: %w", err)
	}
	seen := make(map[key]struct{}, len(f.Tools))
	defs := make([]ToolDefinition, 0, len(f.Tools))
	for _, t := range f.Tools {
		args := make([]argdecl.Declaration, 0, len(t.Arguments))
		for _, a := range t.Arguments {
			args = append(args, argdecl.Declaration{Name: a.Name, Type: argdecl.ArgType(a.Type), Required: a.Required})
		}
		d := ToolDefinition{
			BackendID:   t.BackendID,
			ToolName:    t.ToolName,
			Risk:        toolregistry.RiskLevel(t.Risk),
			SchemaJSON:  t.Schema,
			Arguments:   args,
			Description: t.Description,
		}
		if err := d.Validate(); err != nil {
			return nil, fmt.Errorf("toolcatalog: bootstrap %s/%s: %w", t.BackendID, t.ToolName, err)
		}
		k := key{d.BackendID, d.ToolName}
		if _, dup := seen[k]; dup {
			return nil, fmt.Errorf("toolcatalog: bootstrap duplicate %s/%s", d.BackendID, d.ToolName)
		}
		seen[k] = struct{}{}
		defs = append(defs, d)
	}
	return defs, nil
}

// SeedIfEmpty writes defs into s only when s holds no definitions. All
// definitions are validated before any write.
func SeedIfEmpty(ctx context.Context, s Store, defs []ToolDefinition, actor string) (bool, error) {
	existing, err := s.List(ctx)
	if err != nil {
		return false, fmt.Errorf("toolcatalog: seed: %w", err)
	}
	if len(existing) > 0 || len(defs) == 0 {
		return false, nil
	}
	for _, d := range defs {
		if err := d.Validate(); err != nil {
			return false, err
		}
	}
	for _, d := range defs {
		d.UpdatedBy = actor
		if err := s.Upsert(ctx, d); err != nil {
			return false, fmt.Errorf("toolcatalog: seed %s/%s: %w", d.BackendID, d.ToolName, err)
		}
	}
	return true, nil
}
