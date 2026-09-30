package toolcatalog

import (
	"context"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

func catDef(backend, tool string, risk toolregistry.RiskLevel, args ...argdecl.Declaration) ToolDefinition {
	d := sampleDef(backend, tool)
	d.Risk = risk
	d.Arguments = args
	return d
}

func TestCatalog_NewAndCurrent(t *testing.T) {
	c, err := NewCatalog([]ToolDefinition{catDef("b1", "t1", toolregistry.RiskWrite, argdecl.Declaration{Name: "x", Type: argdecl.ArgTypeString})})
	if err != nil {
		t.Fatal(err)
	}
	snap := c.Current()
	rec := snap.Registry.Lookup(toolregistry.ToolID{BackendID: "b1", ToolName: "t1"}, "")
	if !rec.Known || rec.Risk != toolregistry.RiskWrite {
		t.Fatalf("unexpected record %+v", rec)
	}
	if _, ok := snap.ArgDecls["b1/t1"]; !ok {
		t.Fatal("argdecls must be keyed backendID/toolName")
	}
}

func TestCatalog_NewRejectsInvalid(t *testing.T) {
	bad := catDef("b1", "t1", "")
	if _, err := NewCatalog([]ToolDefinition{bad}); err == nil {
		t.Fatal("expected error")
	}
	dup := []ToolDefinition{catDef("b1", "t1", toolregistry.RiskRead), catDef("b1", "t1", toolregistry.RiskRead)}
	if _, err := NewCatalog(dup); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestCatalog_ReplaceInvalidKeepsOldSnapshot(t *testing.T) {
	c, err := NewCatalog([]ToolDefinition{catDef("b1", "t1", toolregistry.RiskRead)})
	if err != nil {
		t.Fatal(err)
	}
	before := c.Current()
	err = c.Replace([]ToolDefinition{catDef("b1", "t1", toolregistry.RiskWrite), catDef("b1", "t2", "bogus")})
	if err == nil {
		t.Fatal("expected error")
	}
	if c.Current() != before {
		t.Fatal("snapshot must be unchanged after failed Replace")
	}
}

func TestCatalog_ReplaceSwaps(t *testing.T) {
	c, _ := NewCatalog([]ToolDefinition{catDef("b1", "t1", toolregistry.RiskRead)})
	before := c.Current()
	if err := c.Replace([]ToolDefinition{catDef("b1", "t1", toolregistry.RiskDestructive)}); err != nil {
		t.Fatal(err)
	}
	if c.Current() == before {
		t.Fatal("expected new snapshot")
	}
	rec := c.Current().Registry.Lookup(toolregistry.ToolID{BackendID: "b1", ToolName: "t1"}, "")
	if rec.Risk != toolregistry.RiskDestructive {
		t.Fatalf("risk %q", rec.Risk)
	}
}

func TestCatalog_Reload(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Upsert(ctx, catDef("b1", "t1", toolregistry.RiskWrite)); err != nil {
		t.Fatal(err)
	}
	c, _ := NewCatalog(nil)
	if err := c.Reload(ctx, s); err != nil {
		t.Fatal(err)
	}
	rec := c.Current().Registry.Lookup(toolregistry.ToolID{BackendID: "b1", ToolName: "t1"}, "")
	if !rec.Known || rec.Risk != toolregistry.RiskWrite {
		t.Fatalf("unexpected %+v", rec)
	}
}
