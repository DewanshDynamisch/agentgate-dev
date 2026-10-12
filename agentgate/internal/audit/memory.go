package audit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Dynamisch-LLC/agentgate/internal/auditevents"
)

// MemoryStore provides a thread-safe, in-memory implementation of Store for tests and local mocks.
type MemoryStore struct {
	mu       sync.RWMutex
	records  map[string][]StoredRecord // workspace_id -> records ordered by sequence
	globalID int64
	fault    error
}

// NewMemoryStore creates a new in-memory audit store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		records: make(map[string][]StoredRecord),
	}
}

// SetFault configures a synthetic error returned by write operations to simulate DB outages.
func (m *MemoryStore) SetFault(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fault = err
}

// SimulateTamper overrides a stored record at a specific sequence number to simulate unauthorized DB tampering.
func (m *MemoryStore) SimulateTamper(workspaceID string, seq int64, tampered StoredRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	recs := m.records[workspaceID]
	for i := range recs {
		if recs[i].SequenceNumber == seq {
			recs[i] = tampered
			return
		}
	}
}

// AppendDecision persists a decision record into memory.
func (m *MemoryStore) AppendDecision(_ context.Context, record DecisionRecord) (*StoredRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.fault != nil {
		return nil, m.fault
	}

	if record.WorkspaceID == "" {
		return nil, fmt.Errorf("audit: workspace_id is required")
	}

	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	if record.EventType == "" {
		record.EventType = EventTypeDecision
	}

	m.globalID++
	recs := m.records[record.WorkspaceID]
	seq := int64(len(recs) + 1)

	stored := StoredRecord{
		ID:                      m.globalID,
		WorkspaceID:             record.WorkspaceID,
		SequenceNumber:          seq,
		ExecutionID:             record.ExecutionID,
		Timestamp:               record.Timestamp,
		EventType:               record.EventType,
		Decision:                record.Decision,
		Reason:                  record.Reason,
		PrincipalAgentID:        record.PrincipalAgentID,
		PrincipalRoles:          record.PrincipalRoles,
		PrincipalOnBehalfOf:     record.PrincipalOnBehalfOf,
		ToolBackendID:           record.ToolBackendID,
		ToolName:                record.ToolName,
		ToolRisk:                record.ToolRisk,
		PolicyVersion:           record.PolicyVersion,
		PolicyHash:              record.PolicyHash,
		RedactedArguments:       record.RedactedArguments,
		DownstreamCredentialRef: record.DownstreamCredentialRef,
		CanonicalPayload:        record.CanonicalPayload,
		PrevHash:                record.PrevHash,
		RowHash:                 record.RowHash,
	}

	m.records[record.WorkspaceID] = append(recs, stored)
	return &stored, nil
}

// AppendMutation persists a policy mutation event into memory.
func (m *MemoryStore) AppendMutation(ctx context.Context, event auditevents.MutationEvent, prevHash string) (*StoredRecord, error) {
	rec := DecisionRecord{
		WorkspaceID:       event.WorkspaceID,
		ExecutionID:       event.CorrelationID,
		Timestamp:         event.Timestamp,
		EventType:         EventTypeMutation,
		Decision:          "MUTATION",
		Reason:            string(event.Action),
		PrincipalAgentID:  event.OperatorID,
		PolicyVersion:     event.NewVersion,
		PolicyHash:        event.PreviousVersion,
		PrevHash:          prevHash,
		RedactedArguments: make(map[string]string),
	}

	return m.AppendDecision(ctx, rec)
}

// GetLatestRecord returns the most recent record for the specified workspace.
func (m *MemoryStore) GetLatestRecord(_ context.Context, workspaceID string) (*StoredRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	recs, ok := m.records[workspaceID]
	if !ok || len(recs) == 0 {
		return nil, ErrNotFound
	}

	last := recs[len(recs)-1]
	return &last, nil
}

// ListRecords returns up to limit records in descending order of sequence.
func (m *MemoryStore) ListRecords(_ context.Context, workspaceID string, limit int) ([]StoredRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	recs, ok := m.records[workspaceID]
	if !ok || len(recs) == 0 {
		return []StoredRecord{}, nil
	}

	n := len(recs)
	if limit <= 0 || limit > n {
		limit = n
	}

	out := make([]StoredRecord, 0, limit)
	for i := n - 1; i >= n-limit; i-- {
		out = append(out, recs[i])
	}
	return out, nil
}

func (m *MemoryStore) ListRecordsBefore(_ context.Context, workspaceID string, beforeSequence int64, limit int) ([]StoredRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	recs, ok := m.records[workspaceID]
	if !ok || len(recs) == 0 {
		return []StoredRecord{}, nil
	}
	if limit <= 0 {
		limit = len(recs)
	}

	out := make([]StoredRecord, 0, limit)
	for i := len(recs) - 1; i >= 0 && len(out) < limit; i-- {
		if recs[i].SequenceNumber < beforeSequence {
			out = append(out, recs[i])
		}
	}
	return out, nil
}

// GetRecordBySequence finds a specific record by workspace and sequence number.
func (m *MemoryStore) GetRecordBySequence(_ context.Context, workspaceID string, seq int64) (*StoredRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	recs, ok := m.records[workspaceID]
	if !ok {
		return nil, ErrNotFound
	}

	for _, r := range recs {
		if r.SequenceNumber == seq {
			return &r, nil
		}
	}

	return nil, ErrNotFound
}

// Migrate is a no-op for in-memory storage.
func (m *MemoryStore) Migrate(_ context.Context) error {
	return nil
}

// Close releases resources.
func (m *MemoryStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = make(map[string][]StoredRecord)
	return nil
}
