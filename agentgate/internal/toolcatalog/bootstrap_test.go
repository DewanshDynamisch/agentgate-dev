package toolcatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

const sharedBootstrap = "../../../deploy/shared/tools.bootstrap.json"

func TestSharedBootstrapFileLoadsAndMatchesLegacyRegistry(t *testing.T) {
	defs, err := LoadBootstrapFile(sharedBootstrap)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(defs) != 12 {
		t.Fatalf("want 12 definitions, got %d", len(defs))
	}
	cat, err := NewCatalog(defs)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}

	readSchema := `{"type":"object","properties":{"verbose":{"type":"boolean"}}}`
	type want struct {
		risk   toolregistry.RiskLevel
		schema string
		args   []argdecl.Declaration
	}
	wants := map[string]want{
		"read_status":  {toolregistry.RiskRead, readSchema, []argdecl.Declaration{{Name: "verbose", Type: argdecl.ArgTypeBool}}},
		"write_status": {toolregistry.RiskWrite, `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`, []argdecl.Declaration{{Name: "amount", Type: argdecl.ArgTypeInt}}},
		"admin_action": {toolregistry.RiskDestructive, readSchema, nil},
		"list_tickets": {toolregistry.RiskRead, `{"type":"object","properties":{"status_filter":{"type":"string"}}}`, []argdecl.Declaration{{Name: "status_filter", Type: argdecl.ArgTypeString}}},
		"update_ticket_status": {toolregistry.RiskWrite, `{"type":"object","properties":{"ticket_id":{"type":"string"},"new_status":{"type":"string"}}}`,
			[]argdecl.Declaration{{Name: "ticket_id", Type: argdecl.ArgTypeString, Required: true}, {Name: "new_status", Type: argdecl.ArgTypeString, Required: true}}},
		"delete_ticket": {toolregistry.RiskDestructive, `{"type":"object","properties":{"ticket_id":{"type":"string"}}}`,
			[]argdecl.Declaration{{Name: "ticket_id", Type: argdecl.ArgTypeString, Required: true}}},
	}
	snap := cat.Current()
	for _, backend := range []string{"mcp-probe", "default"} {
		for name, w := range wants {
			rec, ok := findRecord(snap, backend, name)
			if !ok {
				t.Fatalf("%s/%s missing", backend, name)
			}
			if rec.Risk != w.risk {
				t.Errorf("%s/%s risk %q want %q", backend, name, rec.Risk, w.risk)
			}
			fp, _ := toolregistry.FingerprintSchema([]byte(w.schema))
			if rec.RegisteredFingerprint != fp {
				t.Errorf("%s/%s fingerprint mismatch", backend, name)
			}
			ds := snap.ArgDecls[backend+"/"+name]
			if ds == nil {
				t.Fatalf("%s/%s missing arg decls", backend, name)
			}
			for _, d := range w.args {
				got, ok := ds.Get(d.Name)
				if !ok || got != d {
					t.Errorf("%s/%s arg %s = %+v ok=%v want %+v", backend, name, d.Name, got, ok, d)
				}
			}
		}
	}
}

func findRecord(s *Snapshot, backend, tool string) (toolregistry.GovernanceRecord, bool) {
	for _, r := range s.Registry.List() {
		if r.ToolID.BackendID == backend && r.ToolID.ToolName == tool {
			return r, true
		}
	}
	return toolregistry.GovernanceRecord{}, false
}

func TestLoadBootstrapFileRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"malformed":     `{`,
		"bad risk":      `{"tools":[{"backend_id":"b","tool_name":"t","risk":"admin","schema":{"type":"object"}}]}`,
		"bad schema":    `{"tools":[{"backend_id":"b","tool_name":"t","risk":"read","schema":[1]}]}`,
		"unknown field": `{"tools":[],"extra":1}`,
		"duplicate":     `{"tools":[{"backend_id":"b","tool_name":"t","risk":"read","schema":{"type":"object"}},{"backend_id":"b","tool_name":"t","risk":"read","schema":{"type":"object"}}]}`,
	}
	for name, body := range cases {
		p := filepath.Join(dir, "x.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadBootstrapFile(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := LoadBootstrapFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing file: expected error")
	}
}

func TestSeedIfEmptySeedsOnceAndDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	defs, err := LoadBootstrapFile(sharedBootstrap)
	if err != nil {
		t.Fatal(err)
	}
	seeded, err := SeedIfEmpty(ctx, s, defs, "bootstrap")
	if err != nil || !seeded {
		t.Fatalf("first seed: seeded=%v err=%v", seeded, err)
	}
	got, _ := s.Get(ctx, "default", "read_status")
	if got.UpdatedBy != "bootstrap" {
		t.Errorf("UpdatedBy = %q", got.UpdatedBy)
	}
	mod := *got
	mod.Risk = toolregistry.RiskWrite
	if err := s.Upsert(ctx, mod); err != nil {
		t.Fatal(err)
	}
	seeded, err = SeedIfEmpty(ctx, s, defs, "bootstrap")
	if err != nil || seeded {
		t.Fatalf("second seed: seeded=%v err=%v", seeded, err)
	}
	after, _ := s.Get(ctx, "default", "read_status")
	if after.Risk != toolregistry.RiskWrite {
		t.Error("second seed overwrote modified tool")
	}
}

func TestSeedIfEmptyInvalidDefinitionSeedsNothing(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	good := sampleDef("b", "ok")
	bad := sampleDef("b", "bad")
	bad.Risk = "admin"
	if _, err := SeedIfEmpty(ctx, s, []ToolDefinition{good, bad}, "bootstrap"); err == nil {
		t.Fatal("expected error")
	}
	if l, _ := s.List(ctx); len(l) != 0 {
		t.Errorf("partial seed: %d rows", len(l))
	}
}
