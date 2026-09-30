package toolcatalog

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

// Snapshot is an immutable, internally consistent view of the catalog.
// ArgDecls is keyed by backendID + "/" + toolName.
type Snapshot struct {
	Registry *toolregistry.Registry
	ArgDecls map[string]*argdecl.DeclarationSet
}

// Catalog serves the current Snapshot and swaps it atomically.
type Catalog struct {
	cur atomic.Pointer[Snapshot]
}

// NewCatalog builds a Catalog from defs; any invalid definition is an error.
func NewCatalog(defs []ToolDefinition) (*Catalog, error) {
	snap, err := buildSnapshot(defs)
	if err != nil {
		return nil, err
	}
	c := &Catalog{}
	c.cur.Store(snap)
	return c, nil
}

// Replace builds a complete new Snapshot and swaps it in only if every
// definition is valid; on error the previous Snapshot stays live.
func (c *Catalog) Replace(defs []ToolDefinition) error {
	snap, err := buildSnapshot(defs)
	if err != nil {
		return err
	}
	c.cur.Store(snap)
	return nil
}

// Current returns the live Snapshot.
func (c *Catalog) Current() *Snapshot {
	return c.cur.Load()
}

// Reload lists definitions from s and replaces the Snapshot.
func (c *Catalog) Reload(ctx context.Context, s Store) error {
	defs, err := s.List(ctx)
	if err != nil {
		return fmt.Errorf("toolcatalog: reload: %w", err)
	}
	return c.Replace(defs)
}

func buildSnapshot(defs []ToolDefinition) (*Snapshot, error) {
	entries := make([]toolregistry.RegistryEntry, 0, len(defs))
	decls := make(map[string]*argdecl.DeclarationSet, len(defs))
	for _, d := range defs {
		if err := d.Validate(); err != nil {
			return nil, err
		}
		fp, err := toolregistry.FingerprintSchema(d.SchemaJSON)
		if err != nil {
			return nil, fmt.Errorf("toolcatalog: %s/%s: %w", d.BackendID, d.ToolName, err)
		}
		id := toolregistry.ToolID{BackendID: d.BackendID, ToolName: d.ToolName}
		entries = append(entries, toolregistry.RegistryEntry{ToolID: id, Risk: d.Risk, RegisteredFingerprint: fp})
		ds, err := argdecl.NewDeclarationSet(d.Arguments)
		if err != nil {
			return nil, fmt.Errorf("toolcatalog: %s: %w", id, err)
		}
		decls[id.String()] = ds
	}
	reg, err := toolregistry.NewRegistry(entries)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Registry: reg, ArgDecls: decls}, nil
}
