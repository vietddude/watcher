package emitter

import (
	"context"
	"fmt"

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

// LogEmitter implements Emitter interface for stdout logging
type LogEmitter struct{}

func (e *LogEmitter) Emit(ctx context.Context, event *domain.Event) error {
	// fmt.Printf("[EVENT] %s: %s (%s)\n", event.ChainID, event.EventType, event.Transaction.Hash)
	return nil
}
func (e *LogEmitter) EmitBatch(ctx context.Context, events []*domain.Event) error {
	for _, ev := range events {
		e.Emit(ctx, ev)
	}
	return nil
}

func (e *LogEmitter) EmitRevert(ctx context.Context, event *domain.Event, reason string) error {
	fmt.Printf("[REVERT] %s: %s\n", event.Transaction.Hash, reason)
	return nil
}
func (e *LogEmitter) Close() error { return nil }
