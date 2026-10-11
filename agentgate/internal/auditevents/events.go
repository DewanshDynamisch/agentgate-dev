package auditevents

import (
	"context"
	"sync"
	"time"
)

// MutationAction identifies the type of policy mutation.
type MutationAction string

const (
	// ActionActivate indicates a policy version was activated.
	ActionActivate MutationAction = "activate"

	// ActionRollback indicates a rollback to a previous version.
	ActionRollback MutationAction = "rollback"

	// ActionCreateCandidate indicates a new candidate was created.
	ActionCreateCandidate MutationAction = "create_candidate"

	// ActionToolUpserted indicates a tool catalog definition was created or replaced.
	ActionToolUpserted MutationAction = "tool_upserted"

	// ActionToolDeleted indicates a tool catalog definition was removed.
	ActionToolDeleted MutationAction = "tool_deleted"
)

// MutationEvent represents a single policy lifecycle mutation for audit purposes.
// G4 emits these via MutationListener; G5 will persist them durably.
type MutationEvent struct {
	WorkspaceID     string
	Action          MutationAction
	PreviousVersion string
	NewVersion      string
	CorrelationID   string
	Timestamp       time.Time
	OperatorID      string
}

type actorCtxKey struct{}

// WithActor returns a context carrying the authenticated actor identity for
// mutation attribution.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, actor)
}

// ActorFromContext returns the actor set by WithActor, or "" if none.
func ActorFromContext(ctx context.Context) string {
	a, _ := ctx.Value(actorCtxKey{}).(string)
	return a
}

// MutationListener receives mutation events. G4 provides a no-op and a recording
// implementation; G5 will provide the durable audit listener.
type MutationListener interface {
	OnMutation(ctx context.Context, event MutationEvent)
}

type noopListener struct{}

func (noopListener) OnMutation(_ context.Context, _ MutationEvent) {}

// NewNoopListener returns a listener that silently discards all events.
func NewNoopListener() MutationListener { return noopListener{} }

// RecordingListener captures events in memory for test assertions.
type RecordingListener struct {
	mu     sync.Mutex
	events []MutationEvent
}

// OnMutation records the event.
func (r *RecordingListener) OnMutation(_ context.Context, ev MutationEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

// Events returns a copy of all recorded events.
func (r *RecordingListener) Events() []MutationEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]MutationEvent, len(r.events))
	copy(out, r.events)
	return out
}
