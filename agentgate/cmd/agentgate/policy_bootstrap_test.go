package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/auditevents"
	"github.com/Dynamisch-LLC/agentgate/internal/fixturepolicy"
	"github.com/Dynamisch-LLC/agentgate/internal/policymanager"
	"github.com/Dynamisch-LLC/agentgate/internal/policystore"
)

type recordingListener struct {
	events []auditevents.MutationEvent
}

func (l *recordingListener) OnMutation(ctx context.Context, event auditevents.MutationEvent) {
	l.events = append(l.events, event)
}

func TestBootstrapDefaultPolicy(t *testing.T) {
	tests := []struct {
		name         string
		setupStore   func(t *testing.T) policystore.Store
		wantErr      bool
		wantVersion  string
		wantEvents   int
	}{
		{
			name: "fresh store => baseline seeded and active",
			setupStore: func(t *testing.T) policystore.Store {
				return policystore.NewMemoryStore()
			},
			wantErr: false,
			wantVersion: "v1.0.0",
			wantEvents: 2, // create and activate
		},
		{
			name: "store where manager A created and activated a non-baseline policy v2",
			setupStore: func(t *testing.T) policystore.Store {
				store := policystore.NewMemoryStore()
				m := policymanager.New(store)
				_, err := m.CreateCandidateWithVersion(context.Background(), "default", "v2", fixturepolicy.CedarSource + "\n// modified", "v2")
				if err != nil {
					t.Fatalf("setup create: %v", err)
				}
				if _, err := m.Activate(context.Background(), "default", "v2"); err != nil {
					t.Fatalf("setup activate: %v", err)
				}
				return store
			},
			wantErr: false,
			wantVersion: "v2",
			wantEvents: 0,
		},
		{
			name: "store with baseline active => bootstrap twice => no new events on second call",
			setupStore: func(t *testing.T) policystore.Store {
				store := policystore.NewMemoryStore()
				m := policymanager.New(store)
				_, err := m.CreateCandidateWithVersion(context.Background(), "default", "v1.0.0", fixturepolicy.CedarSource, "default baseline policy")
				if err != nil {
					t.Fatalf("setup create: %v", err)
				}
				if _, err := m.Activate(context.Background(), "default", "v1.0.0"); err != nil {
					t.Fatalf("setup activate: %v", err)
				}
				return store
			},
			wantErr: false,
			wantVersion: "v1.0.0",
			wantEvents: 0,
		},
		{
			name: "active policy whose stored content is corrupt/unparseable",
			setupStore: func(t *testing.T) policystore.Store {
				store := policystore.NewMemoryStore()
				return &corruptStore{Store: store}
			},
			wantErr: true,
			wantVersion: "",
			wantEvents: 0,
		},
		{
			name: "store with v1.0.0 present but no active policy",
			setupStore: func(t *testing.T) policystore.Store {
				store := policystore.NewMemoryStore()
				m := policymanager.New(store)
				_, err := m.CreateCandidateWithVersion(context.Background(), "default", "v1.0.0", fixturepolicy.CedarSource, "default baseline policy")
				if err != nil {
					t.Fatalf("setup create: %v", err)
				}
				return store
			},
			wantErr: false,
			wantVersion: "v1.0.0",
			wantEvents: 1, // only activate, not create
		},
		{
			name: "postgres: store where manager A created and activated a non-baseline policy v2",
			setupStore: func(t *testing.T) policystore.Store {
				dsn := os.Getenv("AGENTGATE_TEST_POSTGRES_URL")
				if dsn == "" {
					t.Skip("skipping postgres test; set AGENTGATE_TEST_POSTGRES_URL")
				}
				ctx := context.Background()
				store, err := policystore.NewPostgresStore(ctx, dsn)
				if err != nil {
					t.Fatalf("NewPostgresStore failed: %v", err)
				}
				// we won't close here, as it will be used by the test
				if err := store.Migrate(ctx); err != nil {
					t.Fatalf("Migrate failed: %v", err)
				}
				// The DB should be clean for this isolated test run
				m := policymanager.New(store)
				_, err = m.CreateCandidateWithVersion(ctx, "default", "v2", fixturepolicy.CedarSource+"\n// modified", "v2")
				if err != nil {
					t.Fatalf("setup create v2: %v", err)
				}
				if _, err := m.Activate(ctx, "default", "v2"); err != nil {
					t.Fatalf("setup activate v2: %v", err)
				}
				return store
			},
			wantErr: false,
			wantVersion: "v2",
			wantEvents: 0,
		},
		{
			name: "GetActivePolicy error propagates",
			setupStore: func(t *testing.T) policystore.Store {
				return &errorStore{Store: policystore.NewMemoryStore()}
			},
			wantErr: true,
			wantVersion: "",
			wantEvents: 0,
		},
		{
			name: "Activate error propagates",
			setupStore: func(t *testing.T) policystore.Store {
				return &activateErrorStore{Store: policystore.NewMemoryStore()}
			},
			wantErr: true,
			wantVersion: "",
			wantEvents: 1, // CreateCandidate succeeds, Activate fails
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setupStore(t)
			
			listener := &recordingListener{}
			m := policymanager.NewWithListener(store, listener)
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))

			err := bootstrapDefaultPolicy(context.Background(), m, logger)
			if (err != nil) != tt.wantErr {
				t.Fatalf("bootstrap() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				ver, _, err := m.GetActiveProvenance("default")
				if err != nil {
					t.Fatalf("GetActiveProvenance error: %v", err)
				}
				if ver != tt.wantVersion {
					t.Errorf("got version %q, want %q", ver, tt.wantVersion)
				}
			}

			if len(listener.events) != tt.wantEvents {
				t.Errorf("got %d events, want %d", len(listener.events), tt.wantEvents)
			}
		})
	}
}

type corruptStore struct {
	policystore.Store
}
func (c *corruptStore) GetActivePolicy(ctx context.Context, workspaceID string) (policystore.PolicyRecord, error) {
	return policystore.PolicyRecord{
		WorkspaceID: workspaceID,
		Version:     "v3",
		Content:     "invalid cedar",
		State:       policystore.StateActive,
	}, nil
}

type errorStore struct {
	policystore.Store
}
func (e *errorStore) GetActivePolicy(ctx context.Context, workspaceID string) (policystore.PolicyRecord, error) {
	return policystore.PolicyRecord{}, os.ErrPermission // Some generic error
}

type activateErrorStore struct {
	policystore.Store
}
func (e *activateErrorStore) GetActivePolicy(ctx context.Context, workspaceID string) (policystore.PolicyRecord, error) {
	return policystore.PolicyRecord{}, policystore.ErrNoActivePolicy
}
func (e *activateErrorStore) ActivatePolicy(ctx context.Context, workspaceID, version string) (string, error) {
	return "", os.ErrPermission
}
