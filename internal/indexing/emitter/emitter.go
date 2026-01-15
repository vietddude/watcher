package emitter

import (
	"context"

	"github.com/vietddude/watcher/internal/core/domain"
)

// Emitter defines the interface for emitting blockchain events
type Emitter interface {
	// Emit sends a single event
	Emit(ctx context.Context, event *domain.Event) error

	// EmitBatch sends multiple events
	EmitBatch(ctx context.Context, events []*domain.Event) error

	// EmitRevert sends a revert event for a transaction
	EmitRevert(ctx context.Context, originalEvent *domain.Event, reason string) error

	// Close closes the emitter connection
	Close() error
}
