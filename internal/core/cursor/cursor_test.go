package cursor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

// =============================================================================
// Mock Repository
// =============================================================================

type mockCursorRepo struct {
	mu      sync.RWMutex
	cursors map[string]*domain.Cursor
}

func newMockCursorRepo() *mockCursorRepo {
	return &mockCursorRepo{
		cursors: make(map[string]*domain.Cursor),
	}
}

func (r *mockCursorRepo) Get(ctx context.Context, chainID string) (*domain.Cursor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cursor, ok := r.cursors[chainID]
	if !ok {
		return nil, ErrCursorNotFound
	}
	// Return a copy
	c := *cursor
	return &c, nil
}

func (r *mockCursorRepo) Save(ctx context.Context, cursor *domain.Cursor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c := *cursor
	c.UpdatedAt = time.Now()
	r.cursors[cursor.ChainID] = &c
	return nil
}

func (r *mockCursorRepo) UpdateBlock(ctx context.Context, chainID string, blockNumber uint64, blockHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cursor, ok := r.cursors[chainID]
	if !ok {
		return ErrCursorNotFound
	}
	cursor.CurrentBlock = blockNumber
	cursor.CurrentBlockHash = blockHash
	cursor.UpdatedAt = time.Now()
	return nil
}

func (r *mockCursorRepo) UpdateState(ctx context.Context, chainID string, state domain.CursorState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cursor, ok := r.cursors[chainID]
	if !ok {
		return ErrCursorNotFound
	}
	cursor.State = state
	cursor.UpdatedAt = time.Now()
	return nil
}

func (r *mockCursorRepo) Rollback(ctx context.Context, chainID string, blockNumber uint64, blockHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cursor, ok := r.cursors[chainID]
	if !ok {
		return ErrCursorNotFound
	}
	cursor.CurrentBlock = blockNumber
	cursor.CurrentBlockHash = blockHash
	cursor.UpdatedAt = time.Now()
	return nil
}

// =============================================================================
// State Transition Tests
// =============================================================================

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     State
		to       State
		expected bool
	}{
		{"init to scanning", domain.CursorStateInit, domain.CursorStateScanning, true},
		{"init to catchup", domain.CursorStateInit, domain.CursorStateCatchup, true},
		{"init to reorg", domain.CursorStateInit, domain.CursorStateReorg, false},
		{"scanning to catchup", domain.CursorStateScanning, domain.CursorStateCatchup, true},
		{"scanning to reorg", domain.CursorStateScanning, domain.CursorStateReorg, true},
		{"scanning to paused", domain.CursorStateScanning, domain.CursorStatePaused, true},
		{"paused to scanning", domain.CursorStatePaused, domain.CursorStateScanning, true},
		{"paused to reorg", domain.CursorStatePaused, domain.CursorStateReorg, false},
		{"reorg to scanning", domain.CursorStateReorg, domain.CursorStateScanning, true},
		{"backfill to reorg", domain.CursorStateBackfill, domain.CursorStateReorg, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanTransition(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestTransitionIsValid(t *testing.T) {
	valid := NewTransition(domain.CursorStateScanning, domain.CursorStatePaused, "maintenance")
	if !valid.IsValid() {
		t.Error("expected transition scanning->paused to be valid")
	}

	invalid := NewTransition(domain.CursorStatePaused, domain.CursorStateReorg, "unexpected")
	if invalid.IsValid() {
		t.Error("expected transition paused->reorg to be invalid")
	}
}

// =============================================================================
// Manager Tests
// =============================================================================

func TestManagerInitialize(t *testing.T) {
	repo := newMockCursorRepo()
	manager := NewManager(repo)

	ctx := context.Background()
	cursor, err := manager.Initialize(ctx, "ethereum", 1000)

	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if cursor.ChainID != "ethereum" {
		t.Errorf("expected chainID 'ethereum', got %s", cursor.ChainID)
	}
	if cursor.CurrentBlock != 1000 {
		t.Errorf("expected block 1000, got %d", cursor.CurrentBlock)
	}
	if cursor.State != domain.CursorStateInit {
		t.Errorf("expected state init, got %s", cursor.State)
	}
}

func TestManagerAdvance(t *testing.T) {
	repo := newMockCursorRepo()
	manager := NewManager(repo)
	ctx := context.Background()

	// Initialize cursor at block 1000
	_, err := manager.Initialize(ctx, "ethereum", 1000)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Set to scanning state (init cannot advance)
	repo.UpdateState(ctx, "ethereum", domain.CursorStateScanning)

	// Advance to 1001 should succeed
	err = manager.Advance(ctx, "ethereum", 1001, "0xabc")
	if err != nil {
		t.Errorf("Advance to 1001 failed: %v", err)
	}

	// Verify cursor updated
	cursor, _ := manager.Get(ctx, "ethereum")
	if cursor.CurrentBlock != 1001 {
		t.Errorf("expected block 1001, got %d", cursor.CurrentBlock)
	}
}

func TestManagerAdvance_GapDetection(t *testing.T) {
	repo := newMockCursorRepo()
	manager := NewManager(repo)
	ctx := context.Background()

	_, _ = manager.Initialize(ctx, "ethereum", 1000)
	repo.UpdateState(ctx, "ethereum", domain.CursorStateScanning)

	// Try to skip to block 1005 (gap)
	err := manager.Advance(ctx, "ethereum", 1005, "0xdef")
	if err == nil {
		t.Error("expected error for gap, got nil")
	}
	if err != nil && !contains(err.Error(), "gap") {
		t.Errorf("expected gap error, got: %v", err)
	}
}

func TestManagerAdvance_PausedCursor(t *testing.T) {
	repo := newMockCursorRepo()
	manager := NewManager(repo)
	ctx := context.Background()

	_, _ = manager.Initialize(ctx, "ethereum", 1000)
	_ = manager.SetState(ctx, "ethereum", domain.CursorStateScanning, "start")
	_ = manager.Pause(ctx, "ethereum", "maintenance")

	err := manager.Advance(ctx, "ethereum", 1001, "0xabc")
	if err != ErrCursorPaused {
		t.Errorf("expected ErrCursorPaused, got: %v", err)
	}
}

func TestManagerRollback(t *testing.T) {
	repo := newMockCursorRepo()
	manager := NewManager(repo)
	ctx := context.Background()

	// Track state changes
	var transitions []Transition
	manager.SetStateChangeCallback(func(chainID string, t Transition) {
		transitions = append(transitions, t)
	})

	_, _ = manager.Initialize(ctx, "ethereum", 1000)
	_ = manager.SetState(ctx, "ethereum", domain.CursorStateScanning, "start")

	// Advance a few blocks
	for i := uint64(1001); i <= 1005; i++ {
		repo.UpdateBlock(ctx, "ethereum", i, "0x...")
	}
	repo.cursors["ethereum"].CurrentBlock = 1005

	// Rollback to block 1002
	err := manager.Rollback(ctx, "ethereum", 1002, "0xsafe")
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	cursor, _ := manager.Get(ctx, "ethereum")
	if cursor.CurrentBlock != 1002 {
		t.Errorf("expected block 1002 after rollback, got %d", cursor.CurrentBlock)
	}
	if cursor.State != domain.CursorStateReorg {
		t.Errorf("expected reorg state, got %s", cursor.State)
	}

	// Should have recorded the transition to REORG
	found := false
	for _, tr := range transitions {
		if tr.To == domain.CursorStateReorg {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected transition to REORG to be recorded")
	}
}

func TestManagerGetLag(t *testing.T) {
	repo := newMockCursorRepo()
	manager := NewManager(repo)
	ctx := context.Background()

	_, _ = manager.Initialize(ctx, "ethereum", 1000)

	lag, err := manager.GetLag(ctx, "ethereum", 1100)
	if err != nil {
		t.Fatalf("GetLag failed: %v", err)
	}
	if lag != 100 {
		t.Errorf("expected lag 100, got %d", lag)
	}
}

// =============================================================================
// Metrics Tests
// =============================================================================

func TestMetricsCollector(t *testing.T) {
	mc := NewMetricsCollector(10)

	now := time.Now()
	for i := 0; i < 5; i++ {
		mc.RecordBlock(uint64(100+i), now.Add(time.Duration(i)*time.Second))
	}

	metrics := mc.GetMetrics()

	if metrics.BlocksPerSecond < 0.5 || metrics.BlocksPerSecond > 2.0 {
		t.Errorf("expected ~1 block/sec, got %f", metrics.BlocksPerSecond)
	}
}

func TestMetricsCollector_TransitionTracking(t *testing.T) {
	mc := NewMetricsCollector(10)

	mc.RecordTransition(NewTransition(domain.CursorStateInit, domain.CursorStateScanning, "start"))
	mc.RecordTransition(NewTransition(domain.CursorStateScanning, domain.CursorStateReorg, "reorg detected"))

	metrics := mc.GetMetrics()

	if len(metrics.StateHistory) != 2 {
		t.Errorf("expected 2 transitions, got %d", len(metrics.StateHistory))
	}
	if metrics.LastReorgAt == nil {
		t.Error("expected LastReorgAt to be set")
	}
}

// =============================================================================
// Helpers
// =============================================================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
