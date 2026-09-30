package toolcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
)

func sampleDef(backend, tool string) ToolDefinition {
	return ToolDefinition{
		BackendID:   backend,
		ToolName:    tool,
		Risk:        toolregistry.RiskWrite,
		SchemaJSON:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
		Arguments:   []argdecl.Declaration{{Name: "id", Type: argdecl.ArgTypeString, Required: true}},
		Description: "sample tool",
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedBy:   "tester",
	}
}

func canonSchema(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("schema unmarshal: %v", err)
	}
	return v
}

func runStoreBehavior(t *testing.T, s Store) {
	ctx := context.Background()

	if _, err := s.Get(ctx, "b1", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: want ErrNotFound, got %v", err)
	}

	def := sampleDef("b1", "tool_a")
	if err := s.Upsert(ctx, def); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.Get(ctx, "b1", "tool_a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BackendID != def.BackendID || got.ToolName != def.ToolName || got.Risk != def.Risk ||
		got.Description != def.Description || got.UpdatedBy != def.UpdatedBy ||
		!got.UpdatedAt.Equal(def.UpdatedAt) ||
		!reflect.DeepEqual(got.Arguments, def.Arguments) ||
		!reflect.DeepEqual(canonSchema(t, got.SchemaJSON), canonSchema(t, def.SchemaJSON)) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, def)
	}

	over := def
	over.Risk = toolregistry.RiskDestructive
	over.Description = "changed"
	over.Arguments = nil
	over.UpdatedBy = "other"
	if err := s.Upsert(ctx, over); err != nil {
		t.Fatalf("Upsert overwrite: %v", err)
	}
	got, err = s.Get(ctx, "b1", "tool_a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Risk != toolregistry.RiskDestructive || got.Description != "changed" || got.UpdatedBy != "other" || len(got.Arguments) != 0 {
		t.Fatalf("overwrite not applied: %+v", got)
	}

	for _, id := range [][2]string{{"b2", "z"}, {"b1", "tool_0"}, {"b2", "a"}} {
		if err := s.Upsert(ctx, sampleDef(id[0], id[1])); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var order []string
	for _, d := range list {
		order = append(order, d.BackendID+"/"+d.ToolName)
	}
	want := []string{"b1/tool_0", "b1/tool_a", "b2/a", "b2/z"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("List order = %v, want %v", order, want)
	}

	if err := s.Delete(ctx, "b1", "tool_a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, "b1", "tool_a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete: want ErrNotFound, got %v", err)
	}

	bad := sampleDef("b1", "bad")
	bad.Risk = "admin"
	if err := s.Upsert(ctx, bad); err == nil {
		t.Fatal("Upsert with invalid risk must fail")
	}
	if _, err := s.Get(ctx, "b1", "bad"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid def must not be stored, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	ok := sampleDef("b", "t")
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid def rejected: %v", err)
	}
	cases := map[string]func(*ToolDefinition){
		"empty backend":  func(d *ToolDefinition) { d.BackendID = "" },
		"bad tool name":  func(d *ToolDefinition) { d.ToolName = "a b" },
		"long tool name": func(d *ToolDefinition) { d.ToolName = strings.Repeat("a", 129) },
		"bad risk":       func(d *ToolDefinition) { d.Risk = "" },
		"bad schema":     func(d *ToolDefinition) { d.SchemaJSON = json.RawMessage(`{`) },
		"empty schema":   func(d *ToolDefinition) { d.SchemaJSON = nil },
		"bad arg type":   func(d *ToolDefinition) { d.Arguments = []argdecl.Declaration{{Name: "x", Type: "float"}} },
		"empty arg name": func(d *ToolDefinition) { d.Arguments = []argdecl.Declaration{{Name: "", Type: argdecl.ArgTypeBool}} },
		"dup arg": func(d *ToolDefinition) {
			d.Arguments = []argdecl.Declaration{{Name: "x", Type: argdecl.ArgTypeBool}, {Name: "x", Type: argdecl.ArgTypeString}}
		},
	}
	for name, mut := range cases {
		d := sampleDef("b", "t")
		mut(&d)
		if err := d.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestMemoryStore(t *testing.T) {
	runStoreBehavior(t, NewMemoryStore())
}
