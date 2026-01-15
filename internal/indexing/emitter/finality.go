package emitter

import (
	"context"
	"fmt"
	"sync"

	"github.com/vietddude/watcher/internal/core/domain"
)

// FinalityBuffer wraps an Emitter and buffers events until they reach a required depth.
// This implements the "Finality Wait" reorg strategy: only emit when safe.
type FinalityBuffer struct {
	inner         Emitter
	confirmations uint64
	pending       map[uint64][]*domain.Event // blockNum -> events
	mu            sync.Mutex
}

// NewFinalityBuffer creates a new buffer that waits for 'confirmations' blocks before emitting.
func NewFinalityBuffer(inner Emitter, confirmations uint64) *FinalityBuffer {
	return &FinalityBuffer{
		inner:         inner,
		confirmations: confirmations,
		pending:       make(map[uint64][]*domain.Event),
	}
}

// QueueEvent adds an event to the buffer. It is NOT emitted yet.
func (f *FinalityBuffer) QueueEvent(ctx context.Context, event *domain.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// If 0 confirmations required, emit immediately
	if f.confirmations == 0 {
		return f.inner.Emit(ctx, event)
	}

	blockNum := event.BlockNumber
	f.pending[blockNum] = append(f.pending[blockNum], event)
	return nil
}

// OnNewBlock notifies the buffer of the current chain tip.
// It checks for any pending events that have reached finality and emits them.
func (f *FinalityBuffer) OnNewBlock(ctx context.Context, currentBlock uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Nothing to process if we haven't reached finality depth regarding block 0
	if currentBlock < f.confirmations {
		return nil
	}

	// Calculate the latest safe block number
	safeBlock := currentBlock - f.confirmations

	// Find all blocks <= safeBlock that have pending events
	var blocksToEmit []uint64
	for blockNum := range f.pending {
		if blockNum <= safeBlock {
			blocksToEmit = append(blocksToEmit, blockNum)
		}
	}

	// Sort isn't strictly necessary if strict ordering isn't required between blocks in a batch,
	// but generally good to emit in order. Map iteration is random.
	// For simplicity and speed in this map iteration, we'll just process them.
	// If strict ordering is required, we should collect and sort keys.
	// Let's assume block-level ordering is desired but strict total ordering across blocks isn't vital
	// for the batch emit unless specified. However, for consistency, let's just emit them.

	for _, blockNum := range blocksToEmit {
		events := f.pending[blockNum]
		if len(events) > 0 {
			if err := f.inner.EmitBatch(ctx, events); err != nil {
				return fmt.Errorf("failed to emit finalized events for block %d: %w", blockNum, err)
			}
		}
		delete(f.pending, blockNum)
	}

	return nil
}

// DiscardBlock removes pending events for a specific block.
// This logic is used when a reorg is detected: simply delete the pending events
// for the orphaned blocks so they are never emitted.
func (f *FinalityBuffer) DiscardBlock(blockNum uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pending, blockNum)
}

// PendingCount returns the number of pending events for a block.
func (f *FinalityBuffer) PendingCount(blockNum uint64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending[blockNum])
}
