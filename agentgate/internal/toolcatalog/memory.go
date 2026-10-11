package toolcatalog

import (
	"context"
	"sort"
	"sync"
	"time"
)

type key struct{ backend, tool string }

// MemoryStore is an in-memory, concurrency-safe Store.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[key]ToolDefinition
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[key]ToolDefinition)}
}

func (m *MemoryStore) Migrate(context.Context) error { return nil }

func (m *MemoryStore) List(context.Context) ([]ToolDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ToolDefinition, 0, len(m.data))
	for _, d := range m.data {
		out = append(out, d.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BackendID != out[j].BackendID {
			return out[i].BackendID < out[j].BackendID
		}
		return out[i].ToolName < out[j].ToolName
	})
	return out, nil
}

func (m *MemoryStore) Get(_ context.Context, backendID, toolName string) (*ToolDefinition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.data[key{backendID, toolName}]
	if !ok {
		return nil, ErrNotFound
	}
	c := d.clone()
	return &c, nil
}

func (m *MemoryStore) Upsert(_ context.Context, def ToolDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if def.UpdatedAt.IsZero() {
		def.UpdatedAt = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key{def.BackendID, def.ToolName}] = def.clone()
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, backendID, toolName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key{backendID, toolName})
	return nil
}
