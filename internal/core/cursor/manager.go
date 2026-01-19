package cursor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

var (
	// ErrCursorNotFound is returned when a cursor doesn't exist.
	ErrCursorNotFound = errors.New("cursor not found")

	// ErrBlockGap is returned when a gap is detected during Advance.
	ErrBlockGap = errors.New("block gap detected")

	// ErrCursorPaused is returned when trying to advance a paused cursor.
	ErrCursorPaused = errors.New("cursor is paused")

	// ErrCursorInReorg is returned when trying to advance during reorg.
	ErrCursorInReorg = errors.New("cursor is in reorg state")
)

// Manager handles cursor operations with state machine enforcement.
type Manager interface {
	// Get retrieves the current cursor for a chain.
	Get(ctx context.Context, chainID string) (*domain.Cursor, error)

	// Initialize creates a new cursor at starting block.
	Initialize(ctx context.Context, chainID string, startBlock uint64) (*domain.Cursor, error)

	// Advance moves cursor forward (validates sequential, updates state).
	Advance(ctx context.Context, chainID string, blockNumber uint64, blockHash string) error

	// SetState transitions cursor to new state (validates transition).
	SetState(ctx context.Context, chainID string, newState State, reason string) error

	// Rollback moves cursor back for reorg (transitions to REORG state).
	Rollback(ctx context.Context, chainID string, safeBlock uint64, safeHash string) error

	// Pause pauses indexing.
	Pause(ctx context.Context, chainID string, reason string) error

	// Resume resumes indexing.
	Resume(ctx context.Context, chainID string) error

	// GetLag returns blocks behind current chain tip.
	GetLag(ctx context.Context, chainID string, latestBlock uint64) (int64, error)

	// SetMetadata updates cursor metadata.
	SetMetadata(ctx context.Context, chainID string, key string, value any) error

	// GetMetrics returns performance metrics for a chain.
	GetMetrics(chainID string) Metrics

	// SetStateChangeCallback registers callback for state changes.
	SetStateChangeCallback(fn func(chainID string, t Transition))
}

// DefaultManager implements Manager with state machine enforcement.
type DefaultManager struct {
	repo             storage.CursorRepository
	mu               sync.RWMutex
	stateCallback    func(string, Transition)
	blockTimeHistory map[string]*MetricsCollector
}

// Get retrieves the current cursor for a chain.
func (m *DefaultManager) Get(ctx context.Context, chainID string) (*domain.Cursor, error) {
	return m.repo.Get(ctx, chainID)
}

// Initialize creates a new cursor at starting block.
func (m *DefaultManager) Initialize(
	ctx context.Context,
	chainID string,
	startBlock uint64,
) (*domain.Cursor, error) {
	cursor := &domain.Cursor{
		ChainID:      chainID,
		CurrentBlock: startBlock,
		UpdatedAt:    uint64(time.Now().Unix()),
		State:        domain.CursorStateInit,
		Metadata:     make(map[string]any),
	}

	if err := m.repo.Save(ctx, cursor); err != nil {
		return nil, fmt.Errorf("failed to save cursor: %w", err)
	}

	// Initialize metrics collector
	m.mu.Lock()
	m.blockTimeHistory[chainID] = NewMetricsCollector(100)
	m.mu.Unlock()

	return cursor, nil
}

// Advance moves cursor forward after processing a block.
func (m *DefaultManager) Advance(
	ctx context.Context,
	chainID string,
	blockNumber uint64,
	blockHash string,
) error {
	cursor, err := m.repo.Get(ctx, chainID)
	if err != nil {
		return fmt.Errorf("failed to get cursor: %w", err)
	}

	// Validate state allows advancement
	switch cursor.State {
	case domain.CursorStatePaused:
		return ErrCursorPaused
	case domain.CursorStateReorg:
		return ErrCursorInReorg
	}

	// Check for gap (block must be exactly current + 1)
	expectedBlock := cursor.CurrentBlock + 1

	// Check for idempotency (duplicate delivery / re-process)
	if blockNumber == cursor.CurrentBlock {
		if blockHash == cursor.CurrentBlockHash {
			// Already processed this exact block. Treat as success.
			return nil
		}
		// If hash mismatch, it might be a reorganization or error.
		// For now, fail with specific error so we can distinguish it.
		return fmt.Errorf(
			"idempotency check failed: cursor at %d with hash %s, got same block %d with hash %s",
			cursor.CurrentBlock,
			cursor.CurrentBlockHash,
			blockNumber,
			blockHash,
		)
	}

	if blockNumber != expectedBlock {
		return fmt.Errorf("%w: expected block %d, got %d", ErrBlockGap, expectedBlock, blockNumber)
	}

	// Update cursor
	if err := m.repo.UpdateBlock(ctx, chainID, blockNumber, blockHash); err != nil {
		return fmt.Errorf("failed to update cursor: %w", err)
	}

	// Record metrics
	m.mu.Lock()
	if collector, ok := m.blockTimeHistory[chainID]; ok {
		collector.RecordBlock(blockNumber, time.Now())
	}
	m.mu.Unlock()

	return nil
}

// SetState transitions cursor to a new state.
func (m *DefaultManager) SetState(
	ctx context.Context,
	chainID string,
	newState State,
	reason string,
) error {
	cursor, err := m.repo.Get(ctx, chainID)
	if err != nil {
		return fmt.Errorf("failed to get cursor: %w", err)
	}

	// Validate transition
	if !CanTransition(cursor.State, newState) {
		return fmt.Errorf(
			"%w: cannot transition from %s to %s",
			ErrInvalidTransition,
			cursor.State,
			newState,
		)
	}

	// Create transition record
	transition := NewTransition(cursor.State, newState, reason)

	// Update state in repository
	if err := m.repo.UpdateState(ctx, chainID, newState); err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	// Record transition in metrics
	m.mu.Lock()
	if collector, ok := m.blockTimeHistory[chainID]; ok {
		collector.RecordTransition(transition)
	}
	m.mu.Unlock()

	// Invoke callback
	if m.stateCallback != nil {
		m.stateCallback(chainID, transition)
	}

	return nil
}

// Rollback moves cursor back for reorg handling.
func (m *DefaultManager) Rollback(
	ctx context.Context,
	chainID string,
	safeBlock uint64,
	safeHash string,
) error {
	cursor, err := m.repo.Get(ctx, chainID)
	if err != nil {
		return fmt.Errorf("failed to get cursor: %w", err)
	}

	// Transition to REORG state if not already
	if cursor.State != domain.CursorStateReorg {
		transition := NewTransition(
			cursor.State,
			domain.CursorStateReorg,
			fmt.Sprintf("rollback to block %d", safeBlock),
		)

		if err := m.repo.UpdateState(ctx, chainID, domain.CursorStateReorg); err != nil {
			return fmt.Errorf("failed to set reorg state: %w", err)
		}

		// Record transition
		m.mu.Lock()
		if collector, ok := m.blockTimeHistory[chainID]; ok {
			collector.RecordTransition(transition)
		}
		m.mu.Unlock()

		if m.stateCallback != nil {
			m.stateCallback(chainID, transition)
		}
	}

	// Rollback cursor position
	if err := m.repo.Rollback(ctx, chainID, safeBlock, safeHash); err != nil {
		return fmt.Errorf("failed to rollback cursor: %w", err)
	}

	return nil
}

// Pause pauses indexing for a chain.
func (m *DefaultManager) Pause(ctx context.Context, chainID string, reason string) error {
	return m.SetState(ctx, chainID, domain.CursorStatePaused, reason)
}

// Resume resumes indexing for a chain.
func (m *DefaultManager) Resume(ctx context.Context, chainID string) error {
	cursor, err := m.repo.Get(ctx, chainID)
	if err != nil {
		return fmt.Errorf("failed to get cursor: %w", err)
	}

	// Resume to scanning state
	if cursor.State != domain.CursorStatePaused {
		return fmt.Errorf("cursor is not paused, current state: %s", cursor.State)
	}

	return m.SetState(ctx, chainID, domain.CursorStateScanning, "manual resume")
}

// GetLag returns how many blocks behind the chain tip.
func (m *DefaultManager) GetLag(
	ctx context.Context,
	chainID string,
	latestBlock uint64,
) (int64, error) {
	cursor, err := m.repo.Get(ctx, chainID)
	if err != nil {
		return 0, fmt.Errorf("failed to get cursor: %w", err)
	}

	return int64(latestBlock) - int64(cursor.CurrentBlock), nil
}

// SetMetadata updates cursor metadata.
func (m *DefaultManager) SetMetadata(
	ctx context.Context,
	chainID string,
	key string,
	value any,
) error {
	cursor, err := m.repo.Get(ctx, chainID)
	if err != nil {
		return fmt.Errorf("failed to get cursor: %w", err)
	}

	if cursor.Metadata == nil {
		cursor.Metadata = make(map[string]any)
	}
	cursor.Metadata[key] = value

	return m.repo.Save(ctx, cursor)
}

// GetMetrics returns performance metrics for a chain.
func (m *DefaultManager) GetMetrics(chainID string) Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if collector, ok := m.blockTimeHistory[chainID]; ok {
		return collector.GetMetrics()
	}

	return Metrics{}
}

// SetStateChangeCallback registers a callback for state changes.
func (m *DefaultManager) SetStateChangeCallback(fn func(chainID string, t Transition)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateCallback = fn
}
