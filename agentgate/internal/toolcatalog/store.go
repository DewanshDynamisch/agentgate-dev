package toolcatalog

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a definition does not exist.
var ErrNotFound = errors.New("toolcatalog: not found")

// Store persists tool definitions.
type Store interface {
	List(ctx context.Context) ([]ToolDefinition, error)
	Get(ctx context.Context, backendID, toolName string) (*ToolDefinition, error)
	Upsert(ctx context.Context, def ToolDefinition) error
	Delete(ctx context.Context, backendID, toolName string) error
	Migrate(ctx context.Context) error
}
