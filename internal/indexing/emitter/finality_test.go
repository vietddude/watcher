package emitter

import (
	"context"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

// MockEmitter for testing
type MockEmitter struct {
	EmittedEvents     []*domain.Event
	EmittedBatchCount int
}

func (m *MockEmitter) Emit(ctx context.Context, event *domain.Event) error {
	m.EmittedEvents = append(m.EmittedEvents, event)
	return nil
}

func (m *MockEmitter) EmitBatch(ctx context.Context, events []*domain.Event) error {
	m.EmittedEvents = append(m.EmittedEvents, events...)
	m.EmittedBatchCount++
	return nil
}

func (m *MockEmitter) EmitRevert(ctx context.Context, originalEvent *domain.Event, reason string) error {
	return nil
}

func (m *MockEmitter) Close() error {
	return nil
}

func TestFinalityBuffer_QueueAndEmit(t *testing.T) {
	mock := &MockEmitter{}
	buffer := NewFinalityBuffer(mock, 10) // 10 confirmations required
	ctx := context.Background()

	event1 := &domain.Event{BlockNumber: 100, ID: "event1"}
	event2 := &domain.Event{BlockNumber: 101, ID: "event2"}

	// Queue events
	buffer.QueueEvent(ctx, event1)
	buffer.QueueEvent(ctx, event2)

	// Verify pending
	if count := buffer.PendingCount(100); count != 1 {
		t.Errorf("expected 1 pending event for block 100, got %d", count)
	}

	// New block 105: (105 - 100 = 5) < 10. Should NOT emit.
	buffer.OnNewBlock(ctx, 105)
	if len(mock.EmittedEvents) != 0 {
		t.Errorf("expected 0 emitted events, got %d", len(mock.EmittedEvents))
	}

	// New block 110: (110 - 100 = 10) >= 10. Should emit block 100.
	// Block 101: (110 - 101 = 9) < 10. Should keep block 101.
	buffer.OnNewBlock(ctx, 110)

	if len(mock.EmittedEvents) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(mock.EmittedEvents))
	}
	if mock.EmittedEvents[0].ID != "event1" {
		t.Errorf("expected event1 to be emitted, got %s", mock.EmittedEvents[0].ID)
	}

	// Verify block 100 is no longer pending
	if count := buffer.PendingCount(100); count != 0 {
		t.Errorf("expected 0 pending for block 100, got %d", count)
	}
	// Verify block 101 is still pending
	if count := buffer.PendingCount(101); count != 1 {
		t.Errorf("expected 1 pending for block 101, got %d", count)
	}
}

func TestFinalityBuffer_DiscardBlock(t *testing.T) {
	mock := &MockEmitter{}
	buffer := NewFinalityBuffer(mock, 10)
	ctx := context.Background()

	event := &domain.Event{BlockNumber: 100}
	buffer.QueueEvent(ctx, event)

	// Reorg detected! Discard block 100
	buffer.DiscardBlock(100)

	if count := buffer.PendingCount(100); count != 0 {
		t.Errorf("expected 0 pending events after discard, got %d", count)
	}

	// Advance chain far ahead
	buffer.OnNewBlock(ctx, 200)

	// Should emit nothing because it was discarded
	if len(mock.EmittedEvents) != 0 {
		t.Errorf("expected 0 emitted events, got %d", len(mock.EmittedEvents))
	}
}

func TestFinalityBuffer_ZeroConfirmations(t *testing.T) {
	mock := &MockEmitter{}
	buffer := NewFinalityBuffer(mock, 0)
	ctx := context.Background()

	event := &domain.Event{BlockNumber: 100}

	// Should emit immediately
	buffer.QueueEvent(ctx, event)

	if len(mock.EmittedEvents) != 1 {
		t.Errorf("expected 1 emitted event immediately, got %d", len(mock.EmittedEvents))
	}
}

func TestFinalityBuffer_MultipleBlocksEmit(t *testing.T) {
	mock := &MockEmitter{}
	buffer := NewFinalityBuffer(mock, 5)
	ctx := context.Background()

	// Blocks 100, 101, 102
	buffer.QueueEvent(ctx, &domain.Event{BlockNumber: 100, EmittedAt: time.Now()})
	buffer.QueueEvent(ctx, &domain.Event{BlockNumber: 101, EmittedAt: time.Now()})
	buffer.QueueEvent(ctx, &domain.Event{BlockNumber: 102, EmittedAt: time.Now()})

	// Jump to block 200 (finalizes all)
	buffer.OnNewBlock(ctx, 200)

	if len(mock.EmittedEvents) != 3 {
		t.Errorf("expected 3 emitted events, got %d", len(mock.EmittedEvents))
	}
}
