package policystore

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestComputeVersion(t *testing.T) {
	content := "permit(principal, action, resource);"
	v1 := ComputeVersion(content)
	v2 := ComputeVersion(content)

	if v1 == "" {
		t.Fatal("expected non-empty version hash")
	}
	if v1 != v2 {
		t.Fatalf("expected deterministic version hash, got %s != %s", v1, v2)
	}

	diffContent := "forbid(principal, action, resource);"
	v3 := ComputeVersion(diffContent)
	if v1 == v3 {
		t.Fatalf("expected different hash for different content, got identical %s", v1)
	}
}

func testStoreBehavior(t *testing.T, store Store) {
	ctx := context.Background()

	ws1 := "workspace-alpha"
	ws2 := "workspace-beta"

	content1 := "permit(principal, action, resource);"
	v1 := ComputeVersion(content1)

	content2 := "forbid(principal, action, resource);"
	v2 := ComputeVersion(content2)

	// 1. Initially no active policy
	_, err := store.GetActivePolicy(ctx, ws1)
	if !errors.Is(err, ErrNoActivePolicy) {
		t.Fatalf("expected ErrNoActivePolicy, got: %v", err)
	}

	// 2. Create candidates
	rec1 := PolicyRecord{
		WorkspaceID: ws1,
		Version:     v1,
		Content:     content1,
		Description: "Initial permit policy",
	}
	if err := store.CreatePolicy(ctx, rec1); err != nil {
		t.Fatalf("CreatePolicy rec1 failed: %v", err)
	}

	rec2 := PolicyRecord{
		WorkspaceID: ws1,
		Version:     v2,
		Content:     content2,
		Description: "Candidate forbid policy",
	}
	if err := store.CreatePolicy(ctx, rec2); err != nil {
		t.Fatalf("CreatePolicy rec2 failed: %v", err)
	}

	// 3. GetPolicy
	got1, err := store.GetPolicy(ctx, ws1, v1)
	if err != nil {
		t.Fatalf("GetPolicy v1 failed: %v", err)
	}
	if got1.State != StateCandidate {
		t.Fatalf("expected initial state candidate, got %s", got1.State)
	}
	if got1.Content != content1 {
		t.Fatalf("content mismatch: got %q, want %q", got1.Content, content1)
	}

	// 4. Workspace isolation: ws2 should not see ws1's policies
	_, err = store.GetPolicy(ctx, ws2, v1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for ws2 looking up ws1 policy, got: %v", err)
	}

	listWs1, err := store.ListPolicies(ctx, ws1)
	if err != nil {
		t.Fatalf("ListPolicies ws1 failed: %v", err)
	}
	if len(listWs1) != 2 {
		t.Fatalf("expected 2 policies for ws1, got %d", len(listWs1))
	}

	listWs2, err := store.ListPolicies(ctx, ws2)
	if err != nil {
		t.Fatalf("ListPolicies ws2 failed: %v", err)
	}
	if len(listWs2) != 0 {
		t.Fatalf("expected 0 policies for ws2, got %d", len(listWs2))
	}

	// 5. Activate v1
	prev, err := store.ActivatePolicy(ctx, ws1, v1)
	if err != nil {
		t.Fatalf("ActivatePolicy v1 failed: %v", err)
	}
	if prev != "" {
		t.Fatalf("expected empty previousVersion on first activation, got %q", prev)
	}

	active, err := store.GetActivePolicy(ctx, ws1)
	if err != nil {
		t.Fatalf("GetActivePolicy failed: %v", err)
	}
	if active.Version != v1 || active.State != StateActive {
		t.Fatalf("expected active version %s with state active, got %+v", v1, active)
	}

	// 6. Activate v2 (atomic replacement)
	prev, err = store.ActivatePolicy(ctx, ws1, v2)
	if err != nil {
		t.Fatalf("ActivatePolicy v2 failed: %v", err)
	}
	if prev != v1 {
		t.Fatalf("expected previousVersion %s, got %s", v1, prev)
	}

	active, err = store.GetActivePolicy(ctx, ws1)
	if err != nil {
		t.Fatalf("GetActivePolicy failed: %v", err)
	}
	if active.Version != v2 || active.State != StateActive {
		t.Fatalf("expected active version %s, got %+v", v2, active)
	}

	// Verify v1 is now historical
	got1After, err := store.GetPolicy(ctx, ws1, v1)
	if err != nil {
		t.Fatalf("GetPolicy v1 failed: %v", err)
	}
	if got1After.State != StateHistorical {
		t.Fatalf("expected v1 state historical, got %s", got1After.State)
	}

	// 7. Activate unknown version should fail
	_, err = store.ActivatePolicy(ctx, ws1, "unknown-version-hash")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for activating unknown version, got %v", err)
	}

	// Active should still be v2
	active, err = store.GetActivePolicy(ctx, ws1)
	if err != nil || active.Version != v2 {
		t.Fatalf("expected active to remain %s after failed activation, got %+v", v2, active)
	}

	// 8. Rollback to v1
	rolledFrom, err := store.RollbackPolicy(ctx, ws1, v1)
	if err != nil {
		t.Fatalf("RollbackPolicy to v1 failed: %v", err)
	}
	if rolledFrom != v2 {
		t.Fatalf("expected rolledBackFrom %s, got %s", v2, rolledFrom)
	}

	active, err = store.GetActivePolicy(ctx, ws1)
	if err != nil || active.Version != v1 {
		t.Fatalf("expected active version %s after rollback, got %+v", v1, active)
	}

	// 9. Rollback to unknown version should fail and leave active unchanged
	_, err = store.RollbackPolicy(ctx, ws1, "unknown-version")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for rollback to unknown version, got %v", err)
	}

	active, err = store.GetActivePolicy(ctx, ws1)
	if err != nil || active.Version != v1 {
		t.Fatalf("expected active version to remain %s after failed rollback, got %+v", v1, active)
	}
}

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()
	testStoreBehavior(t, store)
}

func TestPostgresStore(t *testing.T) {
	connStr := os.Getenv("AGENTGATE_TEST_POSTGRES_URL")
	if connStr == "" {
		t.Skip("skipping PostgresStore test: AGENTGATE_TEST_POSTGRES_URL not set")
	}

	ctx := context.Background()
	store, err := NewPostgresStore(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect to Postgres: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate Postgres schema: %v", err)
	}

	testStoreBehavior(t, store)
}
