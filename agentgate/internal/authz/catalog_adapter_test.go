package authz

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/argdecl"
	"github.com/Dynamisch-LLC/agentgate/internal/decision"
	"github.com/Dynamisch-LLC/agentgate/internal/identity"
	"github.com/Dynamisch-LLC/agentgate/internal/toolcatalog"
	"github.com/Dynamisch-LLC/agentgate/internal/toolregistry"
	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
)

func catalogDef(tool string, risk toolregistry.RiskLevel, args ...argdecl.Declaration) toolcatalog.ToolDefinition {
	return toolcatalog.ToolDefinition{
		BackendID:  "desk",
		ToolName:   tool,
		Risk:       risk,
		SchemaJSON: json.RawMessage(`{"type":"object"}`),
		Arguments:  args,
	}
}

func catalogAdapter(t *testing.T, cat ToolCatalog) *Adapter {
	t.Helper()
	mapper, err := identity.NewMapper(identity.MapperConfig{AgentIDClaim: "sub", RolesClaim: "roles", OnBehalfOfClaim: "obo"})
	if err != nil {
		t.Fatal(err)
	}
	return NewAdapter(AdapterConfig{
		DefaultWorkspaceID:   "ws",
		AllowStaticWorkspace: true,
		DefaultBackendID:     "desk",
		IdentityMapper:       mapper,
		Catalog:              cat,
	})
}

func catalogCall(t *testing.T, tool, args string) *authv3.CheckRequest {
	t.Helper()
	return &authv3.CheckRequest{Attributes: &authv3.AttributeContext{
		Request: &authv3.AttributeContext_Request{Http: &authv3.AttributeContext_HttpRequest{
			Method: "POST", Path: "/",
			Body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`,
		}},
		MetadataContext: newValidJWTMetadata(t, "agent-1", "support", ""),
	}}
}

func TestCatalogAdapter_ReplaceMakesNewToolKnown(t *testing.T) {
	cat, err := toolcatalog.NewCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	a := catalogAdapter(t, cat)
	if _, aerr := a.Adapt(context.Background(), catalogCall(t, "close_ticket", `{}`)); aerr == nil || aerr.ReasonCode != decision.ReasonUnknownTool {
		t.Fatalf("expected unknown_tool, got %v", aerr)
	}
	if err := cat.Replace([]toolcatalog.ToolDefinition{catalogDef("close_ticket", toolregistry.RiskWrite)}); err != nil {
		t.Fatal(err)
	}
	req, aerr := a.Adapt(context.Background(), catalogCall(t, "close_ticket", `{}`))
	if aerr != nil {
		t.Fatal(aerr)
	}
	if !req.Classification.Known || req.Classification.Risk != "write" {
		t.Fatalf("unexpected classification %+v", req.Classification)
	}
}

func TestCatalogAdapter_ReclassifyToDestructive(t *testing.T) {
	cat, err := toolcatalog.NewCatalog([]toolcatalog.ToolDefinition{catalogDef("update_ticket_status", toolregistry.RiskWrite)})
	if err != nil {
		t.Fatal(err)
	}
	a := catalogAdapter(t, cat)
	req, aerr := a.Adapt(context.Background(), catalogCall(t, "update_ticket_status", `{}`))
	if aerr != nil || req.Classification.Risk != "write" {
		t.Fatalf("before: %v %+v", aerr, req.Classification)
	}
	if err := cat.Replace([]toolcatalog.ToolDefinition{catalogDef("update_ticket_status", toolregistry.RiskDestructive)}); err != nil {
		t.Fatal(err)
	}
	req, aerr = a.Adapt(context.Background(), catalogCall(t, "update_ticket_status", `{}`))
	if aerr != nil || req.Classification.Risk != "destructive" {
		t.Fatalf("after: %v %+v", aerr, req.Classification)
	}
}

func TestCatalogAdapter_RemovedToolIsUnknown(t *testing.T) {
	cat, err := toolcatalog.NewCatalog([]toolcatalog.ToolDefinition{
		catalogDef("a", toolregistry.RiskRead), catalogDef("b", toolregistry.RiskRead),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := catalogAdapter(t, cat)
	if _, aerr := a.Adapt(context.Background(), catalogCall(t, "a", `{}`)); aerr != nil {
		t.Fatal(aerr)
	}
	if err := cat.Replace([]toolcatalog.ToolDefinition{catalogDef("b", toolregistry.RiskRead)}); err != nil {
		t.Fatal(err)
	}
	_, aerr := a.Adapt(context.Background(), catalogCall(t, "a", `{}`))
	if aerr == nil || aerr.ReasonCode != decision.ReasonUnknownTool {
		t.Fatalf("expected unknown_tool, got %v", aerr)
	}
}

func TestCatalogAdapter_ArgDeclsKeyedByBackendAndTool(t *testing.T) {
	cat, err := toolcatalog.NewCatalog([]toolcatalog.ToolDefinition{
		catalogDef("t", toolregistry.RiskRead, argdecl.Declaration{Name: "id", Type: argdecl.ArgTypeString, Required: true}),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := catalogAdapter(t, cat)
	if _, aerr := a.Adapt(context.Background(), catalogCall(t, "t", `{}`)); aerr == nil || aerr.ReasonCode != decision.ReasonMalformedRequest {
		t.Fatalf("missing required arg must be rejected, got %v", aerr)
	}
	req, aerr := a.Adapt(context.Background(), catalogCall(t, "t", `{"id":"x"}`))
	if aerr != nil || req.Arguments["id"].String() != "x" {
		t.Fatalf("got %v %+v", aerr, req.Arguments)
	}
}

func TestCatalogAdapter_ConcurrentReplaceSnapshotConsistency(t *testing.T) {
	writeV := catalogDef("t", toolregistry.RiskWrite, argdecl.Declaration{Name: "w", Type: argdecl.ArgTypeString, Required: true})
	destrV := catalogDef("t", toolregistry.RiskDestructive, argdecl.Declaration{Name: "d", Type: argdecl.ArgTypeString, Required: true})
	cat, err := toolcatalog.NewCatalog([]toolcatalog.ToolDefinition{writeV})
	if err != nil {
		t.Fatal(err)
	}
	a := catalogAdapter(t, cat)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	report := func(s string) {
		select {
		case errs <- s:
		default:
		}
	}

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req, aerr := a.Adapt(context.Background(), catalogCall(t, "t", `{"w":"1","d":"2"}`))
				if aerr != nil {
					report("adapt error: " + aerr.Error())
					return
				}
				switch req.Classification.Risk {
				case "write":
					if _, ok := req.Arguments["w"]; !ok || len(req.Arguments) != 1 {
						report("write risk with mismatched argdecls")
						return
					}
				case "destructive":
					if _, ok := req.Arguments["d"]; !ok || len(req.Arguments) != 1 {
						report("destructive risk with mismatched argdecls")
						return
					}
				default:
					report("unexpected risk " + req.Classification.Risk)
					return
				}
			}
		}()
	}

	for i := 0; i < 1000; i++ {
		def := destrV
		if i%2 == 0 {
			def = writeV
		}
		if err := cat.Replace([]toolcatalog.ToolDefinition{def}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
